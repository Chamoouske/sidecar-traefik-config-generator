# sidecar

Sidecar agent for Traefik dynamic configuration generation. It runs alongside Traefik instances to generate and manage dynamic configuration files based on Docker containers and services.

## Architecture

The sidecar can operate in two modes:

- **`local` mode**: Generates Traefik dynamic configuration files for the local node based on Docker container labels.
- **`global` mode**: Aggregates configuration from all local sidecars and generates shared/global configuration files.

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
| `POLL_INTERVAL` | `30` | Interval in seconds for polling Docker events |
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

## Project Structure

```
.
├── cmd/
│   └── sidecar/
│       └── main.go          # Entry point
├── internal/
│   ├── config/
│   │   └── config.go        # Configuration loading and validation
│   └── logger/
│       └── logger.go        # Structured logging with log/slog
├── go.mod
├── go.sum
└── README.md
```

## License

MIT
