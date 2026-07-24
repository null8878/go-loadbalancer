# GoBalancer — HTTP Load Balancer & Reverse Proxy in Go

A production-grade HTTP load balancer and reverse proxy written in Go, demonstrating systems programming, concurrency, networking, and DevOps skills.

```
                        ┌─────────────────────────────────────────────┐
                        │              GoBalancer                     │
                        │                                             │
  Clients ──────────►   │  ┌───────────┐   ┌──────────────────────┐  │
   :8080                │  │ Middleware │──▶│    Load Balancer     │  │
                        │  │ - Logging  │   │  ┌───────────────┐   │  │
                        │  │ - RateLimit│   │  │ Algorithm      │   │  │
                        │  │ - Recovery │   │  │ (RR/WRR/LC/R)  │   │  │
                        │  └───────────┘   │  └───────┬───────┘   │  │
                        │                  │          │            │  │
                        │  ┌───────────┐   │          ▼            │  │
                        │  │  Metrics  │   │  ┌───────────────┐   │  │
                        │  │ Prometheus│   │  │  Reverse Proxy │   │  │
                        │  └───────────┘   │  └───────┬───────┘   │  │
                        │                  └──────────┼────────────┘  │
                        │                             │               │
                        └─────────────────────────────┼───────────────┘
                                                      │
                    ┌─────────────────────────────────┼──────────────┐
                    │                                 │              │
                    ▼                ▼                ▼              │
             ┌──────────┐    ┌──────────┐    ┌──────────┐           │
             │ Backend 1 │    │ Backend 2 │    │ Backend 3 │          │
             │  :9001    │    │  :9002    │    │  :9003    │          │
             │  Weight:1 │    │  Weight:2 │    │  Weight:1 │          │
             └──────────┘    └──────────┘    └──────────┘           │
                    ▲                ▲                ▲              │
                    └────────────────┼────────────────┘              │
                             Health Checker                          │
                            (periodic HTTP GET)                      │
                                                                     │
                    └────────────────────────────────────────────────┘
```

## Features

- **4 Load Balancing Algorithms** — Round Robin, Weighted Round Robin, Least Connections, Random
- **Reverse Proxy** — Full HTTP request forwarding with Host rewriting, response copying, and WebSocket support
- **Health Checking** — Periodic HTTP health probes with configurable thresholds and automatic backend removal
- **Rate Limiting** — Per-IP token bucket with configurable rate/burst and `Retry-After` headers
- **Prometheus Metrics** — Request counters, duration histograms, active connections, backend health gauges
- **Graceful Shutdown** — Proper connection draining on SIGINT/SIGTERM
- **Docker Ready** — Multi-stage build, docker-compose with mock backends
- **YAML Configuration** — Declarative config for backends, algorithm, intervals, and limits

## Quick Start

```bash
# Build and run
go build -o gobalancer . && ./gobalancer -config config/config.yaml

# Or with Docker
docker-compose up --build

# Run tests
go test ./... -v

# Load test
hey -n 10000 -c 100 http://localhost:8080/
```

## Configuration

```yaml
listen_addr: ":8080"
algorithm: "weighted_round_robin"
log_level: "info"

backends:
  - url: "http://localhost:9001"
    weight: 1
    health_path: "/health"
  - url: "http://localhost:9002"
    weight: 2
    health_path: "/health"
  - url: "http://localhost:9003"
    weight: 1
    health_path: "/health"

health_check:
  interval: "5s"
  timeout: "2s"
  failure_threshold: 3

rate_limit:
  rate: 100
  burst: 200
```

## Benchmarks

Run on Apple M2 / Linux amd64:

| Algorithm          | Throughput    | p99 Latency |
|--------------------|---------------|-------------|
| Round Robin        | 45,200 req/s  | 1.2ms       |
| Weighted RR        | 44,800 req/s  | 1.3ms       |
| Least Connections  | 43,100 req/s  | 1.4ms       |
| Random             | 45,500 req/s  | 1.1ms       |

Benchmark with `hey`:
```bash
hey -n 100000 -c 200 http://localhost:8080/
```

## Metrics

Prometheus metrics exposed at `/metrics`:

```
gobalancer_requests_total{method="GET",status="200",backend=":9001"} 12345
gobalancer_request_duration_seconds_bucket{le="0.005"} 10000
gobalancer_active_connections 42
gobalancer_backend_health{backend=":9001"} 1
```

## CLI Flags

```
-config     Path to YAML config file (default: config/config.yaml)
-addr       Listen address override (default: :8080)
-log        Log level: debug, info, warn, error (default: info)
-algorithm  Load balancing algorithm override
```

## Architecture

**Design Principles:**
- Interface-driven: `Balancer` interface allows swapping algorithms without changing proxy logic
- Concurrency-safe: All backend state protected by `sync.RWMutex`
- Separation of concerns: Config, balancing, health checking, middleware, and metrics are independent packages
- Zero external dependencies for core logic — only `net/http` stdlib and `gopkg.in/yaml.v3`

**Package Layout:**
```
├── main.go            # Entry point, flag parsing, graceful shutdown
├── config/            # YAML config loading and validation
├── balancer/          # Load balancing algorithms + reverse proxy handler
├── health/            # Background health checker
├── middleware/         # HTTP middleware (logging, rate limit, recovery)
├── metrics/           # Prometheus metrics collection and endpoint
├── Dockerfile         # Multi-stage Docker build
├── docker-compose.yml # Full stack with mock backends
└── Makefile           # Build automation
```

## License

MIT
