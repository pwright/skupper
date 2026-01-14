# TLS Certificate Processing in Skupper

This document explains how TLS certificates are managed, processed, and synchronized between Skupper control plane components and the skupper-router for developers working on the codebase.

## Overview

Skupper uses mutual TLS (mTLS) to secure all inter-site communications. The control plane is responsible for:
1. Generating or managing TLS certificates
2. Storing certificates in Kubernetes Secrets (or filesystem for non-Kubernetes)
3. Synchronizing certificates to the router's filesystem
4. Configuring SSL profiles in the router
5. Monitoring certificate updates and rotations

## TLS Architecture

### Certificate Types

**1. Site CA Certificate** (`skupper-site-ca`)
- **Purpose**: Root of trust for a site
- **Lifetime**: 5 years (default)
- **Usage**: Signs all certificates for links and router access
- **Location**: Kubernetes Secret in site namespace
- **Code**: `internal/certs/certs.go` - `GenerateSecret()`

**2. RouterAccess Server Certificate** (`skupper-site-server`)
- **Purpose**: TLS server certificate for accepting incoming links
- **Lifetime**: 5 years (default)
- **Usage**: Server authentication, signed by site CA
- **Key Usage**: Digital Signature, Key Encipherment, Server Auth
- **Location**: Kubernetes Secret, mounted to router pod
- **Code**: `internal/certs/certs.go` - `GenerateSecret()`

**3. Link Client Certificate** (various names)
- **Purpose**: TLS client certificate for outgoing links
- **Lifetime**: 5 years (default)
- **Usage**: Client authentication, signed by accepting site's CA
- **Key Usage**: Digital Signature, Key Encipherment, Client Auth
- **Location**: Kubernetes Secret in linking site namespace
- **Code**: `internal/certs/certs.go` - `GenerateSecret()`

### Certificate Secret Format

All certificates follow the standard Kubernetes `kubernetes.io/tls` Secret format:

```yaml
apiVersion: v1
kind: Secret
type: kubernetes.io/tls
metadata:
  name: <certificate-name>
data:
  ca.crt: <base64-encoded-ca-certificate>
  tls.crt: <base64-encoded-certificate>
  tls.key: <base64-encoded-private-key>
```

**Fields**:
- `ca.crt`: PEM-encoded CA certificate(s) for validation
- `tls.crt`: PEM-encoded X.509 certificate
- `tls.key`: PKCS#1 private key

## Certificate Generation Flow

### Kubernetes Platform

#### 1. Site Initialization with Link Access

**Location**: `internal/kube/site/site.go`

```go
// When a Site is created with link access enabled:
func (s *Site) ensureSiteCa() error {
    // Check if skupper-site-ca exists
    ca, err := s.clients.GetKubeClient().CoreV1().Secrets(s.namespace).Get(...)
    
    if errors.IsNotFound(err) {
        // Generate self-signed CA
        caSecret, err := certs.GenerateSecret(
            "skupper-site-ca",
            "skupper-site-ca",
            []string{"skupper-site-ca"},
            5*365*24*time.Hour, // 5 years
            nil, // nil = self-signed
        )
        // Create the secret
        s.clients.GetKubeClient().CoreV1().Secrets(s.namespace).Create(...)
    }
}
```

#### 2. RouterAccess Server Certificate Generation

**Location**: `internal/kube/site/site.go`

```go
func (s *Site) ensureRouterAccessCertificate() error {
    // Get the site CA
    ca, err := s.clients.GetKubeClient().CoreV1().Secrets(s.namespace).Get(
        context.TODO(), "skupper-site-ca", metav1.GetOptions{})
    
    // Get RouterAccess endpoints (hosts)
    hosts := s.getRouterAccessHosts()
    
    // Generate server certificate signed by site CA
    serverSecret, err := certs.GenerateSecret(
        "skupper-site-server",
        "skupper-router", // Common Name
        hosts,            // Subject Alternative Names
        5*365*24*time.Hour,
        ca, // Signed by site CA
    )
    
    // Create the secret
    s.clients.GetKubeClient().CoreV1().Secrets(s.namespace).Create(...)
}
```

**Important**: Hosts include both DNS names and IP addresses. Due to a router limitation, IPs are added as DNS entries:

```go
// From internal/certs/certs.go
for _, h := range hosts {
    h = strings.TrimSpace(h)
    if ip := net.ParseIP(h); ip != nil {
        template.IPAddresses = append(template.IPAddresses, ip)
    }
    template.DNSNames = append(template.DNSNames, h) // IP also added as DNS
}
```

#### 3. Link Client Certificate Generation

