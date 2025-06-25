# Golang Load Balancer: Technical Documentation

## 1. Project Overview

This project is a Go-based simulation of a Layer 4 (Transport Layer) load balancer. It is designed to distribute traffic from multiple clients to a set of backend servers, demonstrating core load balancing concepts in a controlled environment.

The primary purpose of this load balancer is to serve as an educational and experimental platform for network engineers and developers interested in load balancing technologies. It provides a foundation for implementing and testing various algorithms, connection management strategies, and resiliency patterns.

### Supported Modes

*   **NAT (Network Address Translation) Mode**: The load balancer rewrites the destination IP address of incoming packets to a selected backend server's IP. The source IP remains the client's IP. This is the currently implemented mode.

### Target Use Cases

*   **Educational Tool**: For learning about the internals of a load balancer.
*   **Algorithm Prototyping**: A framework for developing and testing new load-balancing algorithms.
*   **Concurrency and Systems Programming Demo**: Demonstrates advanced Go concurrency patterns for building high-performance systems.

### System Requirements

As this is a simulation, it does not have strict network requirements like kernel parameter tuning. It can be run on any standard machine with the Go toolchain installed.

---

## 2. Architecture

### High-Level System Architecture

The system is composed of four main components that communicate via Go channels, simulating the flow of network packets.

```
+----------------+      (1)      +-----------------+      (2)      +----------------+
|    Clients     | <-----------> |  Load Balancer  | <-----------> |    Servers     |
+----------------+               +-----------------+               +----------------+
                                        |
                                        | (3)
                                        |
                               +-----------------+
                               |   HTTP/WS Hub   |
                               +-----------------+
                                        |
                                        | (4)
                               +-----------------+
                               |  Web Dashboard  |
                               +-----------------+
```

1.  **Clients to Load Balancer**: Clients initiate connections and send streams of packets to the load balancer's "frontend."
2.  **Load Balancer to Servers**: The load balancer applies its algorithm, performs NAT, and forwards packets to the selected backend "backend."
3.  **State Reporting**: The load balancer periodically sends its internal state to a WebSocket Hub.
4.  **Dashboard**: A web-based dashboard can connect to the Hub to visualize the load balancer's state in real-time.

### Core Components

*   **Packet Model (`model/packet.go`)**: A struct representing a network packet, containing source/destination IPs and ports, flags (SYN, ACK, FIN), and a payload. This is the primary unit of data transfer in the simulation.
*   **Clients (`client/client.go`)**: Goroutines that simulate user clients. Each client has its own IP and port, and sends a predefined stream of packets.
*   **Servers (`server/server.go`)**: Goroutines that simulate backend servers. They "listen" for packets from the load balancer and can be put into a "killed" state to simulate failures.
*   **LoadBalancer (`balancer/balancer.go`)**: The central component. It maintains a table of active servers and clients, applies a load-balancing algorithm, and manages the lifecycle of connections.
*   **Algorithms (`algorithms/`)**: Pluggable modules for server selection. Currently, `RoundRobin` is implemented.
*   **HTTP/WebSocket Hub (`http/hub.go`)**: A concurrent hub that manages WebSocket connections and broadcasts the load balancer's state to all connected web clients.

### Communication and Concurrency

The system is heavily reliant on Go's concurrency primitives:

*   **Goroutines**: Every client and server runs in its own goroutine, allowing for high concurrency. The WebSocket hub and the main load balancer ticker also run in dedicated goroutines.
*   **Channels**: Communication between components is handled exclusively through channels, avoiding the need for mutexes in most of the core logic. Each client and server has `DataIn` and `DataOut` channels for sending and receiving `model.Packet` structs.

### Simulating Network Communication with Go Channels

A key feature of this project is the accurate emulation of TCP packet flow over a network, using Go channels as a substitute for actual network sockets. This architecture correctly models the independent request/response paths and the central role of the load balancer.

Here is a high-level overview of how it works:

1.  **Client and Server Channels**:
    *   Each **Client** has an `OutChannel` (to send packets) and an `InChannel` (to receive responses).
    *   Each **Server** has a `DataIn` channel (to receive packets) and a `DataOut` channel (to send responses).

