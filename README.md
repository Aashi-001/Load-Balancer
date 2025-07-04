# Load-Balancer

A concurrent HTTP load balancer built in Go with multiple algorithms, health monitoring, and thread-safe connection tracking.

## Features

- **Multiple Load Balancing Algorithms**: Round Robin, Least Connections, Random
- **Automatic Health Checks**: Background monitoring of backend server health
- **Thread-Safe Connection Tracking**: Atomic operations for race-free active connection counting  
- **YAML Configuration**: Backend management and algorithm selection
- **Concurrent Request Handling**: Built on Go's goroutine model
- **Graceful Error Handling**: Returns 503 when no healthy backends available
- **SQLite Logging**: Stores request & health check data in `lb_logs.db`

## Quick Start

### 1. Create Configuration File

Create `configs/config.yaml`:

```yaml
server:
  host: "localhost"
  port: "8080"

backends:
  - "http://localhost:9000"
  - "http://localhost:9001" 
  - "http://localhost:9002"

algorithm: "leastconn"  # Options: roundrobin, leastconn, random

health_check:
  interval: 30s
  timeout: 5s
  path: "/health"
```

### 2. Run the Load Balancer

```bash
go run main.go
```

### 3. Start Backend Servers

```bash
go run create_servers.go
```

### 4. Test Load Balancing

```bash
curl http://localhost:8080
```

## Load Balancing Algorithms

### Round Robin (`roundrobin`)
Cycles through healthy backends sequentially using atomic counter operations.

### Least Connections (`leastconn`) 
Routes requests to the backend with the fewest active connections. Displays current connection counts for debugging.

### Random (`random`)
Randomly selects from available healthy backends.

### SQLite Logging
- The load balancer automatically creates lb_logs.db and logs:

    |Table | What it stores|
    |------|---------------|
    |request_logs | Client IP, path, backend, latency, status code |
    |health_check_logs | Backend health status & response time |

- Inspect logs anytime using sqlite3:

    ```
    sqlite3 lb_logs.db
    ```
    ```
    .tables
    SELECT * FROM request_logs ORDER BY timestamp DESC LIMIT 5;
    SELECT backend, AVG(response_time_ms) FROM request_logs GROUP BY backend;
    ```

<!-- ## Implementation Details

### Thread-Safe Connection Tracking
```go
func (s *simpleServer) increment() {
    atomic.AddInt32(&s.activeConns, 1)
    atomic.AddInt64(&s.requests, 1)
}

func (s *simpleServer) decrement() {
    atomic.AddInt32(&s.activeConns, -1)
}
```

### Concurrent Health Monitoring
Health checks run every 5 seconds in background goroutines:

```go
go func() {
    for {
        for _, s := range lb.servers {
            go s.(*simpleServer).checkHealth()
        }
        time.Sleep(5 * time.Second)
    }
}()
```

### Health Check Implementation
- Sends GET request to `{backend_url}/health`
- 2-second timeout per check
- Marks backend as alive/dead based on HTTP 200 response

## Configuration

| Field | Description | Implementation |
|-------|-------------|----------------|
| `server.host` | Load balancer host | Read but not used |
| `server.port` | Load balancer port | ✅ Used |
| `backends` | Backend server URLs | ✅ Used |
| `algorithm` | Load balancing strategy | ✅ Used |
| `health_check.*` | Health check settings | Read but hardcoded values used |

## Current Limitations

- Health check interval hardcoded to 5 seconds (config ignored)
- Health check timeout hardcoded to 2 seconds (config ignored)
- Health endpoint fixed to `/health` (config ignored)
- Request counter incremented but not exposed
- Some debug print statements still present
- Basic logging only

## Technical Architecture

**Concurrency Model:**
- HTTP server automatically handles each request in separate goroutine
- Health checks run concurrently per backend
- Atomic operations prevent race conditions on connection counters
- RWMutex protects server alive/dead state

**Key Components:**
- `simpleServer`: Represents a backend with reverse proxy and health tracking
- `LoadBalancer`: Manages server pool and algorithm selection
- `Config`: YAML-driven configuration structure

## Requirements

- Go 1.19+
- `gopkg.in/yaml.v2` for configuration parsing

## What This Demonstrates

This implementation showcases:
- **Go Concurrency**: Proper use of goroutines, atomic operations, and mutexes
- **HTTP Reverse Proxying**: Using `httputil.ReverseProxy`
- **Interface Design**: Clean abstraction with `Server` interface
- **Configuration Management**: YAML parsing and struct tags
- **Error Handling**: Graceful degradation patterns
- **Systems Programming**: Understanding of load balancing fundamentals

## Future Improvements

- Actually use configuration values for health check timing
- Expose metrics endpoint for monitoring
- Add request logging to database
- Implement weighted algorithms
- Add unit tests and benchmarks

--- -->

*Built as a learning project to explore Go concurrency, HTTP proxying, and distributed systems concepts.*