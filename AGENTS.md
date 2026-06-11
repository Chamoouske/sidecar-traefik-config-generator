# AGENTS.md — Traefik Sidecar

## Project Structure

```
/
├── cmd/
│   └── agent/main.go        # Agent entrypoint
├── internal/
│   ├── api/                 # Shared protobuf definitions and gRPC interface
│   ├── agent/               # Agent business logic
│   └── config/              # Shared configuration types
├── pkg/
│   └── docker/              # Docker API client (used by every Agent)
├── docs/
│   ├── ARCHITECTURE.md      # System architecture
│   └── adr/                 # Architecture Decision Records
├── docker-compose.yml
├── Dockerfile.agent
└── CONTEXT.md               # Domain glossary
```

## Architecture Rules

1. **No standard Traefik labels** — containers use only `traefik.sidecar.*` labels. The sidecar generates all routing config via the file provider. Standard `traefik.http.*` labels must not appear on containers to avoid conflicts.
2. **Peer-to-peer gRPC mesh** — each Agent connects to all discovered peers via gRPC bidirectional streams. Connection is initiated by the discovering Agent.
3. **mDNS discovery** — Agents announce and discover each other automatically via multicast DNS.
4. **Each Agent mounts Docker socket** — every Agent reads its own node's containers and labels directly.
5. **Weighted routing** — local routes (weight 9) vs cross-node routes (weight 1) to prevent loops.
6. **Safety net polling** — periodic full-state exchange between peers (default: 60s) as eventual consistency fallback.

## Mandatory Practices

- **TDD (Test-Driven Development)** — write the test first, then implement, then refactor. No code is accepted without a corresponding test. This is not optional.
- **DDD (Domain-Driven Design)** — the codebase must reflect the domain language defined in `CONTEXT.md`. Ubiquitous language, bounded contexts, and aggregates must be respected.

## Code Conventions

- **Language:** Go
- **gRPC:** Protocol Buffers for all Agent-to-Agent communication contracts
- **Docker client:** In `pkg/docker/`, used by every Agent
- **Configuration:** Environment variables, prefixed with `TRAEFIK_SIDECAR_`

## Testing

- **Unit tests:** standard Go `_test.go` files next to implementation
- **Integration tests:** require a multi-host Docker environment with mDNS
- **gRPC contracts:** test via gRPC stub/server in `internal/api/`

## Build

```bash
# Generate protobuf code
protoc --go_out=. --go-grpc_out=. internal/api/*.proto

# Build
go build ./cmd/agent
```