2.  **The Load Balancer as a Virtual Switchboard**: The `LoadBalancer` sits in the middle, managing the flow of packets between all clients and servers without them being directly connected.
    *   **Client to Server Flow**:
        *   The load balancer continuously listens to the `OutChannel` of every connected client.
        *   When a packet is sent by a client, the load balancer receives it.
        *   It inspects the packet and uses its internal state (connection table and balancing algorithm) to determine which backend server should receive it.
        *   It then forwards the packet to the appropriate server's `DataIn` channel. This simulates a packet being sent from a client, through the load balancer, to a backend.
    *   **Server to Client Flow**:
        *   Simultaneously, the load balancer listens to the `DataOut` channel of every registered server.
        *   When a server sends a response packet, the load balancer receives it.
        *   It looks at the packet's key to identify the original client from its connection table.
        *   It then forwards the response packet to that specific client's `InChannel`. This simulates the response from the backend returning to the correct client via the load balancer.

This channel-based design faithfully reproduces the logical flow of a load-balanced network connection, allowing for the realistic modeling of handshakes, data transfer, and connection management.

### Flow of a Typical Packet

1.  A **Client** creates a packet with a `SYN` flag and sends it into its `OutChannel`.
2.  The **Load Balancer**'s listener goroutine picks up the packet from the client's `OutChannel`. It uses the configured **Algorithm** (e.g., Round Robin) to select a healthy **Server**.
3.  It creates a new entry in its connection table, mapping the `client_ip:client_port` to the selected `server_ip:server_port`.
4.  It performs NAT by changing the packet's destination IP to the server's IP and forwards it to the selected server's `DataIn` channel.
5.  The **Server** receives the packet from its `DataIn` channel, processes it, and sends a response packet back into its `DataOut` channel.
6.  The **Load Balancer**'s backend listener picks up the response, looks up the connection in its table to find the original client, reverses the NAT, and forwards the packet to the client's `InChannel`.
7.  The client's listening goroutine receives the packet from its `InChannel`, completing the loop. This process continues for data transfer (`ACK` packets) and connection teardown (`FIN` packets).

---

## 3. Protocol Design

The project uses a custom, simulated transport protocol that mimics TCP. All communication between clients, the load balancer, and servers is done via `model.Packet` objects passed through channels.

### Packet Structure

The fundamental unit of data is the `model.Packet`:

```go
type Packet struct {
    Key           ClientKey     // Unique identifier for the connection
    Flag          Flag          // TCP-like flags (SYN, ACK, FIN, etc.)
    Size          int           // Packet size in bytes
    Delay         time.Duration // Simulated network delay
    PlaceInStream uint32        // Sequence number for ordering
}

type ClientKey struct {
    SrcIP    string
    SrcPort  int16
    DstIP    string
    DstPort  int16
    Protocol Protocol // e.g., "TCP"
}
```

### Flags

The `Flag` type is a bitmask that represents the state and purpose of a packet, mirroring standard TCP flags.

*   `FlagSYN`: Initiates a connection.
*   `FlagACK`: Acknowledges receipt of a packet.
*   `FlagFIN`: Terminates a connection.
*   `FlagRST`: Resets a connection in case of an error.
*   `FlagPSH`: Pushes data to the receiver.
*   `FlagURG`: Indicates urgent data (not currently used in logic).

### Handshake and Data Transfer

The connection lifecycle follows a simplified TCP-like state machine:

1.  **Connection Establishment (3-Way Handshake)**:
    *   **Client -> LB -> Server**: The client sends a packet with the `SYN` flag.
    *   **Server -> LB -> Client**: The server (or LB on its behalf) replies with `SYN|ACK`.
    *   **Client -> LB -> Server**: The client sends an `ACK`, establishing the connection. Data transfer can now begin.
    *   A `SYN_TIMEOUT` of 500ms is in place for waiting for a `SYN|ACK`.

2.  **Data Transfer**:
    *   Data is sent in segments, which are packets with the `PSH` flag. The default segment size is `1024` bytes.
    *   Each data packet must be acknowledged by the receiver with an `ACK` packet.
    *   The system has a `SEGMENT_TIMEOUT` of 700ms. If an `ACK` is not received within this period, the sender will retransmit the packet up to `MAX_RETRANSMISIONS` (3 times).