**Location**: `internal/kube/grants/grants.go` (for AccessToken redemption)

```go
func (g *GrantsServer) issueClientCertificate(grantName string) (*corev1.Secret, error) {
    // Get site CA
    ca, err := g.client.CoreV1().Secrets(g.namespace).Get(
        context.TODO(), "skupper-site-ca", metav1.GetOptions{})
    
    // Generate client certificate
    clientSecret, err := certs.GenerateSecret(
        generateRandomName(), // Random name for link cert
        grantName,            // Common Name
        []string{},           // No SANs needed for client certs
        5*365*24*time.Hour,
        ca, // Signed by site CA
    )
    
    return clientSecret, nil
}
```

### Non-Kubernetes Platform

**Location**: `internal/nonkube/common/fs_config_renderer.go`

For non-Kubernetes platforms, certificates are managed as files on the filesystem:

```go
func (s *SiteStateRenderer) renderCertificates() error {
    certsPath := path.Join(siteConfigPath, string(api.CertificatesPath))
    
    // Write CA certificate
    os.WriteFile(path.Join(certsPath, "skupper-site-ca/ca.crt"), ...)
    
    // Write server certificate
    os.WriteFile(path.Join(certsPath, "skupper-site-server/tls.crt"), ...)
    os.WriteFile(path.Join(certsPath, "skupper-site-server/tls.key"), ...)
    
    // Write link certificates
    for _, link := range links {
        os.WriteFile(path.Join(certsPath, link.Name+"/tls.crt"), ...)
        os.WriteFile(path.Join(certsPath, link.Name+"/tls.key"), ...)
        os.WriteFile(path.Join(certsPath, link.Name+"/ca.crt"), ...)
    }
}
```

## Certificate Synchronization to Router

### Kubernetes: Secret to Filesystem Sync

**Location**: `internal/kube/secrets/sync.go`

The `Sync` component watches Kubernetes Secrets and writes them to the router's filesystem:

```go
type Sync struct {
    cache          SecretsCache
    configured     map[string]qdr.SslProfile  // What router expects
    profileSecrets map[string]syncContext     // What we have
    callback       Callback                   // Notify on updates
}

func (s *Sync) handle(key string, secret *corev1.Secret) error {
    // Extract profile metadata from secret
    metadata, found, err := fromSecret(secret)
    
    for _, profileMetadata := range metadata {
        // Check if this profile is configured in router
        configuredProfile, isConfigured := s.getConfigured(profileMetadata.ProfileName)
        
        if isConfigured {
            // Write certificate files to disk
            writeSslProfile(secret, configuredProfile)
            
            // Notify callback (triggers router config update)
            s.doCallback(profileMetadata.ProfileName)
        }
    }
}

func writeSslProfile(secret *corev1.Secret, profile qdr.SslProfile) error {
    // profile.CaCertFile = "/etc/skupper-router-certs/<profile-name>/ca.crt"
    baseName := path.Dir(profile.CaCertFile)
    os.MkdirAll(baseName, 0755)
    
    // Write certificate files
    os.WriteFile(profile.CaCertFile, secret.Data["ca.crt"], 0644)
    os.WriteFile(profile.CertFile, secret.Data["tls.crt"], 0644)
    os.WriteFile(profile.PrivateKeyFile, secret.Data["tls.key"], 0600)
}
```

### Filesystem Layout

Certificates are written to `/etc/skupper-router-certs/` in the router container:

```
/etc/skupper-router-certs/
├── skupper-site-server/
│   ├── ca.crt      # Site CA certificate
│   ├── tls.crt     # Server certificate
│   └── tls.key     # Server private key
├── link-to-site-a/
│   ├── ca.crt      # Remote site's CA
│   ├── tls.crt     # Client certificate
│   └── tls.key     # Client private key
└── my-service-tls/
    ├── ca.crt      # Service CA
    ├── tls.crt     # Service certificate
    └── tls.key     # Service private key
```

## SSL Profile Configuration

### Creating SSL Profiles

**Location**: `internal/qdr/qdr.go`

SSL profiles tell the router where to find certificate files:

```go
func ConfigureSslProfile(name string, basePath string, requireClientCerts bool) SslProfile {
    profile := SslProfile{
        Name:           name,
        CaCertFile:     path.Join(basePath, name, "ca.crt"),
        CertFile:       path.Join(basePath, name, "tls.crt"),
        PrivateKeyFile: path.Join(basePath, name, "tls.key"),
    }
    
    if requireClientCerts {
        profile.CaCertFile = path.Join(basePath, name, "ca.crt")
    }
    
    return profile
}

type SslProfile struct {
    Name           string `json:"name"`
    CaCertFile     string `json:"caCertFile,omitempty"`
    CertFile       string `json:"certFile,omitempty"`
    PrivateKeyFile string `json:"privateKeyFile,omitempty"`
    Ordinal        uint64 `json:"-"` // For rotation tracking
}
```

