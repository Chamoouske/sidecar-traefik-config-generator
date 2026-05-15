# sidecar

Sidecar agent for Traefik dynamic configuration generation. It runs alongside Traefik instances to generate and manage dynamic configuration files based on Docker containers and services.

## Architecture

The sidecar can operate in two modes:

- **`local` mode**: Generates Traefik dynamic configuration files for the local node based on Docker containers labeled with `traefik.federation.enable=true`.
- **`global` mode**: Aggregates configuration from all local sidecars and generates shared/global configuration files for cross-node routing and shared middlewares.

### Component Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Entry Point (main.go)                       │
│  Loads config → sets up logger → creates LocalGenerator or          │
│  GlobalGenerator based on MODE env var → starts generator           │
└──────────────────────┬──────────────────────────────────────────────┘
                       │
          ┌────────────┴────────────┐
          ▼                         ▼
┌─────────────────────┐  ┌─────────────────────────┐
│   LocalGenerator    │  │    GlobalGenerator       │
│  (internal/generator│  │  (internal/generator     │
│   /local.go)        │  │   /global.go)            │
├─────────────────────┤  ├──────────────────────────┤
│ • Watches Docker    │  │ • Watches file-based     │
│   events            │  │   registry for changes   │
│ • Polls containers  │  │ • Polls registry on      │
│   periodically      │  │   interval               │
│ • Generates per-    │  │ • Generates federation   │
│   container YAML    │  │   configs (cross-node)   │
│   (router → local   │  │ • Collects shared        │
│   container IP)     │  │   middlewares from labels │
│ • Registers node    │  │ • Cleans orphan files    │
│   services in       │  │                          │
│   shared registry   │  │                          │
└───────┬─────────────┘  └──────────┬───────────────┘
        │                           │
        ▼                           ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     Shared Components                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  internal/docker/client.go     — Docker API client abstraction      │
│  internal/watcher/watcher.go   — Docker event stream watcher        │
│  internal/configbuilder/builder.go — Traefik YAML config builder    │
│  internal/hostrule/builder.go  — Traefik Host rule generator        │
│  internal/middleware/collector.go — Middleware definition extractor  │
│  internal/filewriter/writer.go — Atomic file writer with orphans    │
│  internal/reconciler/reconciler.go — Periodic reconciliation loop   │
│  internal/registry/registry.go — File-based node/service registry   │
│  internal/config/config.go    — Configuration loader + validator    │
│  internal/logger/logger.go    — Structured logging (log/slog)       │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Package Details

#### [`internal/config`](internal/config/config.go)
Loads and validates configuration from environment variables. Detects the node IP by inspecting network interfaces (checks `docker_gwbridge`, `eth0`, then falls back to the first non-loopback private IPv4 address). Validates operating mode, port range, poll interval, and log level.

#### [`internal/logger`](internal/logger/logger.go)
Provides structured logging using Go's standard `log/slog` package with four levels: `debug`, `info`, `warn`, `error`. Outputs text-formatted logs to stdout.

#### [`internal/docker`](internal/docker/client.go)
Defines the [`DockerClient`](internal/docker/client.go:28) interface and its implementation for interacting with the Docker daemon. Uses the official Moby SDK with API version negotiation. Filters containers by the `traefik.federation.enable=true` label. Resolves the node hostname from the `traefik.federation.node` label on each container, falling back to the system hostname.

#### [`internal/watcher`](internal/watcher/watcher.go)
Monitors the Docker event stream via [`DockerWatcher`](internal/watcher/watcher.go:14). Normalizes raw Docker actions (`start`, `die`, `kill`, `pause` → `"stop"`; `unpause` → `"start"`) and invokes a registered [`EventHandler`](internal/watcher/watcher.go:11) callback with the event type, container ID, and container name.

#### [`internal/configbuilder`](internal/configbuilder/builder.go)
Builds Traefik v3.7-compatible YAML configuration. Provides Go structs for routers, services, load balancers, health checks, TLS, and middlewares. Key functions:
- [`LocalConfig()`](internal/configbuilder/builder.go:58) — Creates a per-container config with a router pointing directly to the container's IP and port.
- [`FederationConfig()`](internal/configbuilder/builder.go:93) — Creates a cross-node config with a router pointing to a remote node's hostname and Traefik port.
- [`MiddlewareConfig()`](internal/configbuilder/builder.go:128) — Creates a shared middleware definition.
- [`ToYAML()`](internal/configbuilder/builder.go:136) and [`ParseYAML()`](internal/configbuilder/builder.go:145) — YAML serialization/deserialization.

#### [`internal/hostrule`](internal/hostrule/builder.go)
Generates Traefik `Host(...)` rules in the format ``Host(`serviceName.nodeHostname.domainSuffix`)``. Supports custom host override via the `traefik.federation.host` Docker label. The default domain suffix is `lab`.

#### [`internal/middleware`](internal/middleware/collector.go)
Extracts Traefik middleware definitions from Docker container labels prefixed with `traefik.federation.middleware.<name>.<type>.<key>`. Also extracts middleware references from the `traefik.federation.middlewares` (comma-separated) label. The [`Collector`](internal/middleware/collector.go:14) deduplicates middleware definitions across services.

#### [`internal/filewriter`](internal/filewriter/writer.go)
Writes YAML configuration files atomically (writes to a `.tmp` file, then renames to the target path). Validates YAML before writing. Supports dry-run mode (logs instead of writing). Provides [`CleanOrphans()`](internal/filewriter/writer.go:72) to remove unexpected `.yaml`/`.yml` files from an output directory.