3.  **Connection Teardown**:
    *   The party wishing to close the connection sends a `FIN` packet.
    *   The other party responds with a `FIN|ACK`.
    *   The first party sends a final `ACK` to confirm, and the connection is closed.

### Connection State Machine

Below is a simplified diagram of the client/server state transitions.

```
              +--------------+
              |    CLOSED    |
              +--------------+
                   |  (send SYN)
                   v
              +--------------+
              |   SYN_SENT   |
              +--------------+
                   |  (receive SYN|ACK)
                   v
              +--------------+
              | ESTABLISHED  |
              +--------------+
                   |  (send FIN)
                   v
              +--------------+
              |   FIN_WAIT   |
              +--------------+
                   |  (receive FIN|ACK)
                   v
              +--------------+
              |    CLOSED    |
              +--------------+
```

---

## 4. Load Balancing Algorithms

The load balancer's logic for selecting a backend server is encapsulated in algorithms that implement the `Algorithm` interface. This allows for easy extension with new custom algorithms.

### The `Algorithm` Interface

Any load balancing algorithm must implement the following interface:

```go
type Algorithm interface {
    Pick([]*server.Server) (*server.Server, error)
}
```

The `Pick` method receives a slice of available, healthy servers and returns the selected server or an error if no servers are available.

### Supported Algorithms

#### Round Robin

*   **Implementation**: `algorithms/round-robin.go`
*   **Design**: This is a stateful, thread-safe algorithm. It maintains an internal counter and, for each call to `Pick`, it increments the counter and returns the server at the new index (`index % number_of_servers`). A mutex (`sync.Mutex`) is used to prevent race conditions when multiple goroutines access the counter simultaneously.
*   **Use Case**: Distributes load evenly across all servers in a sequential manner. It's most effective when servers have similar capacities.

#### Random Choice

*   **Implementation**: `algorithms/algorithms.go`
*   **Design**: This is a stateless algorithm. It simply picks a random server from the list of available backends on each call to `Pick`.
*   **Use Case**: Provides good load distribution with very low overhead. It can lead to imbalanced loads over the short term due to statistical randomness. (Note: This algorithm is implemented but not used in the default `main.go` configuration).

### Future Work & Planned Algorithms

The following algorithms are on the roadmap:

*   **Least Connections**: Select the server with the fewest active connections. Requires the load balancer to track connection counts for each server.
*   **Power of Two Choices (P2C)**: Pick two servers at random and choose the one with the fewer connections. A good balance between performance and load distribution.
*   **Weighted Load Balancing**: Assign a weight to each server (e.g., based on capacity) and distribute load proportionally.
*   **Adaptive (Dynamic Scoring)**: Develop a custom scoring function based on server health, latency, and connection count.

---

## 5. Connection Management

The load balancer is responsible for tracking every connection to ensure that packets are correctly routed between clients and their designated backend servers.

### Connection Table Structure

The core of connection management is the connection table, defined as:

```go
connTable map[*server.Server][]*client.Client
```

This map tracks which clients are connected to which server. When a packet arrives from a client, the load balancer must first determine which server it belongs to. This is done by iterating through the map's values, which has performance implications for a large number of connections.

A secondary map is used for idle timeout handling:

```go
clientLastActivity map[model.ClientKey]time.Time
```

This map stores a timestamp of the last seen packet for each unique client connection, identified by its `ClientKey`.

### Mapping Strategy

1.  When a client sends its initial `SYN` packet, the load balancer's `Route` function is invoked.
2.  The configured load balancing `Algorithm` (e.g., Round Robin) is used to `Pick` an available backend server.
3.  Once a server is selected, the client is added to the list of clients for that server in the `connTable`.
4.  All subsequent packets from that client's unique key (`SrcIP:SrcPort`) are then forwarded to the same server, ensuring session persistence.

### Idle Timeout Handling

The system has a mechanism to clean up stale or idle connections, although it is **currently disabled** in the code (`startIdleConnectionManager` is commented out).

*   **Concept**: A background goroutine would periodically scan the `clientLastActivity` map.
*   **Timeouts**: Two timeouts are defined:
    *   `HalfOpenTimeout` (2s): For connections that have not completed the handshake.
    *   `EstablishedTimeout` (2s): For established connections with no traffic.
