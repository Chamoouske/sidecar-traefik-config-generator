# Traefik Sidecar

Cross-node routing system for multi-host Docker (no Swarm). Generates **all** Traefik configuration files so HTTP requests arriving at any node can reach containers on any other node — including across Windows and Linux hosts.

The sidecar is the **sole source of routing configuration**. Containers use custom `traefik.sidecar.*` labels instead of standard Traefik labels, preventing conflicts between file-provider and Traefik's Docker provider.

## How It Works

```
Client ──► Traefik (Node A) ──► http://<host-ip-node-b>/ ──► Traefik (Node B) ──► Container (local)
```

- **Agent** (one per node) mounts the Docker socket, discovers local containers, and connects to all other Agents via a peer-to-peer gRPC mesh.
- Agents discover each other automatically via **mDNS** — no configuration needed when adding new nodes.
- Each Agent independently computes its own Traefik configuration from local + peer container data.

## Quick Start

```bash
docker compose up -d
```

## Labels

Containers use only `traefik.sidecar.*` labels. Do NOT use standard Traefik labels (`traefik.http.*`).

```yaml
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
- [ADR-0003: Weighted cross-node routing](docs/adr/0003-weighted-cross-node-routing.md)
- [ADR-0004: Peer-to-peer mesh with mDNS](docs/adr/0004-peer-to-peer-mesh-with-mdns.md)
