# AMQP Integration in Skupper

This document provides a comprehensive guide to all AMQP-related code in the Skupper project, explaining how AMQP is used for router management, flow collection, and inter-component communication.

## Overview

Skupper uses AMQP (Advanced Message Queuing Protocol) 1.0 for:
1. **Router Management** - Configuring and querying the skupper-router via management protocol
2. **Flow Collection** - Collecting network telemetry and flow records
3. **Request/Response** - Internal service communication patterns
4. **Event Streaming** - Real-time network status updates

**AMQP Library**: [`github.com/interconnectedcloud/go-amqp`](https://github.com/interconnectedcloud/go-amqp)

## Architecture

### AMQP Connection Hierarchy

```
AMQP Client (Connection)
  └── Session
      ├── Sender (Link)
      │   ├── Target Address
      │   └── Messages →
      └── Receiver (Link)
          ├── Source Address
          ├── Credit (flow control)
          └── ← Messages
```

### Key Components

| Component | Purpose | Connection | Code Location |
|-----------|---------|------------|---------------|
| **Agent** | Router management client | `amqp://localhost:5672` | `internal/qdr/amqp_mgmt.go` |
| **AgentPool** | Connection pooling for agents | `amqp://localhost:5672` | `internal/qdr/amqp_mgmt.go` |
| **Flow Collector** | Network telemetry collection | `amqp://localhost:5672` | `internal/kube/adaptor/collector.go` |
| **Request Server** | Request/response pattern | Custom addresses | `internal/qdr/request.go` |
| **Connection Factory** | Connection creation | Configurable | `internal/qdr/messaging.go` |

---

## Core AMQP Components

### 1. Connection Factory

**Location**: `internal/qdr/messaging.go`

Creates AMQP connections with optional TLS configuration.

**IMPORTANT**: The "optional TLS" refers to **local connections only** (e.g., `amqp://localhost:5672` for control plane to local router). **Inter-site links ALWAYS require TLS** - there is no option to link sites without TLS. The router enforces mutual TLS (mTLS) for all inter-site connections.

**TLS Usage**:
- **Local connections** (control plane ↔ local router): Plain AMQP on localhost
- **Inter-site links** (router ↔ remote router): **Always TLS/mTLS** - no exceptions
- **Flow collection** (local): Plain AMQP on localhost

```go
type ConnectionFactory struct {
    url            string
    config         TlsConfigRetriever
    connectTimeout time.Duration
}

func (f *ConnectionFactory) Connect() (messaging.Connection, error) {
    if f.config == nil {
        // Plain AMQP connection
        return dial(f.url, 
            amqp.ConnMaxFrameSize(4294967295),
            amqp.ConnConnectTimeout(f.connectTimeout))
    } else {
        // TLS-secured AMQP connection
        tlsConfig, err := f.config.GetTlsConfig()
        return dial(f.url,
            amqp.ConnSASLExternal(),           // Use TLS client cert
            amqp.ConnMaxFrameSize(4294967295),
            amqp.ConnConnectTimeout(f.connectTimeout),
            amqp.ConnTLSConfig(tlsConfig))
    }
}

func dial(addr string, opts ...amqp.ConnOption) (*AmqpConnection, error) {
    client, err := amqp.Dial(addr, opts...)
    if err != nil {
        return nil, err
    }
    session, err := client.NewSession()
    if err != nil {
        client.Close()
        return nil, err
    }
    return &AmqpConnection{client: client, session: session}, nil
}
```

**Key Features**:
- **Max Frame Size**: 4GB (4294967295 bytes) for large messages
- **TLS Support**: SASL EXTERNAL for certificate-based authentication
- **Session Management**: Automatic session creation
- **Timeout Control**: Configurable connection timeout

### 2. AMQP Connection Wrapper

**Location**: `internal/qdr/messaging.go`

Wraps go-amqp client with Skupper's messaging interface.

```go
type AmqpConnection struct {
    client  *amqp.Client
    session *amqp.Session
}

func (c *AmqpConnection) Sender(address string) (messaging.Sender, error) {
    sender, err := c.session.NewSender(amqp.LinkTargetAddress(address))
    if err != nil {
        return nil, err
    }
    return &AmqpSender{connection: c, sender: sender}, nil
}

func (c *AmqpConnection) Receiver(address string, credit uint32) (messaging.Receiver, error) {
    receiver, err := c.session.NewReceiver(
        amqp.LinkSourceAddress(address),
        amqp.LinkCredit(credit),  // Flow control
    )
    if err != nil {
        return nil, err
    }
    return &AmqpReceiver{connection: c, receiver: receiver}, nil
}
```

**Flow Control**:
- **Credit**: Number of messages receiver can accept
- **Backpressure**: Prevents overwhelming receivers
- **Default**: 10 messages for management, configurable for flows

### 3. Sender and Receiver

**Location**: `internal/qdr/messaging.go`

```go
type AmqpSender struct {
    connection *AmqpConnection
    sender     *amqp.Sender
}

func (s *AmqpSender) Send(msg *amqp.Message) error {
    return s.sender.Send(context.Background(), msg)
}

type AmqpReceiver struct {
    connection *AmqpConnection
    receiver   *amqp.Receiver
}

func (s *AmqpReceiver) Receive() (*amqp.Message, error) {
    return s.receiver.Receive(context.Background())
}

func (s *AmqpReceiver) Accept(msg *amqp.Message) error {
    return msg.Accept()  // Acknowledge message
}
```

**Message Acknowledgment**:
- `Accept()`: Message processed successfully
- `Reject()`: Message processing failed
- `Release()`: Return message to queue (not used in Skupper)

---

## Router Management via AMQP

### Agent: Router Management Client

**Location**: `internal/qdr/amqp_mgmt.go`

The Agent provides a high-level interface for managing the router via AMQP management protocol.

```go
type Agent struct {
    connection *amqp.Client
    session    *amqp.Session
    sender     *amqp.Sender      // To $management
    anonymous  *amqp.Sender      // For replies
    receiver   *amqp.Receiver    // Dynamic address for responses
    local      *Router           // Local router info
    closed     bool
}
```

#### Creating an Agent

```go
func newAgent(factory *ConnectionFactory) (*Agent, error) {
    client, err := factory.Connect()
    connection := client.(*AmqpConnection)
    
    // Create dynamic receiver for responses
    receiver, err := connection.session.NewReceiver(
        amqp.LinkSourceAddress(""),      // Empty = dynamic
        amqp.LinkAddressDynamic(),       // Router assigns address
        amqp.LinkCredit(10),
    )
    
    // Create sender to management address
    sender, err := connection.session.NewSender(
        amqp.LinkTargetAddress("$management"),
    )
    
    // Create anonymous sender for replies
    anonymous, err := connection.session.NewSender()
    
    a := &Agent{
        connection: connection.client,
        session:    connection.session,
        sender:     sender,
        anonymous:  anonymous,
        receiver:   receiver,
    }
    
    // Get local router information
    a.local, err = a.GetLocalRouter()
    return a, nil
}
```

**Key Addresses**:
- `$management`: Router's management endpoint
- Dynamic address: Router-assigned reply-to address
- Anonymous sender: Can send to any address

### Management Operations

#### QUERY Operation

**Purpose**: Retrieve entities from router

```go
func (a *Agent) Query(typename string, attributes []string) ([]Record, error) {
    ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
    defer cancel()
    
    // Build request message
    var request amqp.Message
    var properties amqp.MessageProperties
    properties.ReplyTo = a.receiver.Address()  // Where to send response
    properties.CorrelationID = uint64(1)       // Match request/response
    request.Properties = &properties
    
    request.ApplicationProperties = make(map[string]interface{})
    request.ApplicationProperties["operation"] = "QUERY"
    request.ApplicationProperties["entityType"] = typename
    
    body := map[string]interface{}{
        "attributeNames": attributes,
    }
    request.Value = body
    
    // Send request
    if err := a.sender.Send(ctx, &request); err != nil {
        return nil, fmt.Errorf("Could not send request: %s", err)
    }
    
    // Receive response
    response, err := a.receiver.Receive(ctx)
    if err != nil {
        return nil, fmt.Errorf("Failed to receive response: %s", err)
    }
    response.Accept()
    
    // Parse response
    statusCode := response.ApplicationProperties["statusCode"].(int)
    if !isOk(statusCode) {
        return nil, fmt.Errorf("Query failed: %s", 
            response.ApplicationProperties["statusDescription"])
    }
    
    // Extract results
    results := response.Value.(map[string]interface{})
    attributeNames := results["attributeNames"].([]interface{})
    resultsList := results["results"].([]interface{})
    
    records := make([]Record, len(resultsList))
    for i, result := range resultsList {
        records[i] = makeRecord(
            stringify(attributeNames),
            result.([]interface{}),
        )
    }
    return records, nil
}
```

**Common Queries**:
```go
// Get all connections
connections, err := agent.Query("io.skupper.router.connection", []string{})

// Get all connectors
connectors, err := agent.Query("io.skupper.router.connector", []string{})

// Get TCP listeners
listeners, err := agent.Query("io.skupper.router.tcpListener", []string{})

// Get SSL profiles
profiles, err := agent.Query("io.skupper.router.sslProfile", []string{})
```

#### CREATE Operation

**Purpose**: Create new entities in router

```go
func (a *Agent) Create(typename string, name string, entity recordType) error {
    attributes := entity.toRecord()
    return a.request("CREATE", typename, name, attributes)
}

func (a *Agent) request(operation string, typename string, name string, 
                        attributes map[string]interface{}) error {
    ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
    defer cancel()
    
    var request amqp.Message
    var properties amqp.MessageProperties
    properties.ReplyTo = a.receiver.Address()
    properties.CorrelationID = uint64(1)
    request.Properties = &properties
    
    request.ApplicationProperties = make(map[string]interface{})
    request.ApplicationProperties["operation"] = operation
    request.ApplicationProperties["type"] = typename
    request.ApplicationProperties["name"] = name
    
    if attributes != nil {
        request.Value = attributes
    }
    
    if err := a.sender.Send(ctx, &request); err != nil {
        return fmt.Errorf("Could not send request: %s", err)
    }
    
    response, err := a.receiver.Receive(ctx)
    if err != nil {
        return fmt.Errorf("Failed to receive response: %s", err)
    }
    response.Accept()
    
    if status, ok := AsInt(response.ApplicationProperties["statusCode"]); 
       !ok && !isOk(status) {
        return fmt.Errorf("Operation failed: %s", 
            response.ApplicationProperties["statusDescription"])
    }
    return nil
}
```

**Example Usage**:
```go
// Create SSL profile
profile := qdr.SslProfile{
    Name:           "my-tls",
    CaCertFile:     "/etc/certs/ca.crt",
    CertFile:       "/etc/certs/tls.crt",
    PrivateKeyFile: "/etc/certs/tls.key",
}
err := agent.Create("io.skupper.router.sslProfile", "my-tls", profile)

// Create TCP listener
listener := qdr.TcpEndpoint{
    Name:    "backend",
    Host:    "0.0.0.0",
    Port:    "8080",
    Address: "backend:8080",
}
err := agent.Create("io.skupper.router.tcpListener", "backend", listener)
```

#### UPDATE Operation

**Purpose**: Modify existing entities

```go
func (a *Agent) Update(typename string, name string, entity recordType) error {
    attributes := entity.toRecord()
    log.Println("UPDATE", typename, name, attributes)
    return a.request("UPDATE", typename, name, attributes)
}
```

**Example Usage**:
```go
// Update SSL profile with new certificate
profile.Ordinal = 2  // Increment for rotation
err := agent.Update("io.skupper.router.sslProfile", "my-tls", profile)
```

#### DELETE Operation

**Purpose**: Remove entities from router

```go
func (a *Agent) Delete(typename string, name string) error {
    if name == "" {
        return fmt.Errorf("Cannot delete entity of type %s with no name", typename)
    }
    log.Println("DELETE", typename, name)
    return a.request("DELETE", typename, name, nil)
}
```

**Example Usage**:
```go
// Delete connector
err := agent.Delete("io.skupper.router.connector", "old-link")

// Delete TCP listener
err := agent.Delete("io.skupper.router.tcpListener", "backend")
```

### Agent Pool

**Location**: `internal/qdr/amqp_mgmt.go`

Connection pooling for efficient agent reuse.

```go
type AgentPool struct {
    url            string
    config         TlsConfigRetriever
    pool           chan *Agent
    connectTimeout time.Duration
}

func NewAgentPool(url string, config TlsConfigRetriever) *AgentPool {
    return &AgentPool{
        url:    url,
        config: config,
        pool:   make(chan *Agent, 10),  // Pool size: 10
    }
}

func (p *AgentPool) Get() (*Agent, error) {
    var a *Agent
    var err error
    select {
    case a = <-p.pool:  // Reuse existing agent
    default:
        a, err = ConnectTimeout(p.url, p.config, p.connectTimeout)
    }
    return a, err
}

func (p *AgentPool) Put(a *Agent) {
    if !a.closed {
        select {
        case p.pool <- a:  // Return to pool
        default:
            a.Close()      // Pool full, close connection
        }
    }
}
```

**Usage Pattern**:
```go
pool := qdr.NewAgentPool("amqp://localhost:5672", nil)

// Get agent from pool
agent, err := pool.Get()
if err != nil {
    return err
}
defer pool.Put(agent)  // Return to pool

// Use agent
connectors, err := agent.GetLocalConnectors()
```

### Remote Router Management

**Location**: `internal/qdr/amqp_mgmt.go`

Query routers across the network using special addresses.

```go
func GetRouterAgentAddress(id string, edge bool) string {
    if edge {
        return "amqp:/_edge/" + id + "/$management"
    } else {
        return "amqp:/_topo/0/" + id + "/$management"
    }
}

func (a *Agent) QueryByAgentAddress(typename string, attributes []string, 
                                    agentAddress string) ([]Record, error) {
    // Query specific router by address
    ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
    defer cancel()
    
    var request amqp.Message
    var properties amqp.MessageProperties
    properties.To = agentAddress  // Target specific router
    properties.ReplyTo = a.receiver.Address()
    // ... rest of query logic
}
```

**Address Patterns**:
- Interior router: `amqp:/_topo/0/<router-id>/$management`
- Edge router: `amqp:/_edge/<router-id>/$management`

**Example**:
```go
// Get all routers in network
routers, err := agent.GetAllRouters()

// Query each router
for _, router := range routers {
    address := qdr.GetRouterAgentAddress(router.Id, router.Edge)
    connections, err := agent.QueryByAgentAddress(
        "io.skupper.router.connection",
        []string{},
        address,
    )
}
```

---

## Flow Collection via AMQP

### Flow Records

**Location**: `internal/kube/adaptor/collector.go`, `pkg/vanflow/`

Flow records provide network telemetry using AMQP event streaming.

```go
func siteCollector(ctx context.Context, cli *internalclient.KubeClient) {
    // Create AMQP connection for flow collection
    factory := session.NewContainerFactory(
        "amqp://localhost:5672",
        session.ContainerConfig{
            ContainerID: "kube-flow-collector",
        },
    )
    
    statusSyncClient := &StatusSyncClient{
        client: cli.Kube.CoreV1().ConfigMaps(cli.Namespace),
    }
    
    // Start flow collection
    statusSync := flow.NewStatusSync(
        factory,
        nil,
        statusSyncClient,
        types.NetworkStatusConfigMapName,
    )
    go statusSync.Run(ctx)
}
```

### Flow Record Types

Flow records are AMQP messages with specific formats:

```go
// Site Record
type SiteRecord struct {
    RecType   string
    Identity  string
    StartTime uint64
    EndTime   uint64
    Name      string
    Platform  string
}

// Router Record
type RouterRecord struct {
    RecType   string
    Identity  string
    Parent    string  // Site ID
    Name      string
    Mode      string  // interior/edge
}

// Link Record
type LinkRecord struct {
    RecType  string
    Identity string
    Parent   string  // Router ID
    Name     string
    Peer     string  // Remote router access
    Status   string  // up/down
}

// Flow Record (connection)
type FlowRecord struct {
    RecType    string
    Identity   string
    Parent     string
    SourceHost string
    DestHost   string
    Protocol   string
    BytesSent  uint64
    BytesRecv  uint64
}
```

### Event Streaming

**Location**: `pkg/vanflow/session/`

```go
type Client struct {
    connection messaging.Connection
    receiver   messaging.Receiver
    handlers   []RecordHandler
}

func (c *Client) Listen(ctx context.Context, address string) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            msg, err := c.receiver.Receive()
            if err != nil {
                return err
            }
            
            // Parse flow record from message
            record := parseFlowRecord(msg)
            
            // Dispatch to handlers
            for _, handler := range c.handlers {
                handler.OnRecord(record)
            }
            
            c.receiver.Accept(msg)
        }
    }
}
```

---

## Request/Response Pattern

**Location**: `internal/qdr/request.go`

Generic request/response pattern over AMQP.

```go
type Request struct {
    Address    string
    Type       string
    Version    string
    Properties map[string]interface{}
    Body       string
}

type Response struct {
    Type       string
    Version    string
    Properties map[string]interface{}
    Body       string
}

type RequestResponse interface {
    Request(request *Request) (*Response, error)
}

type RequestServer struct {
    pool    *AgentPool
    address string
    handler RequestResponse
}

func (s *RequestServer) Run(ctx context.Context) error {
    agent, err := s.pool.Get()
    if err != nil {
        return fmt.Errorf("Could not get management agent: %s", err)
    }
    defer agent.Close()
    
    receiver, err := agent.newReceiver(s.address)
    if err != nil {
        return fmt.Errorf("Could not open receiver for %s: %s", s.address, err)
    }
    
    for {
        err = s.serve(ctx, receiver, agent.anonymous)
        if err != nil {
            return fmt.Errorf("Error handling request for %s: %s", s.address, err)
        }
    }
}

func (s *RequestServer) serve(ctx context.Context, receiver *amqp.Receiver, 
                               sender *amqp.Sender) error {
    for {
        // Receive request
        requestMsg, err := receiver.Receive(ctx)
        if err != nil {
            return fmt.Errorf("Failed reading request: %s", err.Error())
        }
        
        // Parse request
        request := Request{
            Address: requestMsg.Properties.To,
            Type:    requestMsg.Properties.Subject,
        }
        for k, v := range requestMsg.ApplicationProperties {
            if k == "version" {
                request.Version = v.(string)
            } else {
                request.Properties[k] = v
            }
        }
        if body, ok := requestMsg.Value.(string); ok {
            request.Body = body
        }
        
        // Handle request
        response, err := s.handler.Request(&request)
        if err != nil {
            requestMsg.Reject(&amqp.Error{
                Condition:   amqp.ErrorInternalError,
                Description: err.Error(),
            })
            return err
        }
        
        // Send response
        requestMsg.Accept()
        responseMsg := amqp.Message{
            Properties: &amqp.MessageProperties{
                To:            requestMsg.Properties.ReplyTo,
                Subject:       response.Type,
                CorrelationID: requestMsg.Properties.CorrelationID,
            },
            ApplicationProperties: response.Properties,
            Value:                 response.Body,
        }
        
        err = sender.Send(ctx, &responseMsg)
        if err != nil {
            return fmt.Errorf("Could not send response: %s", err)
        }
    }
}
```

---

## AMQP Message Structure

### Message Components

```go
type Message struct {
    // Message Properties (AMQP 1.0 standard)
    Properties *MessageProperties
    
    // Application Properties (custom key-value pairs)
    ApplicationProperties map[string]interface{}
    
    // Message Body
    Value interface{}  // Can be string, map, list, etc.
}

type MessageProperties struct {
    MessageID     interface{}
    UserID        []byte
    To            string      // Destination address
    Subject       string      // Message type/operation
    ReplyTo       string      // Response address
    CorrelationID interface{} // Match request/response
    ContentType   string
    // ... other standard properties
}
```

### Management Request Message

```go
request := amqp.Message{
    Properties: &amqp.MessageProperties{
        To:            "$management",
        ReplyTo:       receiver.Address(),
        CorrelationID: uint64(1),
    },
    ApplicationProperties: map[string]interface{}{
        "operation":  "QUERY",
        "entityType": "io.skupper.router.connection",
    },
    Value: map[string]interface{}{
        "attributeNames": []string{"container", "role", "host"},
    },
}
```

### Management Response Message

```go
response := amqp.Message{
    Properties: &amqp.MessageProperties{
        To:            replyTo,
        CorrelationID: requestCorrelationID,
    },
    ApplicationProperties: map[string]interface{}{
        "statusCode":        200,
        "statusDescription": "OK",
    },
    Value: map[string]interface{}{
        "attributeNames": []string{"container", "role", "host"},
        "results": [][]interface{}{
            {"router-1", "inter-router", "10.0.0.1"},
            {"router-2", "edge", "10.0.0.2"},
        },
    },
}
```

---

## Error Handling

### Connection Errors

```go
agent, err := qdr.Connect("amqp://localhost:5672", nil)
if err != nil {
    // Connection failed
    // - Router not running
    // - Wrong address/port
    // - Network issues
    return fmt.Errorf("Failed to connect to router: %s", err)
}
defer agent.Close()
```

### Operation Errors

```go
err := agent.Create("io.skupper.router.connector", "my-connector", connector)
if err != nil {
    // Operation failed
    // - Entity already exists
    // - Invalid attributes
    // - Router rejected request
    return fmt.Errorf("Failed to create connector: %s", err)
}
```

### Timeout Handling

```go
ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
defer cancel()

err := sender.Send(ctx, &message)
if err != nil {
    // Timeout or send error
    return fmt.Errorf("Send timeout: %s", err)
}
```

---

## Testing AMQP Integration

### Mock AMQP Components

**Location**: `internal/messaging/mock.go`

```go
type Broker struct {
    addresses map[string]*multicast
}

type mockConnection struct {
    broker *Broker
    closed bool
}

func (c *mockConnection) Sender(address string) (Sender, error) {
    return &mockSender{connection: c, address: address}, nil
}

func (c *mockConnection) Receiver(address string, credit uint32) (Receiver, error) {
    r := &mockReceiver{
        connection: c,
        channel:    make(chan *amqp.Message, credit),
    }
    c.broker.subscribe(address, r)
    return r, nil
}
```

### Testing Router Management

```go
func TestAgentQuery(t *testing.T) {
    // Create mock broker
    broker := messaging.NewBroker()
    factory := messaging.NewMockFactory(broker)
    
    // Create agent
    agent, err := qdr.newAgent(factory)
    assert.NoError(t, err)
    defer agent.Close()
    
    // Mock router response
    broker.SendTo("$management", mockQueryResponse())
    
    // Test query
    results, err := agent.Query("io.skupper.router.connection", []string{})
    assert.NoError(t, err)
    assert.Equal(t, 2, len(results))
}
```

---

## Code Locations Reference

| Component | File | Purpose |
|-----------|------|---------|
| **Connection Factory** | `internal/qdr/messaging.go` | Create AMQP connections |
| **AMQP Wrappers** | `internal/qdr/messaging.go` | Wrap go-amqp types |
| **Agent** | `internal/qdr/amqp_mgmt.go` | Router management client |
| **Agent Pool** | `internal/qdr/amqp_mgmt.go` | Connection pooling |
| **Request/Response** | `internal/qdr/request.go` | Generic RPC pattern |
| **Flow Collection** | `internal/kube/adaptor/collector.go` | Network telemetry |
| **Flow Session** | `pkg/vanflow/session/` | Flow record streaming |
| **Messaging Interface** | `internal/messaging/messaging.go` | Abstract interfaces |
| **Mock Components** | `internal/messaging/mock.go` | Testing utilities |
| **Router State Handler** | `internal/nonkube/controller/router_state_handler.go` | Non-Kubernetes flow handling |

---

## Development Guidelines

### Adding New Management Operations

1. **Define entity type** in router
2. **Create Go struct** implementing `recordType`
3. **Add query method** to Agent
4. **Add create/update/delete** as needed
5. **Test with mock broker**

Example:
```go
type MyEntity struct {
    Name  string `json:"name"`
    Value string `json:"value"`
}

func (e MyEntity) toRecord() Record {
    return Record{
        "name":  e.Name,
        "value": e.Value,
    }
}

func (a *Agent) GetMyEntities() ([]MyEntity, error) {
    records, err := a.Query("io.skupper.router.myEntity", []string{})
    if err != nil {
        return nil, err
    }
    
    entities := make([]MyEntity, len(records))
    for i, r := range records {
        entities[i] = MyEntity{
            Name:  r.AsString("name"),
            Value: r.AsString("value"),
        }
    }
    return entities, nil
}
```

### Debugging AMQP Issues

1. **Enable AMQP tracing** in go-amqp library
2. **Check router logs** for management errors
3. **Verify addresses** are correct
4. **Test connectivity** with AMQP tools
5. **Check message format** matches router expectations

```bash
# Test AMQP connection
curl -u admin:admin http://localhost:8672/management

# Check router management interface
kubectl exec -it deployment/skupper-router -c router -- \
    qdmanage query --type=connection
```

---

## Related Documentation

- [Prerequisites](prerequisites.md) - Development environment setup
- [TLS Certificate Processing](tls-certificate-processing.md) - TLS with AMQP
- [AMQP 1.0 Specification](https://www.amqp.org/resources/specifications)
- [go-amqp Library](https://github.com/interconnectedcloud/go-amqp)

## Security Considerations

### Critical Security Requirements

1. **Inter-Site Links ALWAYS Require TLS**
   - **No exceptions**: All router-to-router connections across sites MUST use mutual TLS (mTLS)
   - Plain AMQP is **never** used for inter-site communication
   - The router enforces this requirement - cannot be disabled
   - See [TLS Certificate Processing](tls-certificate-processing.md) for details

2. **Local Connections Typically Use Plain AMQP**
   - Control plane to local router: `amqp://localhost:5672` (no TLS by default)
   - Flow collection from local router: Plain AMQP
   - Management operations on localhost: Plain AMQP
   - **Rationale**: Local connections are within the same pod/host, no network exposure
   - **However**: TLS can be enabled for local connections if needed (see below)

3. **SASL EXTERNAL for Inter-Site Authentication**
   - Certificate-based authentication for all inter-site links
   - Client certificate validated against server's trusted CA
   - Server certificate validated against client's trusted CA
   - Mutual authentication required

4. **Management Access Restrictions**
   - Management endpoint (`$management`) restricted to localhost by default
   - Remote management uses authenticated AMQP addresses
   - Requires proper routing through the network

5. **Flow Records Security**
   - May contain sensitive network topology information
   - May include IP addresses and connection patterns
   - Should be treated as confidential data
   - Access restricted to authorized components

### Why TLS is Optional in Code

The `ConnectionFactory` supports optional TLS configuration because:
- **Local connections** (localhost) don't need TLS by default
- **Inter-site connections** are handled by the router itself, not the control plane
- Control plane only manages the router via localhost
- **But TLS can be enabled** for local connections in specific scenarios

**The control plane never makes direct inter-site AMQP connections** - that's the router's job, and the router always uses TLS for inter-site links.

### Enabling TLS for Local Connections

While local connections typically use plain AMQP, you can enable TLS if needed:

#### 1. For Non-Kubernetes Deployments

The non-Kubernetes controller can use TLS for local connections:

```go
// internal/nonkube/controller/router_state_handler.go
tls := runtime.GetRuntimeTlsCert(h.Namespace, "skupper-local-client")
connFactory := qdr.NewConnectionFactory(url, tls)
```

**Certificate Structure:**
```go
type TlsCert struct {
    CaPath   string  // Path to CA certificate
    CertPath string  // Path to client certificate
    KeyPath  string  // Path to client key
    Verify   bool    // Whether to verify server certificate
}
```

**Default Paths** (non-Kubernetes):
- CA: `<namespace>/certs/skupper-local-client/ca.crt`
- Cert: `<namespace>/certs/skupper-local-client/tls.crt`
- Key: `<namespace>/certs/skupper-local-client/tls.key`

#### 2. For Kubernetes Deployments

**Current Architecture (Plain AMQP):**

In Kubernetes, the control plane (kube-adaptor) and router run in the same pod and communicate via localhost:

```
┌─────────────────────────────────────────────────────┐
│ skupper-router Pod                                  │
│                                                     │
│  ┌──────────────┐         ┌──────────────────────┐  │
│  │ router       │ :5672   │ kube-adaptor         │  │
│  │ container    │◄────────┤ container            │  │
│  │              │  plain  │                      │  │
│  │              │  AMQP   │ - config-sync        │  │
│  │              │         │ - flow-collector     │  │
│  └──────────────┘         └──────────────────────┘  │
│         │                                           │
│         │ Volume Mount: /etc/skupper-router-certs   │
│         │ (emptyDir - shared between containers)    │
│         └───────────────────────────────────────────┤
└─────────────────────────────────────────────────────┘
```

**How It Works:**

1. **Shared Volume Mount**
   - Both containers mount `/etc/skupper-router-certs` (emptyDir volume)
   - Certificates are written to this shared filesystem
   - No Kubernetes Secrets are mounted directly

2. **Init Container (config-init)**
   ```go
   // internal/kube/adaptor/config_init.go
   // Runs before main containers start
   // - Watches Kubernetes Secrets
   // - Syncs certificate data to /etc/skupper-router-certs
   // - Writes router config to skrouterd.json
   ```

3. **Secret-to-Filesystem Sync**
   ```go
   // internal/kube/secrets/sync.go
   // Monitors Secrets with skupper.io/type: connection-token
   // Extracts certificate data (ca.crt, tls.crt, tls.key)
   // Writes to /etc/skupper-router-certs/<profile-name>/
   ```

4. **Router Configuration**
   ```json
   {
     "sslProfiles": {
       "inter-router": {
         "name": "inter-router",
         "caCertFile": "/etc/skupper-router-certs/inter-router/ca.crt",
         "certFile": "/etc/skupper-router-certs/inter-router/tls.crt",
         "privateKeyFile": "/etc/skupper-router-certs/inter-router/tls.key"
       }
     },
     "listeners": [
       {
         "port": 5672,
         "role": "normal",
         "host": "localhost"
         // No sslProfile - plain AMQP for localhost
       },
       {
         "port": 55671,
         "role": "inter-router",
         "sslProfile": "inter-router"
         // TLS for inter-site connections
       }
     ]
   }
   ```

5. **Control Plane Connection**
   ```go
   // internal/kube/adaptor/collector.go:75
   factory := session.NewContainerFactory(
       "amqp://localhost:5672",  // Plain AMQP
       session.ContainerConfig{
           ContainerID: "kube-flow-collector"
           // No TLSConfig - plain connection
       }
   )
   ```

**Enabling TLS for Localhost (If Needed):**

To enable TLS for local connections in Kubernetes:

1. **Add TLS Listener to Router Config**
   ```go
   config.AddListener(Listener{
       Port:       5671,
       Host:       "localhost",
       Role:       "normal",
       SslProfile: "local-client",
   })
   ```

2. **Create SSL Profile for Local Client**
   ```go
   config.SslProfiles["local-client"] = qdr.ConfigureSslProfile(
       "local-client",
       qdr.SSL_PROFILE_PATH,  // /etc/skupper-router-certs
       true,                   // clientAuth required
   )
   ```

3. **Create Kubernetes Secret**
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: skupper-local-client
     labels:
       skupper.io/type: connection-token
   data:
     ca.crt: <base64-encoded-ca>
     tls.crt: <base64-encoded-cert>
     tls.key: <base64-encoded-key>
   ```

4. **Update Control Plane Connection**
   ```go
   import "crypto/tls"
   
   // Load certificates from shared volume
   cert, _ := tls.LoadX509KeyPair(
       "/etc/skupper-router-certs/local-client/tls.crt",
       "/etc/skupper-router-certs/local-client/tls.key",
   )
   caCert, _ := os.ReadFile("/etc/skupper-router-certs/local-client/ca.crt")
   caCertPool := x509.NewCertPool()
   caCertPool.AppendCertsFromPEM(caCert)
   
   tlsConfig := &tls.Config{
       Certificates: []tls.Certificate{cert},
       RootCAs:      caCertPool,
   }
   
   factory := session.NewContainer(
       "amqps://localhost:5671",
       session.ContainerConfig{
           ContainerID: "kube-flow-collector",
           TLSConfig:   tlsConfig,
           SASLType:    session.SASLTypeExternal,
       }
   )
   ```

**Key Differences from Non-Kubernetes:**

| Aspect | Kubernetes | Non-Kubernetes |
|--------|-----------|----------------|
| Certificate Storage | Kubernetes Secrets → emptyDir volume | Filesystem directly |
| Sync Mechanism | Init container + Secret watcher | Direct filesystem access |
| Volume Type | emptyDir (ephemeral) | Host filesystem or volume |
| Certificate Path | `/etc/skupper-router-certs` | `<namespace>/certs/` |
| Default Connection | Plain AMQP (same pod) | Can use TLS (different processes) |

#### 3. Router Configuration for TLS Listener

To accept TLS connections on localhost, configure the router:

```yaml
# Router listener with TLS
listeners:
  - name: local-tls
    host: localhost
    port: 5671
    role: normal
    sslProfile: local-ssl
    authenticatePeer: true
    saslMechanisms: EXTERNAL

sslProfiles:
  - name: local-ssl
    caCertFile: /etc/skupper-router-certs/ca.crt
    certFile: /etc/skupper-router-certs/tls.crt
    privateKeyFile: /etc/skupper-router-certs/tls.key
```

#### 4. When to Use TLS for Local Connections

**Use TLS when:**
- Security policy requires encryption for all connections
- Running in a multi-tenant environment
- Local connection crosses security boundaries
- Compliance requirements mandate encryption

**Plain AMQP is sufficient when:**
- Connection is truly localhost (same pod/container)
- No security boundary is crossed
- Performance is critical (TLS adds overhead)
- Default Skupper deployment (most common case)

#### 5. Code Locations

**TLS Configuration:**
- `internal/nonkube/client/runtime/certs.go` - TLS certificate loading
- `pkg/vanflow/session/container.go` - Container TLS config (lines 71-93)
- `internal/qdr/qdr.go` - Router SSL profile configuration

**Usage Examples:**
- `internal/nonkube/controller/router_state_handler.go:174` - Non-Kubernetes TLS
- `internal/kube/adaptor/collector.go:75` - Kubernetes plain AMQP (default)

### TLS Configuration Best Practices

1. **Certificate Management**
   - Use separate certificates for local vs. inter-site connections
   - Rotate certificates regularly
   - Store private keys securely

2. **Verification**
   - Always verify server certificates (`Verify: true`)
   - Use proper CA certificates, not self-signed certs in production
   - Set `InsecureSkipVerify: false` in production

3. **Performance**
   - Plain AMQP is faster for localhost connections
   - TLS adds ~10-20% overhead for local connections
   - Consider performance vs. security requirements

4. **Debugging**
   - Use `openssl s_client` to test TLS connections
   - Check router logs for TLS handshake errors
   - Verify certificate paths and permissions