*   **Cleanup**: If the time since the last activity for a connection exceeds the relevant timeout, its entry would be removed from the connection table.

### Connection Draining and Shutdown

*   **Server Removal**: When a server is removed (e.g., via `RemoveServer` or a failed health check), its entry in the connection table is deleted, and all active connections to it are immediately dropped.
*   **Graceful Shutdown**: The current implementation does not support graceful connection draining, where a server can be marked for removal but allowed to finish its active connections. This is a potential area for future enhancement.

---

## 6. Security

While this project is a simulation, it includes a basic security measure to protect backend servers from being overwhelmed by connection requests. More advanced features are on the roadmap.

### SYN Flood Protection

The primary defense against a SYN flood-style attack is limiting the number of "half-open" connections a backend server can have.

*   **Mechanism**: Each `server.Server` has a `MaxHalfOpen` parameter. When the load balancer receives a `SYN` packet for a server, it checks the server's current `HalfOpenConnections` counter.
*   **Action**: If the number of half-open connections (those that have sent a `SYN` but not completed the handshake) is at or above the limit, the new `SYN` packet is dropped. This prevents the server's connection queue from being exhausted by malicious or malfunctioning clients.
*   **Implementation**: This check is performed in the `ListenOnClientEmission` goroutine within `balancer/balancer.go`.

### Future Security Measures

The following security enhancements are planned:

*   **SYN Cookies**: To avoid storing any state for `SYN` requests until the client's address is verified. This is a more robust defense against SYN floods.
*   **Rate Limiting**: Implementing a token bucket or similar algorithm to limit the rate of incoming connections or packets from a single source IP.
*   **Packet Validation**: Stricter checks on packet fields to ensure they are well-formed.

---

## 7. Metrics and Monitoring

The load balancer provides real-time monitoring of its internal state, which is crucial for observing traffic flow and diagnosing issues. The primary mechanism is a WebSocket-based broadcast of the system's state.

### State Broadcast via WebSocket

*   **Mechanism**: At a configurable interval (`RefreshInterval`), the load balancer's main ticker function calls the `Tick()` method. This method captures a snapshot of the current state.
*   **Hub**: The state object is then passed to the `http.Hub`, which serializes it to JSON and broadcasts it to all connected WebSocket clients (e.g., a web dashboard).
*   **Endpoint**: A web client can connect to the WebSocket endpoint to receive this stream of state updates in real-time.

### Collected Metrics

The following metrics are collected and broadcast in each update.

#### Load Balancer State (`model.LoadBalancerState`)

*   `mode`: The current load balancing mode (e.g., "NAT").
*   `algorithm`: The name of the current load balancing algorithm.

#### Per-Server State (`model.ServerState`)

*   `ID`, `Name`, `Address`, `Port`: Server identifiers.
*   `Active`: A boolean indicating if the server is passing health checks.
*   `ActiveConnections`: The number of fully established connections.
*   `HalfOpenConnections`: The number of connections waiting to complete the handshake.
*   `LatencyP95Milli`: The 95th percentile of health-check probe latency, in milliseconds.
*   `NumberOfHandledPackets`: A counter for all packets processed by the server.

#### Per-Client State (`model.ClientState`)

*   `Key`: The unique `ClientKey` of the connection.
*   `RetrySyn`: The number of `SYN` retransmissions for this client.
*   `RetryData`: The number of data packet retransmissions.

### Future Work

*   **Prometheus Integration**: The long-term plan is to expose these metrics via a `/metrics` endpoint that a Prometheus server can scrape. This would involve mapping the existing state objects to Prometheus metric types (Gauges, Counters) and providing labels for dimensions like `server_id`.
*   **Latency Sampling**: While p95 latency is available for health checks, more detailed latency sampling for actual packet round-trips could be added.

---

## 8. Testing

As the project currently lacks a dedicated test suite, this section outlines a recommended strategy for ensuring correctness, stability, and performance.

### Unit Testing

Unit tests should be the foundation of the testing pyramid, focusing on individual components in isolation.

