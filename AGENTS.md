# AGENTS.md — Traefik Sidecar

## Project Structure

```
/
├── cmd/
│   ├── hub/main.go          # Hub entrypoint
│   └── agent/main.go        # Agent entrypoint
├── internal/
│   ├── api/                 # Shared protobuf definitions and gRPC interface
│   ├── hub/                 # Hub business logic
│   ├── agent/               # Agent business logic
│   └── config/              # Shared configuration types
├── pkg/
│   └── docker/              # Docker/Swarm API client (Hub only)
├── docs/
│   ├── ARCHITECTURE.md      # System architecture
│   └── adr/                 # Architecture Decision Records
├── docker-compose.yml
├── Dockerfile.hub
├── Dockerfile.agent
└── CONTEXT.md               # Domain glossary
```

## Architecture Rules

1. **Hub is the single source of truth** — only the Hub reads the Docker/Swarm API. Agents never mount the Docker socket.
2. **No standard Traefik labels** — services use only `traefik.sidecar.*` labels. The sidecar generates all routing config via the file provider. Standard `traefik.http.*` labels must not appear on services to avoid conflicts with the Swarm provider.
3. **Agent-initiated gRPC stream** — Agent dials out to Hub. The connection is persistent and bidirectional.
4. **Weighted routing** — local routes (weight 9) vs cross-node routes (weight 1) to prevent loops.
5. **Safety net polling** — Agent polls Hub every 60s as eventual consistency fallback.

## Code Conventions

- **Language:** Go
- **gRPC:** Protocol Buffers for all Hub-Agent communication contracts
- **Docker client:** Only in `pkg/docker/`, used exclusively by Hub
- **Configuration:** Environment variables, prefixed with `TRAEFIK_SIDECAR_`

## Testing

- **Unit tests:** standard Go `_test.go` files next to implementation
- **Integration tests:** require a Docker Swarm test environment
- **gRPC contracts:** test via gRPC stub/server in `internal/api/`

## Build

```bash
# Generate protobuf code
protoc --go_out=. --go-grpc_out=. internal/api/*.proto

# Build
go build ./cmd/hub
go build ./cmd/agent
```
