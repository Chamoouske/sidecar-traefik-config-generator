# Traefik Sidecar

Cross-node routing system for multi-host Docker (no Swarm). Generates **all** Traefik configuration files so HTTP requests arriving at any node can reach containers on any other node — including across Windows and Linux hosts.

The sidecar is the **sole source of routing configuration**. Containers use custom `traefik.sidecar.*` labels instead of standard Traefik labels, preventing conflicts between file-provider and Traefik's Docker provider.

## How It Works

```
Client ──► Traefik (Node A) ──► http://<host-ip-node-b>/ ──► Traefik (Node B) ──► Container (local)
```

- **Agent** (one per node) mounts the Docker socket, discovers local containers, and connects to all other Agents via a peer-to-peer gRPC mesh.
- Agents discover each other via **mDNS** (LAN) or **static peer configuration** (`TRAEFIK_SIDECAR_PEERS`) for cross-host setups (Docker Desktop, multi-subnet).
- Each Agent independently computes its own Traefik configuration from local + peer container data.

## Quick Start

```bash
docker compose up -d
```

## Configuration

| Env variable | Default | Description |
|---|---|---|
| `TRAEFIK_SIDECAR_NODE_HOST_IP` | auto-detected | Routable IP of the host (required on Docker Desktop) |
| `TRAEFIK_SIDECAR_AGENT_PORT` | `9090` | gRPC port for peer-to-peer communication |
| `TRAEFIK_SIDECAR_PEERS` | `""` | Comma-separated peer IPs for static discovery |
| `TRAEFIK_SIDECAR_CONFIG_DIR` | `/etc/traefik-sidecar` | Directory for generated YAML config files |
| `TRAEFIK_SIDECAR_POLL_INTERVAL` | `60s` | Safety-net full-state sync interval |
| `TRAEFIK_SIDECAR_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker API endpoint |
| `TRAEFIK_SIDECAR_LOG_LEVEL` | `info` | Log level |
| `TRAEFIK_SIDECAR_PUID` | `1000` | User ID for file permissions |
| `TRAEFIK_SIDECAR_GUID` | `1000` | Group ID for file permissions |

### Docker Desktop

`network_mode: host` does not expose mDNS or ports to the physical network on Docker Desktop (Windows/Mac). Use bridge networking instead:

```yaml
agent:
  ports:
    - target: 9090
      published: 9090
  environment:
    TRAEFIK_SIDECAR_NODE_HOST_IP: <windows-host-lan-ip>
    TRAEFIK_SIDECAR_PEERS: <linux-peer-ip>
```

mDNS will not work for discovery between Docker Desktop and Linux hosts. Use `TRAEFIK_SIDECAR_PEERS` to configure peers explicitly.

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