*   **Algorithms (`algorithms/`)**: Each algorithm should be tested with various inputs.
    *   **Round Robin**: Verify that it cycles through backends correctly and handles an empty list of servers.
    *   **Random**: Test that it returns a valid server from the list and handles an empty list.
*   **Packet Logic (`model/`)**: Test flag manipulation (e.g., setting and checking `SYN`, `ACK` flags).
*   **Client/Server State Machines**: Test the connection logic in `client/client.go` and `server/server.go` by sending packets and asserting the state transitions.

Example test for Round Robin:
```go
// in algorithms/round_robin_test.go
func TestRoundRobin(t *testing.T) {
    rr := &RoundRobin{}
    servers := []*server.Server{
        {ID: "srv-1"},
        {ID: "srv-2"},
    }

    s, _ := rr.Pick(servers)
    assert.Equal(t, "srv-1", s.ID)

    s, _ = rr.Pick(servers)
    assert.Equal(t, "srv-2", s.ID)

    s, _ = rr.Pick(servers)
    assert.Equal(t, "srv-1", s.ID)
}
```

### Integration Testing

Integration tests will verify that the core components work together correctly. The main simulation in `main.go` serves as a basic integration test, but more structured tests are needed.

*   **Test Scenario**: Create a test that initializes a `LoadBalancer`, one or more `client` instances, and a few `server` instances.
*   **Verification**:
    *   Assert that the client successfully completes its handshake and data transfer.
    *   Check the load balancer's connection table to ensure the mappings are correct.
    *   Verify that packets are distributed across servers according to the chosen algorithm.

### Load Testing

To understand the performance characteristics of the load balancer, a load testing setup is essential.

*   **Setup**: The existing simulation can be configured with a high number of clients and servers (`nClients`, `nServers` in `main.go`).
*   **Metrics**: Monitor the WebSocket state for:
    *   Packet processing throughput.
    *   Growth in p95 latency under load.
    *   Error rates or dropped packets.
*   **Goal**: Identify performance bottlenecks, such as contention on the load balancer's mutex or inefficiencies in the connection table lookup.

### Concurrency and Race Condition Testing

Given the highly concurrent nature of the project, testing for race conditions is critical.

*   **`-race` flag**: All tests should be run with the Go race detector enabled: `go test -race ./...`. This will identify any data races in the code.

---

## 9. Configuration

Currently, the primary parameters for the simulation are hardcoded as constants in `main.go`.

```go
const (
    nClients     = 100
    nServers     = nClients / 10
    packetsCount = 5000
    gap          = 100 * time.Millisecond
)
```

The load balancer itself is configured programmatically:

```go
func setupLoadBalancer() *balancer.LoadBalancer {
    return balancer.NewLoadBalancer(
        model.LBModeNAT,
        &algorithms.RoundRobin{},
        time.Millisecond*500, // RefreshInterval
    )
}
```

### Future Work: Configuration File

For a production-grade system, these parameters should be externalized into a configuration file (e.g., `config.yaml`). This would allow for easier modification without recompiling the code.

Example `config.yaml`:
```yaml
mode: "NAT"
algorithm: "RoundRobin"
listen_port: 80
refresh_interval: "500ms"

servers:
  - name: "backend-01"
    address: "10.0.0.10:80"
  - name: "backend-02"
    address: "10.0.0.11:80"

timeouts:
  half_open: "2s"
  established: "2s"
```

---

## 10. Deployment

As this is a self-contained simulation, deployment is straightforward.

### Build Instructions

The project can be built into a single binary using the standard Go toolchain.

```bash
# Build the binary
go build -o load-balancer

# Run the simulation
./load-balancer
```

For production builds, consider using flags to strip debug information and reduce binary size:
```bash
go build -ldflags="-s -w" -o load-balancer
```

### Containerization (Future Work)

A `Dockerfile` could be created to package the application as a container image. This simplifies distribution and ensures a consistent runtime environment.

Example `Dockerfile`:
```dockerfile
# Use a multi-stage build for a small final image
FROM golang:1.19-alpine AS builder

WORKDIR /app
COPY . .
RUN go build -ldflags="-s -w" -o /load-balancer

# Final stage
FROM alpine:latest
COPY --from=builder /load-balancer /load-balancer
# The web dashboard could be served from here too
# COPY --from=builder /app/http/static /static
# EXPOSE 8080 
CMD ["/load-balancer"]
```