#### [`internal/reconciler`](internal/reconciler/reconciler.go)
Implements a periodic reconciliation loop. The [`Reconciler`](internal/reconciler/reconciler.go:11) runs a callback function immediately on start and then at a configurable interval (in seconds). Used by both `LocalGenerator` and `GlobalGenerator` for polling-based regeneration of configuration files.

#### [`internal/registry`](internal/registry/registry.go)
A file-based registry for cross-node service discovery. Each node writes its [`NodeRegistration`](internal/registry/registry.go:19) (hostname, IP, Traefik port, list of services) as a YAML file to a shared directory (default: `/config/shared/registry`). Provides:
- [`WriteNodeRegistration()`](internal/registry/registry.go:48) — Register/update the local node.
- [`ReadNodeRegistration()`](internal/registry/registry.go:70) — Read a specific node's registration.
- [`ListAllNodes()`](internal/registry/registry.go:89) — List all registered nodes.
- [`DeleteNodeRegistration()`](internal/registry/registry.go:124) — Remove a node's registration.
- [`WatchRegistryChanges()`](internal/registry/registry.go:146) — Returns a channel that emits events when registry files are created, modified, or deleted (using fsnotify). Used by the `GlobalGenerator` to react to changes in real time.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `MODE` | `local` | Operating mode: `local` or `global` |
| `LOCAL_OUTPUT_PATH` | `/config/local/generated` | Path to write local dynamic config files |
| `SHARED_OUTPUT_PATH` | `/config/shared/generated` | Path to write shared dynamic config files |
| `REGISTRY_PATH` | `/config/shared/registry` | Path to the shared registry for cross-node discovery |
| `NODE_HOSTNAME` | system hostname | Hostname of the current node |
| `NODE_IP` | auto-detected | IP address of the current node |
| `LOCAL_TRAEFIK_PORT` | `80` | Port where the local Traefik instance listens |
| `DEFAULT_DOMAIN_SUFFIX` | `lab` | Default domain suffix for generated routes |
| `POLL_INTERVAL` | `30` | Interval in seconds for polling Docker events / registry |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker daemon socket path |
| `DRY_RUN` | `false` | If `true`, log generated configs without writing them |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

## How to Run

### Prerequisites

- Go 1.22 or later
- Docker (for local mode)

### Building

```bash
go build -o bin/sidecar ./cmd/sidecar
```

### Running

```bash
# Run in local mode (default)
MODE=local ./bin/sidecar

# Run in global mode
MODE=global ./bin/sidecar

# With custom log level
LOG_LEVEL=debug ./bin/sidecar

# Dry-run mode (log only, no file writes)
DRY_RUN=true ./bin/sidecar
```

### Development

```bash
# Run directly without building
go run ./cmd/sidecar
```

## Docker

### Building the Image

```bash
docker build -t sidecar .
```

The [`Dockerfile`](Dockerfile) uses a multi-stage build:
1. **Builder stage** — Compiles the Go binary with `CGO_ENABLED=0` for a fully static binary.
2. **Runtime stage** — Copies the binary to a minimal `alpine:3.21` image with only CA certificates and timezone data.

### Docker Compose

The [`docker-compose.yml`](docker-compose.yml) provides a reference deployment for Docker Swarm with two services:

- **`sidecar-global`** — Runs in `global` mode (1 replica, manager node). Generates federation and shared middleware configurations.
- **`sidecar-local`** — Runs in `local` mode (global deploy — one instance per node). Generates per-container configurations and registers services in the shared registry.

Both services share a `sync_data` volume mounted at `/data`, which corresponds to the paths configured via `SHARED_OUTPUT_PATH`, `LOCAL_OUTPUT_PATH`, and `REGISTRY_PATH`.

```bash
# Deploy to Docker Swarm
docker stack deploy -c docker-compose.yml sidecar
```

## Project Structure

```
.
├── cmd/
│   └── sidecar/
│       └── main.go                    # Entry point
├── internal/
│   ├── config/
│   │   ├── config.go                  # Configuration loading and validation
│   │   └── config_test.go
│   ├── configbuilder/
│   │   ├── builder.go                 # Traefik YAML config builder
│   │   └── builder_test.go
│   ├── docker/
│   │   ├── client.go                  # Docker API client abstraction
│   │   └── client_test.go
│   ├── filewriter/
│   │   ├── writer.go                  # Atomic file writer with orphan cleanup
│   │   └── writer_test.go
│   ├── generator/
│   │   ├── generator.go               # Generator interface
│   │   ├── local.go                    # Local mode generator
│   │   └── global.go                   # Global mode generator
│   ├── hostrule/
│   │   ├── builder.go                  # Traefik Host rule generator
│   │   └── builder_test.go
│   ├── logger/
│   │   ├── logger.go                   # Structured logging (log/slog)
│   │   └── logger_test.go
│   ├── middleware/
│   │   ├── collector.go                # Middleware definition extractor
│   │   └── collector_test.go
│   ├── reconciler/
│   │   └── reconciler.go               # Periodic reconciliation loop
│   └── registry/
│       ├── registry.go                 # File-based node/service registry
│       └── registry_test.go
├── Dockerfile                          # Multi-stage Docker build
├── docker-compose.yml                  # Docker Swarm deployment reference
├── go.mod
├── go.sum
└── README.md
```

## License

MIT