### Adding Profiles to Router Config

**Location**: `internal/kube/site/site.go`

```go
func (s *Site) Apply(config *qdr.RouterConfig) bool {
    // Add SSL profile for RouterAccess
    config.AddSslProfile(qdr.ConfigureSslProfile(
        "skupper-local-server",
        "/etc/skupper-router-certs",
        true, // Require client certificates
    ))
    
    // Add SSL profiles for links
    for _, link := range s.links {
        config.AddSslProfile(qdr.ConfigureSslProfile(
            link.Spec.TlsCredentials,
            "/etc/skupper-router-certs",
            false, // Client doesn't require client certs
        ))
    }
}
```

### Associating Profiles with Listeners/Connectors

**Location**: `internal/site/routeraccess.go`, `internal/site/link.go`

```go
// RouterAccess Listener (server)
listener := qdr.Listener{
    Name:       "skupper-inter-router",
    Role:       qdr.RoleInterRouter,
    Host:       "0.0.0.0",
    Port:       "55671",
    SslProfile: "skupper-local-server", // References SSL profile
}

// Link Connector (client)
connector := qdr.Connector{
    Name:       "link-to-remote-site",
    Role:       qdr.RoleInterRouter,
    Host:       "remote-site.example.com",
    Port:       "55671",
    SslProfile: "link-to-remote-site-tls", // References SSL profile
}
```

## Router Configuration Update Flow

### 1. ConfigMap Update

**Location**: `internal/kube/site/site.go`

```go
func (s *Site) updateRouterConfig(update qdr.ConfigUpdate) error {
    // Update is applied to RouterConfig
    config := s.getCurrentRouterConfig()
    changed := update.Apply(config)
    
    if changed {
        // Serialize to JSON
        data, err := config.AsConfigMapData()
        
        // Update ConfigMap
        cm, err := s.clients.GetKubeClient().CoreV1().ConfigMaps(s.namespace).Get(...)
        cm.Data = data
        s.clients.GetKubeClient().CoreV1().ConfigMaps(s.namespace).Update(...)
    }
}
```

### 2. Kube Adaptor Watches ConfigMap

**Location**: `internal/kube/adaptor/config_sync.go`

```go
func (c *ConfigSync) configEvent(key string, configmap *corev1.ConfigMap) error {
    // Parse router config from ConfigMap
    desired, err := qdr.GetRouterConfigFromConfigMap(configmap)
    
    // Sync SSL profiles to filesystem
    c.syncSslProfileCredentialsToDisk(desired.SslProfiles)
    
    // Sync router config via AMQP
    c.syncRouterConfig(desired)
}

func (c *ConfigSync) syncSslProfileCredentialsToDisk(profiles map[string]qdr.SslProfile) error {
    // Tell secret syncer what profiles we expect
    delta := c.profileSyncer.Expect(profiles)
    
    if !delta.Empty() {
        return delta.Error() // Missing or outdated certificates
    }
}
```

### 3. Router Applies SSL Profiles

**Location**: `internal/qdr/amqp_mgmt.go`

```go
func (a *Agent) CreateSslProfile(profile SslProfile) error {
    // Check if profile already exists
    existing, err := a.Query("io.skupper.router.sslProfile", []string{})
    
    if profileExists(existing, profile.Name) {
        return nil // Already exists
    }
    
    // Create via AMQP management
    return a.Create("io.skupper.router.sslProfile", profile.Name, profile)
}
```

## Certificate Rotation

### Rotation Process

**Location**: `internal/kube/secrets/sync.go`

Certificate rotation is tracked using ordinals:

```go
type profileContext struct {
    ProfileName string
    Ordinal     uint64 // Increments on each Secret update
}

func (s *Sync) handleProfile(key string, secret *corev1.Secret, pctx profileContext) (bool, error) {
    prev, hadPrev := s.getProfile(pctx.ProfileName)
    configuredProfile, isConfigured := s.getConfigured(pctx.ProfileName)
    
    // Check if ordinal advanced
    if hadPrev && pctx.Ordinal < prev.Ordinal {
        // Ignore downgrades
        return false, nil
    }
    
    // Check if content changed
    sumChanged := updateSecretChecksum(secret, &prev.SecretContentSum)
    
    if !hadPrev || sumChanged {
        // Write new certificates to disk
        writeSslProfile(secret, configuredProfile)
        
        // Notify callback to update router
        return true, nil
    }
}
```

