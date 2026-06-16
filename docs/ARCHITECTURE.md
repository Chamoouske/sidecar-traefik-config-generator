# Architecture — Traefik Sidecar

## Overview

Traefik Sidecar is a cross-node routing system for multi-host Docker (no Swarm). It is the **sole source of Traefik routing configuration** — it generates all configuration files so that HTTP requests arriving at any node can reach containers running on any other node.

Containers do NOT use standard Traefik labels (`traefik.http.*`). Instead, they use custom `traefik.sidecar.*` labels. Each Agent reads these labels via the local Docker API and builds the complete Traefik configuration through the file provider.

The system is composed of a single component deployed per node:

- **Agent** — per-node sidecar container

Agents discover each other via **mDNS** and form a **full mesh** of gRPC bidirectional streams. Each Agent mounts the local Docker socket, discovers its own containers, and reports them to all peers. Each Agent independently computes its own Traefik configuration.

```
┌──────────────────────────────────────────────────────┐
│                   Network (LAN)                        │
│                                                        │
│  ┌──────────┐       mDNS       ┌──────────┐          │
│  │  Host 1   │◄──────────────►│  Host 2   │          │
│  │           │                │           │          │
│  │ ┌──────┐ │   gRPC stream   │ ┌──────┐ │          │
│  │ │Agent │─┼─────────────────┼─│Agent │ │          │
│  │ └──┬───┘ │                │ └──┬───┘ │          │
│  │    │     │                │    │     │          │
│  │ ┌──┴───┐ │                │ ┌──┴───┐ │          │
│  │ │Traefik│ │                │ │Traefik│ │          │
│  │ └──────┘ │                │ └──────┘ │          │
│  └──────────┘                └──────────┘          │
│        │                            │                │
│  ┌─────┴─────┐              ┌──────┴─────┐          │
│  │ Container  │              │  Container  │          │
│  │   A, B     │              │    C        │          │
│  └───────────┘              └────────────┘          │
└──────────────────────────────────────────────────────┘
```

## Components

### Agent (one per host)

The Agent is autonomous. It:

- Mounts the **Docker socket** to discover local containers and subscribe to Docker events
- Reads `traefik.sidecar.*` labels from every local container
- Announces itself via **mDNS** (`_traefik-sidecar._tcp`) and discovers peers
- Establishes **gRPC bidirectional streams** with every discovered peer
- Exchanges `ContainerReport` messages: sends its local container list, receives peer container lists
- Maintains a local registry: mapping of host IP → containers on that host
- Computes its own Traefik configuration from the combined local + peer data
- Writes Traefik configuration files to a shared volume (`/dynamic`)
- Runs a **safety net** periodic full-state sync (default: 60s) with all peers
- Exposes a **gRPC health service** (port 9090) for healthchecks

The Agent is the **sole authority** on its own containers — no other component reads its Docker socket.

## Communication

### mDNS Discovery (LAN only)

When an Agent starts, it:

1. Registers a service `_traefik-sidecar._tcp` on its host IP with port 9090
2. Browses for other `_traefik-sidecar._tcp` services on the network
3. For each discovered peer host, attempts a gRPC connection

Re-discovery runs periodically to detect new peers joining the network.

mDNS is link-local multicast and works within a single LAN segment. It does **not** work across subnets, WAN, or out of Docker Desktop's VM (WSL2/Hyper-V).

### Static Peers (cross-host / Docker Desktop)

For environments where mDNS is unavailable (Docker Desktop, multi-subnet, WAN), peers can be configured statically via the `TRAEFIK_SIDECAR_PEERS` environment variable:

```yaml
TRAEFIK_SIDECAR_PEERS: 192.168.1.20,192.168.1.30
```

Static peers bypass the hostname comparison guard and connect immediately. mDNS continues to work in parallel for LAN-discoverable peers.

When running the Agent on **Docker Desktop** (Windows/Mac), additional configuration is needed:

- Set `TRAEFIK_SIDECAR_NODE_HOST_IP` to the **host machine's LAN IP** (not the VM's internal IP)
- Expose the gRPC port explicitly (since `network_mode: host` does not bridge mDNS or ports to the physical network):

```yaml
agent:
  ports:
    - target: 9090
      published: 9090
  network_mode: ""   # bridge mode
  environment:
    TRAEFIK_SIDECAR_NODE_HOST_IP: 192.168.1.10   # Windows/Mac host LAN IP
    TRAEFIK_SIDECAR_PEERS: 192.168.1.20          # Linux peer IP
```

On **Linux hosts**, `network_mode: host` works correctly — the Agent listens directly on the host's network and mDNS multicasts reach the physical LAN.

### gRPC Bidirectional Stream (Peer-to-Peer)

Once two Agents discover each other, they establish a single long-lived gRPC bidirectional stream:

