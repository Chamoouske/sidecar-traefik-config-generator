# Traefik Sidecar

Cross-node routing system for Docker Swarm. Generates **all** Traefik configuration files so HTTP requests arriving at any node can reach services on any other node — including across Windows and Linux hosts.

The sidecar is the **sole source of routing configuration**. Services use custom `traefik.sidecar.*` labels instead of standard Traefik Swarm provider labels, preventing conflicts between file-provider and Swarm-provider routes.

## How It Works

```
Client ──► Traefik (Node A) ──► http://<host-ip-node-b>/ ──► Traefik (Node B) ──► Service (local task)
```

- **Hub** (manager node, 1 replica) reads the Docker/Swarm API and `traefik.sidecar.*` labels, builds the full routing configuration, and computes cross-node routes.
- **Agent** (one per node) connects to the Hub via a persistent gRPC stream, receives route commands, and writes local Traefik configuration files.

## Quick Start

```bash
docker stack deploy -c docker-compose.yml traefik-sidecar
```

## Labels

Services use only `traefik.sidecar.*` labels. Do NOT use standard Traefik labels (`traefik.http.*`).

```yaml
deploy:
  labels:
    - "traefik.sidecar.enable=true"
    - "traefik.sidecar.cross-node=true"
    - "traefik.sidecar.router.rule=Host(`app.local`)"
    - "traefik.sidecar.router.entrypoints=websecure"
    - "traefik.sidecar.service.port=80"
```

See [Architecture — Labels Convention](docs/ARCHITECTURE.md#labels-convention) for the full reference.

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [ADR-0001: gRPC bidirectional streams](docs/adr/0001-grpc-bidirectional-streams.md)
- [ADR-0002: Centralized Hub state](docs/adr/0002-hub-centralized-state.md)
- [ADR-0003: Weighted cross-node routing](docs/adr/0003-weighted-cross-node-routing.md)