### Ordinal Tracking

Ordinals are stored in Secret annotations:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: skupper-site-server
  annotations:
    skupper.io/tls-ordinal: "2"  # Increments on rotation
    skupper.io/tls-prior-valid-revisions: "1"  # Keep old connections alive
```

### Router Connection Migration

When certificates rotate:

1. **New certificates written** to filesystem
2. **SSL profile updated** in router via AMQP
3. **New connections** use new certificates
4. **Old connections** remain open (configurable via `tls-prior-valid-revisions`)
5. **Graceful migration** without disrupting traffic

## Certificate Validation

### Host Validation

**Location**: `internal/certs/certs.go`

```go
// Generate certificate with proper SANs
template := x509.Certificate{
    Subject: pkix.Name{
        CommonName: subject,
    },
    ExtKeyUsage: []x509.ExtKeyUsage{
        x509.ExtKeyUsageServerAuth,
        x509.ExtKeyUsageClientAuth,
    },
}

for _, h := range hosts {
    h = strings.TrimSpace(h)
    if ip := net.ParseIP(h); ip != nil {
        template.IPAddresses = append(template.IPAddresses, ip)
    }
    // IMPORTANT: Also add as DNS name due to router limitation
    template.DNSNames = append(template.DNSNames, h)
}
```

### CA Trust Chain

```go
// Link certificate includes accepting site's CA
secret.Data["ca.crt"] = acceptingSiteCA.Data["tls.crt"]

// RouterAccess validates clients against its CA
routerAccessProfile.CaCertFile = "/etc/skupper-router-certs/skupper-site-ca/ca.crt"
```

## Development Guidelines

### Adding New Certificate Types

1. **Define Secret name** and purpose
2. **Generate certificate** using `certs.GenerateSecret()`
3. **Create SSL profile** with `qdr.ConfigureSslProfile()`
4. **Associate with listener/connector** in router config
5. **Ensure secret syncer** watches the secret

### Testing Certificate Changes

```bash
# Check certificate in secret
kubectl get secret skupper-site-server -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout

# Check certificate on filesystem (in router pod)
kubectl exec -it deployment/skupper-router -c router -- \
    openssl x509 -in /etc/skupper-router-certs/skupper-site-server/tls.crt -text -noout

# Test TLS connection
kubectl exec -it deployment/skupper-router -c router -- \
    openssl s_client \
    -CAfile /etc/skupper-router-certs/link-cert/ca.crt \
    -cert /etc/skupper-router-certs/link-cert/tls.crt \
    -key /etc/skupper-router-certs/link-cert/tls.key \
    -connect remote-host:55671
```

### Debugging Certificate Issues

1. **Check Secret exists** and has correct format
2. **Verify filesystem sync** - files present in router pod
3. **Check SSL profile** in router config (ConfigMap)
4. **Query router** via AMQP for SSL profile status
5. **Check router logs** for TLS errors
6. **Validate certificate** chain and expiration

## Code Locations Reference

| Component | File | Purpose |
|-----------|------|---------|
| Certificate Generation | `internal/certs/certs.go` | Generate X.509 certificates |
| Secret Sync | `internal/kube/secrets/sync.go` | Watch secrets, write to filesystem |
| Secret Manager | `internal/kube/secrets/manager.go` | Coordinate secret watching |
| SSL Profile Config | `internal/qdr/qdr.go` | Create SSL profile structures |
| Site Certificate Management | `internal/kube/site/site.go` | Manage site CA and server certs |
| Link Certificate Issuance | `internal/kube/grants/grants.go` | Issue client certificates |
| Config Sync | `internal/kube/adaptor/config_sync.go` | Sync config to router |
| AMQP Management | `internal/qdr/amqp_mgmt.go` | Update router via AMQP |
| Non-Kubernetes Certs | `internal/nonkube/common/fs_config_renderer.go` | Filesystem-based cert management |

## Related Documentation

- [TLS Requirements](../tls/README.md) - User-facing TLS documentation
- [Prerequisites](prerequisites.md) - Development environment setup
- [Code Generation](code_generation.md) - API client generation

## Security Considerations

1. **Private keys** are written with `0600` permissions
2. **Certificates** are written with `0644` permissions
3. **CA keys** never leave the control plane
4. **Rotation** preserves service continuity
5. **Mutual TLS** required for all inter-site communication
6. **No central CA** - each site is its own root of trust (default)