```
Agent A                          Agent B
  │                                │
  │── ContainerReport(c1, c2) ──► │
  │◄── ContainerReport(c3) ────── │
  │                                │
  │── (on change) Report(c2') ──► │
  │◄── (on change) Report(c4) ─── │
```

**Each Agent sends only its own local containers.** No route commands are exchanged — each peer independently computes its configuration.

### Cross-node Routing

When a request arrives at a Traefik that has no local container for the target service, that Traefik forwards the request to the Traefik on the node where the container is running.

The routing uses the **host IP** of the destination node, not the overlay network.

```
Client ──► Traefik (Node 2) ──► http://<host-ip-node-1>/ ──► Traefik (Node 1) ──► Service A (local container)
                                    │                            │
                                Host header: app.local       Sidecar-generated
                                                             local route resolves
```

### Loop Prevention — Weighted Routing

Each node applies **weighted routing** to prevent forwarding loops:

| Route type | Weight |
|-----------|--------|
| Local      | 9      |
| Cross-node | 1      |

The probability of a request being forwarded at each hop is 10%, so after N hops the probability is 0.1^N.

### Safety Net (Periodic Full Sync)

As a recovery mechanism, each Agent performs a full state exchange with all peers at a configurable interval (default: 60s). The Agent compares the received container reports with its local registry and removes stale entries.

This ensures eventual consistency even if a gRPC notification was missed.

## Data Flow: Container Creation

```
1. Docker: container started with traefik.sidecar.* labels
2. Agent A: receives Docker event via local Docker socket
3. Agent A: inspects container, extracts labels, ports, networks
4. Agent A: sends ContainerReport(updated) to all peers
5. Agent A: recomputes own config
       - New container is local to A → route weight 9
       - Other nodes route to A's IP → route weight 1
6. Agent A: writes <service>.yml to /dynamic
7. Agent B: receives ContainerReport from A
8. Agent B: recomputes own config
       - Container lives on A → remote route to A's IP → weight 1
9. Agent B: writes <service>.yml to /dynamic
10. Each Traefik: file provider detects change, reloads config
```

## Data Flow: Container Removal

```
1. Docker: container stopped / removed
2. Agent A: receives Docker event
3. Agent A: sends ContainerReport(container removed) to all peers
4. Agent A: recomputes config, removes stale routes
5. Agent A: removes <service>.yml from /dynamic
6. Agent B: receives updated ContainerReport from A
7. Agent B: recomputes config, removes routes pointing to A's node
8. Agent B: removes <service>.yml from /dynamic
```

## Data Flow: New Peer Discovery

```
1. Agent B starts, announces via mDNS
2. Agent A: discovers Agent B via mDNS browse
3. Agent A: establishes gRPC stream to B:9090
4. Agent A: sends full ContainerReport(local containers) to B
5. Agent B: sends full ContainerReport(local containers) to A
6. Both Agents: recompute configs with new peer data
7. Both Agents: write updated YAML files
```

## Labels Convention

Containers use only `traefik.sidecar.*` labels. Standard Traefik labels (`traefik.http.*`) must NOT be used — the sidecar generates the complete routing configuration via the file provider.

| Label | Required | Description |
|-------|----------|-------------|
| `traefik.sidecar.enable` | Yes | `true` to enable routing for this container |
| `traefik.sidecar.cross-node` | No | `true` to enable cross-node routing |
| `traefik.sidecar.router.rule` | Yes | Traefik router rule (e.g. `Host(\`app.local\`)`) |
| `traefik.sidecar.router.entrypoints` | No | Entrypoints (default: `websecure`) |
| `traefik.sidecar.router.tls` | No | `true` to enable TLS (default: `true`) |
| `traefik.sidecar.router.middlewares` | No | Comma-separated middleware references |
| `traefik.sidecar.service.port` | Yes | Container port to route traffic to |
| `traefik.sidecar.service.scheme` | No | Protocol scheme (default: `http`) |
| `traefik.sidecar.service.http2` | No | `true` to enable `forwardHTTPVersion` on cross-node transport (needed for gRPC). Default (unset) uses HTTP/1.1, required for WebSocket. |
| `traefik.sidecar.middleware.<name>.<type>` | No | Inline middleware definition |

Each Agent reads these labels from the local Docker API and translates them into the corresponding Traefik dynamic configuration YAML.

## Directory Structure

```
/
├── cmd/
│   └── agent/           # Agent entrypoint
├── internal/
│   ├── api/             # Shared protobuf definitions and gRPC service interfaces
│   ├── agent/           # Agent business logic
│   └── config/          # Shared configuration types
├── pkg/
│   └── docker/          # Docker API client (used by every Agent)
├── docs/
│   ├── ARCHITECTURE.md
│   └── adr/
├── docker-compose.yml
├── Dockerfile.agent
├── CONTEXT.md
├── AGENTS.md
└── README.md
```