---

## 11. Code Structure

The project is organized into several packages, each with a distinct responsibility.

*   **`main.go`**: The entry point of the application. It initializes all components and starts the simulation.
*   **`model/`**: Contains the core data structures and constants used throughout the project (`Packet`, `ClientKey`, `Flag`, etc.). It has no external dependencies.
*   **`client/`**: Defines the `Client` type, which simulates a user sending packets.
*   **`server/`**: Defines the `Server` type, which simulates a backend server that processes packets.
*   **`balancer/`**: Contains the main `LoadBalancer` logic, including connection management, packet routing, and health checks.
*   **`algorithms/`**: Defines the `Algorithm` interface and concrete implementations like `RoundRobin`.
*   **`http/`**: Implements the WebSocket hub (`hub.go`) for broadcasting state and the simple HTTP server (`server.go`) to serve the WebSocket connection.

### Extending with New Algorithms

To add a new load balancing algorithm:
1.  Create a new file in the `algorithms/` package (e.g., `least_connections.go`).
2.  Define a struct for your algorithm.
3.  Implement the `Pick([]*server.Server) (*server.Server, error)` method on your struct.
4.  Instantiate your new algorithm in `main.go` when creating the `LoadBalancer`.

---

## 12. Troubleshooting and Debugging

### Interpreting Logs

The application prints simple logs to standard output for key events.

*   `CHAOS: Killed server srv-XX`: The chaos engineering ticker has randomly stopped a server. This is expected.
*   `ALL STREAMS COMPLETED`: The simulation has finished successfully, with all clients completing their packet streams.
*   "no backends": The `Pick` method was called when no servers were available or healthy. This can happen if all servers are killed by the chaos function.

### Debugging with the Web Dashboard

The most powerful debugging tool is the web dashboard connected to the WebSocket stream.
*   **Monitor Server State**: Watch the `Active` flag on servers. If a server goes inactive, it means its health check is failing.
*   **Check Connections**: Observe the `ActiveConnections` and `HalfOpenConnections` counters to see how load is being distributed. High `HalfOpenConnections` may indicate a problem with the handshake.
*   **Latency**: If `LatencyP95Milli` spikes for a server, it indicates network or processing delays in the simulation.

### Enabling Verbose Mode

Currently, there is no explicit verbose mode. To add more detailed logging, you can uncomment the `fmt.Println` statements within the `balancer` and `client`/`server` loops.

---

## 13. Future Work and Roadmap

This section consolidates the planned features and improvements for the project.

### Core Features

*   **Additional Algorithms**: Implement `LeastConnections`, `Power of Two Choices`, and `Weighted` load balancing.
*   **Connection Draining**: Implement graceful shutdown for servers to allow them to finish active connections before removal.
*   **SYN Cookies**: Implement SYN cookies for robust SYN flood protection.
*   **Rate Limiting**: Add IP-based rate limiting to prevent abuse.
*   **Circuit Breakers**: Automatically detect and temporarily remove failing or slow backends from the rotation.
*   **Connection Pooling**: Pre-warm connections to backends to reduce latency on the first request.

### Advanced Features

*   **Adaptive Load Balancing**: Create a dynamic scoring system for backends based on latency, errors, and connection count.
*   **Sticky Sessions**: Implement session affinity to route a client to the same server for the duration of their session.
*   **Geo-based Routing**: In a multi-region setup, route clients to the geographically closest backend.
*   **Pluggable Policy Engine**: Allow routing decisions to be customized with a scripting language like Lua or Rego.

### Observability

*   **Prometheus Integration**: Expose all metrics via a `/metrics` endpoint.
*   **Distributed Tracing**: Add support for distributed tracing (e.g., OpenTelemetry) to follow a packet's journey through the system.

### Deployment and Configuration

*   **External Configuration**: Move all configurable parameters to a YAML or JSON file with hot-reload support.
*   **Official Docker Image**: Publish a container image on Docker Hub.

---

## 14. Core Concepts Explained

This section provides a high-level overview of the fundamental networking and system design concepts that this simulator aims to model.

