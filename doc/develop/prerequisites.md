# Developer Prerequisites for Skupper CLI

## Repository Overview

This repository contains the Skupper v2 control plane and management components. Together with the [skupper-router](https://github.com/skupperproject/skupper-router) (based on Apache Qpid Dispatch Router), these components work together to create and manage Virtual Application Networks (VANs).

**Note:** The actual data plane routing is provided by the skupper-router project. This repository focuses on the control plane, CLI, and management infrastructure.

### Main Components

The repository is organized into several key components, each serving a specific purpose:

#### 1. **Skupper CLI** (`cmd/skupper/`)
- Command-line interface for managing Skupper sites, connectors, listeners, and links
- Supports both Kubernetes and non-Kubernetes (Linux, Docker, Podman) platforms
- Primary tool for developers and operators to interact with Skupper
- Built target: `make build-cli` → produces `skupper` binary

#### 2. **Skupper Controller** (`cmd/controller/`)
- Kubernetes controller that manages Skupper Custom Resources (CRDs)
- Reconciles Site, Connector, Listener, Link, and other Skupper resources
- Can be deployed cluster-scoped or namespace-scoped
- Built target: `make build-controller` → produces `controller` binary

#### 3. **Kube Adaptor** (`cmd/kube-adaptor/`)
- Bridges Kubernetes resources with the Skupper router
- Manages the lifecycle of router configurations based on Kubernetes state
- Handles service bindings and network topology
- Built target: `make build-kube-adaptor` → produces `kube-adaptor` binary

#### 4. **Network Observer** (`cmd/network-observer/`)
- Collects telemetry and operational data from the entire Skupper network
- Exposes REST API and Prometheus metrics
- Powers the [Skupper Console](https://github.com/skupperproject/skupper-console) web UI
- Provides network-wide visibility across all sites
- Built target: `make build-network-observer` → produces `network-observer` binary

#### 5. **System Controller** (`cmd/system-controller/`)
- Manages system-level Skupper operations
- Handles non-Kubernetes site lifecycle on Linux systems
- Built target: `make build-system-controller` → produces `system-controller` binary

### Supporting Code

#### API Definitions (`api/`, `pkg/apis/`)
- Go types for Skupper Custom Resource Definitions (CRDs)
- Client libraries for interacting with Skupper resources
- Generated code for Kubernetes client-go integration

#### Internal Packages (`internal/`)
- **`internal/cmd/skupper/`**: CLI command implementations
- **`internal/kube/`**: Kubernetes-specific logic (controllers, watchers, resource management)
- **`internal/nonkube/`**: Non-Kubernetes platform support (Linux, Docker, Podman)
- **`internal/qdr/`**: Apache Qpid Dispatch Router integration and management
- **`internal/site/`**: Core site, connector, and listener logic
- **`internal/network/`**: Network topology and flow management
- **`internal/certs/`**: Certificate and TLS management

#### Configuration (`config/`)
- **`config/crd/bases/`**: Custom Resource Definition specifications
- **`config/rbac/`**: Kubernetes RBAC configurations
- **`config/samples/`**: Example Skupper resource manifests

#### Deployment (`charts/`)
- Helm charts for deploying Skupper controller and network observer
- Supports various deployment scenarios and configurations

#### Documentation (`doc/`)
- **`doc/develop/`**: Developer documentation (this file, code generation guides)
- **`doc/tls/`**: TLS certificate management documentation
- **`doc/adr/`**: Architectural Decision Records

#### Testing (`tests/`)
- End-to-end test scenarios using Ansible
- Integration tests for various Skupper features
- Test infrastructure and utilities

### Key Technologies

- **Go 1.24+**: Primary programming language
- **Kubernetes**: Target platform for controller and CRDs
- **Apache Qpid Dispatch Router**: Underlying message routing technology
- **AMQP**: Protocol for inter-site communication
- **Docker/Podman**: Container runtime for non-Kubernetes deployments
- **Helm**: Package manager for Kubernetes deployments

### Development Workflow

Depending on what you're working on, you'll interact with different parts of the codebase:

- **CLI Development**: Focus on `cmd/skupper/` and `internal/cmd/skupper/`
- **Controller Development**: Work with `cmd/controller/` and `internal/kube/`
- **API Changes**: Modify `pkg/apis/` and regenerate client code (see [code_generation.md](code_generation.md))
- **Non-Kubernetes Support**: Develop in `internal/nonkube/` and `cmd/bootstrap/`
- **Network Observability**: Enhance `cmd/network-observer/` and related metrics

### Build Targets

The Makefile provides targets for building all components:

```bash
make all                    # Build all components
make build-cli              # Build skupper CLI
make build-controller       # Build Kubernetes controller
make build-kube-adaptor     # Build kube adaptor

---

## Control Plane and Data Plane Architecture

### Overview

Skupper's architecture separates concerns between the **control plane** (this repository) and the **data plane** ([skupper-router](https://github.com/skupperproject/skupper-router)):

- **Control Plane**: Manages configuration, monitors state, and orchestrates the network
- **Data Plane**: Routes actual application traffic between sites using AMQP protocol

### Interfaces Between Control Plane and Router

The control plane components interact with the skupper-router through several well-defined interfaces:

#### 1. **Router Configuration (ConfigMap)**

**Direction**: Control Plane → Router

**Mechanism**: Kubernetes ConfigMap (`skupper-router`) containing router configuration in JSON format

**Location in Code**: 
- Configuration generation: `internal/qdr/qdr.go`
- ConfigMap management: `internal/kube/site/site.go`, `internal/kube/qdr/update_config.go`
- Non-Kubernetes: `internal/nonkube/common/fs_config_renderer.go`

**What Gets Configured**:
- Router metadata (site ID, mode: interior/edge)
- Listeners (AMQP, inter-router, edge connections)
- Connectors (links to other sites)
- SSL/TLS profiles for secure communication
- TCP/HTTP bridges for application services
- Addresses and routing policies
- Log levels and configuration

**Configuration Structure**:
```go
type RouterConfig struct {
    Metadata    RouterMetadata
    Addresses   map[string]Address
    SslProfiles map[string]SslProfile
    Listeners   map[string]Listener
    Connectors  map[string]Connector
    LogConfig   map[string]LogConfig
    Bridges     BridgeConfig
}
```

**Update Flow**:
1. Controller/CLI modifies Skupper CRDs (Site, Listener, Connector, Link)
2. Control plane reconciles changes
3. Updates ConfigMap with new router configuration
4. Router watches ConfigMap and applies changes dynamically

#### 2. **AMQP Management Protocol**

**Direction**: Control Plane ↔ Router (bidirectional)

**Mechanism**: AMQP management protocol over `amqp://localhost:5672`

**Location in Code**:
- AMQP client: `internal/qdr/amqp_mgmt.go`, `internal/qdr/messaging.go`
- Agent pool: `internal/qdr/request.go`
- Kube adaptor: `internal/kube/adaptor/config_sync.go`

**Operations**:

**Query Operations** (Control Plane reads from Router):
- `io.skupper.router.router` - Router identity and metadata
- `io.skupper.router.router.node` - Network topology
- `io.skupper.router.connection` - Active connections
- `io.skupper.router.connector` - Connector status
- `io.skupper.router.listener` - Listener status
- `io.skupper.router.tcpConnector` - TCP bridge connectors
- `io.skupper.router.tcpListener` - TCP bridge listeners
- `io.skupper.router.tcpConnection` - Active TCP connections
- `io.skupper.router.sslProfile` - TLS certificate profiles

**Management Operations** (Control Plane modifies Router):
- `CREATE` - Add new entities (connectors, listeners, bridges)
- `UPDATE` - Modify existing entities (SSL profiles, log levels)
- `DELETE` - Remove entities (connectors, listeners)

**Example Query**:
```go
agent, err := qdr.NewAgent("amqp://localhost:5672", nil)
records, err := agent.Query("io.skupper.router.connection", []string{})
```

#### 3. **TLS Certificates (Secrets/Filesystem)**

**Direction**: Control Plane → Router

**Mechanism**: 
- **Kubernetes**: Secrets mounted as volumes at `/etc/skupper-router-certs/`
- **Non-Kubernetes**: Files written to filesystem

**Location in Code**:
- Secret synchronization: `internal/kube/secrets/sync.go`, `internal/kube/secrets/manager.go`
- Profile management: `internal/qdr/qdr.go` (ConfigureSslProfile)
- Non-Kubernetes: `internal/nonkube/common/fs_config_renderer.go`

**Certificate Types**:
- **CA certificates**: `ca.crt` - Certificate authority for validation
- **Server certificates**: `tls.crt`, `tls.key` - Router's identity
- **Client certificates**: For mutual TLS authentication

**Profile Structure**:
```go
type SslProfile struct {
    Name           string
    CaCertFile     string  // /etc/skupper-router-certs/<profile>/ca.crt
    CertFile       string  // /etc/skupper-router-certs/<profile>/tls.crt
    PrivateKeyFile string  // /etc/skupper-router-certs/<profile>/tls.key
}
```

**Synchronization Flow**:
1. Control plane creates/updates Kubernetes Secrets
2. Secrets mounted into router pod
3. Control plane updates SSL profiles in router config
4. Router loads certificates from filesystem
5. Certificates used for inter-site links and service TLS

#### 4. **Network Status Collection (Flow Records)**

**Direction**: Router → Control Plane

**Mechanism**: AMQP event stream with flow records

**Location in Code**:
- Flow collection: `internal/kube/adaptor/collector.go`
- Status processing: `internal/flow/status.go`
- Network observer: `cmd/network-observer/`

**Flow Record Types**:
- `SITE` - Site information
- `ROUTER` - Router instances
- `LINK` - Inter-site connections
- `LISTENER` - Service listeners
- `CONNECTOR` - Service connectors
- `FLOW` - Active connections and traffic
- `PROCESS` - Application processes

**Collection Flow**:
1. Router emits flow records via AMQP
2. Kube adaptor collects records
3. Aggregates into network status
4. Writes to ConfigMap (`skupper-network-status`)
5. Network observer reads and exposes via API/metrics

#### 5. **Bridge Configuration (TCP/HTTP Services)**

**Direction**: Control Plane → Router

**Mechanism**: Part of router configuration, managed via AMQP

**Location in Code**:
- Bridge config: `internal/qdr/qdr.go` (BridgeConfig)
- Listener/Connector logic: `internal/site/listener.go`, `internal/site/connector.go`
- Kubernetes bindings: `internal/kube/site/bindings.go`

**Bridge Types**:
- **TCP Listeners**: Expose services into the network
- **TCP Connectors**: Connect to backend services
- **HTTP Listeners/Connectors**: HTTP-aware bridging

**Configuration**:
```go
type BridgeConfig struct {
    TcpListeners  map[string]TcpEndpoint
    TcpConnectors map[string]TcpEndpoint
}

type TcpEndpoint struct {
    Name       string
    Host       string
    Port       string
    Address    string  // AMQP address for routing
    SiteId     string
    SslProfile string  // Optional TLS
}
```

#### 6. **Log Configuration**

**Direction**: Control Plane → Router

**Mechanism**: Router configuration updates via ConfigMap and AMQP

**Location in Code**:
- Log config: `internal/qdr/router_logging.go`
- Site spec: `pkg/apis/skupper/v2alpha1/site_types.go`

**Configurable Modules**:
- `ROUTER` - Core router operations
- `ROUTER_CORE` - Router core functionality
- `ROUTER_HELLO` - Neighbor discovery
- `ROUTER_LS` - Link state routing
- `ROUTER_MA` - Management agent
- `TCP_ADAPTOR` - TCP bridge operations
- `HTTP_ADAPTOR` - HTTP bridge operations

**Log Levels**: `trace`, `debug`, `info`, `notice`, `warning`, `error`, `critical`

### Component Responsibilities

#### **Controller** (`cmd/controller/`)
- Watches Skupper CRDs
- Generates router configuration
- Updates ConfigMaps
- Manages secrets and certificates
- Reconciles desired state

#### **Kube Adaptor** (`cmd/kube-adaptor/`)
- Synchronizes ConfigMap changes to router via AMQP
- Ensures router state matches configuration
- Handles dynamic updates (bridges, connectors, listeners)
- Syncs TLS certificates to filesystem

#### **Network Observer** (`cmd/network-observer/`)
- Collects flow records from all routers
- Aggregates network-wide telemetry
- Exposes REST API and Prometheus metrics
- Powers Skupper Console UI

#### **CLI** (`cmd/skupper/`)
- Creates and manages Skupper resources
- Generates tokens for site linking
- Provides status and debugging commands
- Works with both Kubernetes and non-Kubernetes platforms

### Development Implications

When developing Skupper control plane components:

1. **Router Config Changes**: Modify `internal/qdr/qdr.go` structures
2. **New Router Features**: Add AMQP management operations in `internal/qdr/amqp_mgmt.go`
3. **Service Bindings**: Update bridge logic in `internal/site/` and `internal/kube/site/`
4. **Certificate Management**: Modify `internal/kube/secrets/` or `internal/certs/`
5. **Network Telemetry**: Enhance flow processing in `internal/flow/`

### Testing Router Integration

```bash
# Build control plane components
make build-controller build-kube-adaptor

# Deploy with local router image
export SKUPPER_ROUTER_IMAGE=quay.io/skupper/skupper-router:main
kubectl apply -f skupper-cluster-scope.yaml

# Check router configuration
kubectl get configmap skupper-router -o yaml

# Query router via AMQP (requires skupper-router running)
# Use internal/qdr/amqp_mgmt.go Agent methods
```

### Related Documentation

- Router project: [skupper-router](https://github.com/skupperproject/skupper-router)
- AMQP protocol: [AMQP 1.0 Specification](https://www.amqp.org/resources/specifications)
- Flow records: `cmd/network-observer/spec/openapi.yaml`

make build-network-observer # Build network observer
make build-system-controller # Build system controller
make test                   # Run all tests
```

---

This guide outlines the prerequisites and setup instructions for developers who want to contribute to the Skupper CLI codebase.

## Quick Start Checklist

Before you begin developing for Skupper CLI, ensure you have the following tools installed:

- [ ] **Go 1.24.0+** (1.24.9 recommended)
- [ ] **Container Engine** (Docker or Podman)
- [ ] **kubectl** (Kubernetes command-line tool)
- [ ] **Make** (GNU Make)
- [ ] **jq** (JSON processor)
- [ ] **Git** (version control)
- [ ] **Optional:** Kind or Minikube (for local Kubernetes testing)

---

## Core Requirements

### 1. Go Programming Language

**Required Version:** Go 1.24.0 or later (1.24.9 recommended)

Skupper is written in Go and requires a recent version of the Go toolchain for building and testing.

#### Installation

**Official Download:**
- Visit [https://go.dev/dl/](https://go.dev/dl/)
- Download and install Go 1.24.9 or later

**Linux (using package manager):**
```bash
# Ubuntu/Debian
sudo apt update
sudo apt install golang-1.24

# Fedora/RHEL
sudo dnf install golang

# Arch Linux
sudo pacman -S go
```

**macOS:**
```bash
# Using Homebrew
brew install go
```

**Verification:**
```bash
go version
# Expected output: go version go1.24.9 linux/amd64 (or similar)
```

#### Configuration

Set up your Go workspace:
```bash
# Add to your ~/.bashrc or ~/.zshrc
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin
```

---

### 2. Container Engine

**Required:** Docker **OR** Podman

Skupper uses containers for building multi-platform images and running tests. You need either Docker or Podman installed.

#### Docker

**Installation:**
- **Linux:** [https://docs.docker.com/engine/install/](https://docs.docker.com/engine/install/)
- **macOS:** [https://docs.docker.com/desktop/install/mac-install/](https://docs.docker.com/desktop/install/mac-install/)
- **Windows:** [https://docs.docker.com/desktop/install/windows-install/](https://docs.docker.com/desktop/install/windows-install/)

**Linux Post-Installation:**
```bash
# Add your user to the docker group to run without sudo
sudo usermod -aG docker $USER
newgrp docker
```

**Verification:**
```bash
docker --version
docker run hello-world
```

#### Podman (Alternative to Docker)

**Installation:**
- **Linux:** [https://podman.io/getting-started/installation](https://podman.io/getting-started/installation)
- **macOS:** [https://podman.io/getting-started/installation#macos](https://podman.io/getting-started/installation#macos)

```bash
# Fedora/RHEL
sudo dnf install podman

# Ubuntu/Debian
sudo apt install podman

# macOS
brew install podman
```

**Verification:**
```bash
podman --version
podman run hello-world
```

---

### 3. Kubernetes Tools

#### kubectl (Required)

**kubectl** is the Kubernetes command-line tool used for deploying and testing Skupper.

**Installation:**
- Official guide: [https://kubernetes.io/docs/tasks/tools/](https://kubernetes.io/docs/tasks/tools/)

```bash
# Linux
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# macOS
brew install kubectl

# Verification
kubectl version --client
```

#### Kind (Optional - Recommended for Local Testing)

**Kind** (Kubernetes in Docker) is useful for creating local Kubernetes clusters for testing.

**Installation:**
- Official guide: [https://kind.sigs.k8s.io/docs/user/quick-start/#installation](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)

```bash
# Linux/macOS
go install sigs.k8s.io/kind@latest

# Or using package managers
# macOS
brew install kind

# Verification
kind version
```

**Quick Cluster Setup:**
```bash
# Create a test cluster using the provided script
KUBECONFIG=~/.kube/config ./scripts/kind-dev-cluster -r --metallb -i podman
```

See [`scripts/kind-dev-cluster`](../../scripts/kind-dev-cluster) for more options.

#### Minikube (Optional - Alternative to Kind)

**Minikube** is another option for running Kubernetes locally.

**Installation:**
- Official guide: [https://minikube.sigs.k8s.io/docs/start/](https://minikube.sigs.k8s.io/docs/start/)

```bash
# Linux
curl -LO https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64
sudo install minikube-linux-amd64 /usr/local/bin/minikube

# macOS
brew install minikube

# Verification
minikube version
```

---

### 4. Build Tools

#### Make (Required)

**GNU Make** is used to build Skupper components via the Makefile.

**Installation:**
```bash
# Ubuntu/Debian
sudo apt install build-essential

# Fedora/RHEL
sudo dnf install make

# macOS (comes with Xcode Command Line Tools)
xcode-select --install

# Verification
make --version
```

#### jq (Required)

**jq** is a lightweight JSON processor used in build scripts and Dockerfiles.

**Installation:**
- Official site: [https://jqlang.github.io/jq/download/](https://jqlang.github.io/jq/download/)

```bash
# Ubuntu/Debian
sudo apt install jq

# Fedora/RHEL
sudo dnf install jq

# macOS
brew install jq

# Verification
jq --version
```

#### Git (Required)

**Git** is required for version control and contributing to the project.

**Installation:**
```bash
# Ubuntu/Debian
sudo apt install git

# Fedora/RHEL
sudo dnf install git

# macOS
brew install git

# Verification
git --version
```

**Configuration:**
```bash
# Set your identity
git config --global user.name "Your Name"
git config --global user.email "your.email@example.com"
```

---

### 5. Code Generation Tools (Optional)

These tools are needed if you're working on Kubernetes API types or need to regenerate client code.

#### k8s.io/code-generator

Used for generating Kubernetes client code, informers, and listers.

**Installation:**
```bash
# The code-generator is pulled automatically via go.mod
# To run code generation, use the provided script:
./scripts/update-codegen.sh

# Or run in a container (recommended):
GO_VERSION=1.24.9
podman run -v $(pwd):/work:rw,Z \
  -w /work \
  "docker.io/golang:$GO_VERSION" \
  bash -c 'go mod download && ./scripts/update-codegen.sh'
```

See [code_generation.md](code_generation.md) for detailed instructions.

#### controller-gen (Optional)

Used for generating or validating CRD specifications.

**Installation:**
```bash
go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0

# Verification
controller-gen --version
```

**Usage:**
```bash
# Generate CRDs from Go types
controller-gen crd \
    paths=./pkg/apis/skupper/v2alpha1/... \
    output:crd:dir=./generated
```

---

## Verification Steps

After installing all prerequisites, verify your setup:

```bash
# Check Go
go version

# Check container engine
docker --version  # or: podman --version

# Check Kubernetes tools
kubectl version --client
kind version      # if installed
minikube version  # if installed

# Check build tools
make --version
jq --version
git --version

# Clone the repository
git clone https://github.com/skupperproject/skupper.git
cd skupper

# Build the CLI
make build-cli

# Run tests
make test

# Verify the binary
./skupper version
```

---

## Platform-Specific Notes

### Linux

- **Preferred platform** for Skupper development
- All features and tests are fully supported
- Use your distribution's package manager for most tools

### macOS

- Fully supported for development
- Use Homebrew for easy installation of tools
- Docker Desktop or Podman Desktop recommended for container support
- Some scripts may require GNU versions of tools (e.g., `gnu-sed`, `gnu-tar`)

### Windows

- Development on Windows requires WSL2 (Windows Subsystem for Linux)
- Install WSL2 with Ubuntu: [https://docs.microsoft.com/en-us/windows/wsl/install](https://docs.microsoft.com/en-us/windows/wsl/install)
- Follow Linux instructions within WSL2
- Docker Desktop with WSL2 backend recommended

---

## Next Steps

Once you have all prerequisites installed:

1. **Clone the Repository:**
   ```bash
   git clone https://github.com/skupperproject/skupper.git
   cd skupper
   ```

2. **Build the CLI:**
   ```bash
   make build-cli
   ```

3. **Run Tests:**
   ```bash
   make test
   ```

4. **Try the CLI Example:**
   - See [cmd/skupper/README.md](../../cmd/skupper/README.md) for a quick start guide

5. **Explore the Codebase:**
   - Review the main [README.md](../../README.md) for project overview
   - Check [code_generation.md](code_generation.md) if working on API types
   - Browse [examples](../../cmd/controller/example/README.md) for usage patterns

6. **Set Up Local Testing:**
   ```bash
   # Create a local Kind cluster
   KUBECONFIG=~/.kube/config ./scripts/kind-dev-cluster -r --metallb -i podman
   
   # Deploy Skupper controller
   kubectl apply -f https://github.com/skupperproject/skupper/releases/download/v2-preview/skupper-cluster-scope.yaml
   ```

---

## Troubleshooting

### Go Module Issues

If you encounter Go module errors:
```bash
go mod download
go mod tidy
```

### Container Build Issues

If container builds fail:
```bash
# Clear build cache
docker system prune -a  # or: podman system prune -a

# Ensure you have enough disk space
df -h
```

### Kubernetes Connection Issues

If kubectl cannot connect to your cluster:
```bash
# Check your kubeconfig
kubectl config view

# Verify cluster access
kubectl cluster-info

# For Kind clusters
kind get clusters
kubectl config use-context kind-<cluster-name>
```

### Permission Issues (Linux)

If you get permission errors with Docker:
```bash
# Add user to docker group
sudo usermod -aG docker $USER
newgrp docker

# Or use Podman (rootless by default)
```

---

## Additional Resources

- **Skupper Project:** [https://skupper.io/](https://skupper.io/)
- **Skupper GitHub:** [https://github.com/skupperproject/skupper](https://github.com/skupperproject/skupper)
- **Community:** [https://skupper.io/community/](https://skupper.io/community/)
- **Mailing List:** skupper@googlegroups.com
- **Kubernetes Documentation:** [https://kubernetes.io/docs/](https://kubernetes.io/docs/)
- **Go Documentation:** [https://go.dev/doc/](https://go.dev/doc/)

---

## Contributing

Ready to contribute? Check out:
- GitHub Issues: [https://github.com/skupperproject/skupper/issues](https://github.com/skupperproject/skupper/issues)
- Good First Issues: Look for issues labeled `good-first-issue`
- Pull Request Guidelines: Follow the standard GitHub flow

For questions or help, reach out on the [Skupper community channels](https://skupper.io/community/).