### What is a Load Balancer?

A load balancer is a critical component in modern distributed systems. It acts as a "traffic cop" for requests coming from clients, distributing them across a pool of backend servers. The primary goals are to:
*   **Improve Availability**: If one server fails, the load balancer can redirect traffic to the remaining healthy servers, preventing an outage.
*   **Increase Scalability**: As traffic grows, new servers can be added to the pool without clients needing to be aware of them.
*   **Enhance Performance**: By distributing the load, it ensures no single server is overwhelmed, leading to faster response times for users.

This project simulates a Layer 4 (Transport Layer) load balancer, which operates on network information like IP addresses and ports (TCP/UDP), without inspecting the content of the data packets.

### Health Checking

A load balancer needs to know which backend servers are healthy and able to accept traffic. It determines this through **health checks**.
*   **How it Works**: The load balancer periodically sends a small, lightweight request (a "probe") to each server to check its status. If the server responds correctly within a timeout, it's considered healthy. If it fails to respond or sends an error, it's marked as unhealthy and temporarily removed from the pool of available servers.
*   **In this Simulator**: The `balancer/balancer.go` component has a `startHealthChecker` goroutine for each server. It sends a probe and waits for a `probeAck`. The time it takes for this round trip is measured to calculate latency, and failures lead to the server being marked as inactive.

### SYN Flooding Attacks

A **SYN flood** is a type of Denial-of-Service (DoS) attack that exploits the TCP three-way handshake.
*   **The Attack**: An attacker sends a high volume of `SYN` packets (the first step in a TCP connection) to a target server, often from spoofed (fake) IP addresses. The server responds with `SYN|ACK` and allocates resources to wait for the final `ACK` from the client. Because the client is fake, the final `ACK` never arrives, and the server's connection table fills up with these "half-open" connections, eventually preventing it from accepting legitimate requests.
*   **In this Simulator**: You can simulate this by having a large number of clients that only send a `SYN` packet and never complete the handshake. The `MaxHalfOpen` parameter in the `server` model is a direct mitigation strategy for this, preventing a server from using up all its resources on half-open connections.

### SYN Cookies

**SYN cookies** are a technique used to mitigate SYN flood attacks.
*   **How it Works**: Instead of storing state about a new connection upon receiving a `SYN` packet, the server sends a special `SYN|ACK` response where the sequence number is a cryptographically generated "cookie" containing encoded information about the connection (`source IP`, `port`, etc.). The server then discards the `SYN` packet and stores no state. If the client is legitimate, it will respond with an `ACK` containing the incremented cookie. The server can then validate the cookie from the `ACK` packet and fully establish the connection. This avoids allocating memory for potentially malicious requests.
*   **In this Simulator**: SYN cookies are listed as a "Future Work" item. Implementing them would involve changing the handshake logic to not store state in the connection table until a valid `ACK` with a verified cookie is received.

### Rate Limiting

Rate limiting is a technique to control the amount of incoming traffic from a client or to a service.
*   **How it Works**: A system sets a threshold on how many requests a client can make in a given time window. If the client exceeds this limit, their subsequent requests are either dropped, delayed, or they receive an error message. This is crucial for preventing abuse, ensuring fair resource usage, and mitigating DoS attacks.
*   **In this Simulator**: Rate limiting is a planned feature. It could be implemented in the `ListenOnClientEmission` function by tracking the number of packets received from each `ClientKey` within a time window.

### Chaos Engineering (Random Kills)

**Chaos engineering** is the practice of intentionally injecting failures into a system to test its resilience and identify weaknesses before they cause real-world outages.
*   **In this Simulator**: The `runTicker` function in `main.go` contains a "Chaos" section.
    *   `lb.KillRandomServer()`: This simulates a sudden server failure, like a power cut, hardware failure, or process crash. The goal is to verify that the load balancer's health checks quickly detect the failure and correctly redirect traffic to the remaining healthy servers.
    *   `lb.KillRandomClient()`: This simulates an abrupt client-side disconnection. This helps test how the load balancer and server handle unexpected connection teardowns and resource cleanup.

By modeling these concepts, the simulator provides a practical, hands-on environment for understanding how production-grade load balancers and distributed systems are designed to be resilient and secure.