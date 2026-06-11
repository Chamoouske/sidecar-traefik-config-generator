# Architecture — Traefik Sidecar

## Overview

Traefik Sidecar is a cross-node routing system for Docker Swarm. It is the **sole source of Traefik routing configuration** — it generates all configuration files so that HTTP requests arriving at any node can reach services running on any other node.

Services do NOT use standard Traefik Swarm provider labels (`traefik.http.*`). Instead, they use custom `traefik.sidecar.*` labels. The sidecar reads these labels via the Docker API and builds the complete Traefik configuration through the file provider.

The system is composed of two Go components:

- **Hub** — central coordinator (manager node, 1 replica)
- **Agent** — per-node sidecar (global mode)

Both run as containers in the same Docker Swarm overlay network as Traefik.

```
┌─────────────────────────────────────────────────────┐
│                   Swarm Cluster                      │
│                                                      │
│  ┌────── Manager (Linux) ──────┐                     │
│  │  ┌─────────┐  ┌──────────┐  │                     │
│  │  │ Traefik │  │   Hub    │  │                     │
│  │  │ (global)│  │ (1 repl) │  │                     │
│  │  └─────────┘  └────┬─────┘  │                     │
│  └─────────────────────┼────────┘                     │
│                        │ gRPC stream                  │
│        ┌───────────────┼───────────────┐              │
│        │               │               │              │
│  ┌─────┴───┐    ┌──────┴────┐    ┌────┴─────┐        │
│  │ Agent 1 │    │ Agent 2   │    │ Agent 3  │ ...    │
│  │ (Linux) │    │ (Windows) │    │ (Linux)  │        │
│  └──┬──────┘    └──┬───────┘    └──┬───────┘        │
│     │ write        │ write         │ write            │
│  ┌──┴──────┐  ┌────┴──────┐  ┌────┴──────┐          │
│  │ Traefik │  │  Traefik  │  │  Traefik  │           │
│  │ config  │  │  config   │  │  config   │           │
│  │ files   │  │  files    │  │  files    │           │
│  └─────────┘  └───────────┘  └───────────┘           │
└─────────────────────────────────────────────────────┘
```

## Components

### Hub

The Hub is the brain of the system. It:

- Connects to the Docker/Swarm API via the mounted socket
- Subscribes to Docker events (service create, update, remove; task scheduling)
- Reads `traefik.sidecar.*` labels from every service in the Swarm
- Maintains a global registry: mapping of services → tasks → nodes → host IPs
- Computes the **full routing configuration** for each service (routers, middlewares, services, servers, loadbalancer settings)
- Computes cross-node routes between nodes
- Pushes route creation/removal commands to the relevant Agents

The Hub is stateless in terms of storage — all state is rebuilt from the Docker API on restart. It does NOT store data in a database.

### Agent

The Agent is the executor. It:

- Initiates a gRPC bidirectional stream connection to the Hub (Agent → Hub dial)
- Listens for incoming commands (Hub → Agent) on the stream
- Writes Traefik configuration files to a shared volume (/dynamic)
- Sends status acknowledgments back (Agent → Hub)
- Polls the Hub periodically (safety net) to verify route validity

The Agent does NOT mount the Docker socket — all service/task information comes through the Hub.

## Communication

### gRPC Bidirectional Stream

The Agent initiates a single long-lived gRPC stream to the Hub:

```
Agent ───── gRPC stream ──────► Hub
  │                                │
  │ ◄──── commands ─────────────── │
  │ ────── status/ack ──────────► │
```

**Why gRPC over HTTP:** The previous system used HTTP (Agent as server, Hub as client) and suffered from "connection refused" on Agent → Hub requests. With gRPC streams, the Agent dials out to the Hub — the connection is established once and kept alive. This eliminates the connectivity issue regardless of network topology.

**Wire format:** Protocol Buffers (protobuf) contracts shared between Hub and Agent.

### Cross-node Routing (service discovery)

When a request arrives at a Traefik that has no local task for the target service, that Traefik forwards the request to the Traefik on the node where the task is running.

The routing uses the **host IP** of the destination node (not the overlay network), because native overlay communication between Windows and Linux nodes is unreliable.

```
Client ──► Traefik (Node 2) ──► http://<host-ip-node-1>/ ──► Traefik (Node 1) ──► Service A (local task)
                                  │                              │
                              Host header: app.local         Sidecar-generated
                                                              local route resolves
```

### Loop Prevention — Weighted Routing

Since the sidecar generates all routes (both local and cross-node), it can assign different weights to each. To prevent forwarding loops, each node applies **weighted routing**:

| Route type | Weight |
|-----------|--------|
| Local      | 9      |
| Cross-node | 1      |

With this weighting, the probability of a request looping back and forth more than a few hops approaches zero exponentially. Formally, the chance of a request being forwarded at each hop is 10%, so after N hops the probability is 0.1^N.

### Safety Net (Polling)

As a recovery mechanism, each Agent polls the Hub at a configurable interval (default: 60s). The Agent sends its current list of cross-node routes, and the Hub responds with the authoritative list for that node. The Agent removes any route no longer present in the Hub's response.

This ensures eventual consistency even if a gRPC notification was missed.

## Data Flow: Service Creation

```
1. Docker: service created with traefik.sidecar.* labels
2. Hub: receives Docker event via Swarm API
3. Hub: queries Swarm API for service details, labels, task placement, node host IPs
4. Hub: builds full Traefik config for the service (routers, services, middlewares)
5. Hub: computes required cross-node routes
       For each node that does NOT have a local task:
         → schedule a cross-node route pointing to the node that HAS the task
6. Hub: sends gRPC command to each node's Agent
       ─→ Command: UPSERT_ROUTE {service, full_config, target_node_host_ip}
   -- Node with local task → writes local route (weight 9) + cross-node routes (weight 1)
   -- Node without local task → writes only cross-node routes (weight 1)
7. Agent: receives command, writes <service>.yml to /dynamic
8. Agent: sends ACK back to Hub
9. Traefik: file provider detects change, reloads config
```

## Data Flow: Service Removal

```
1. Docker: service removed / scaled down
2. Hub: receives Docker event
3. Hub: computes which cross-node routes are no longer needed
4. Hub: sends gRPC command to each relevant Agent
       ─→ Command: DELETE_ROUTE {service}
5. Agent: removes <service>.yml from /dynamic
6. Agent: sends ACK back to Hub
```

## Labels Convention

Services use only `traefik.sidecar.*` labels. Standard Traefik Swarm labels (`traefik.http.*`) must NOT be used — the sidecar generates the complete routing configuration via the file provider.

| Label | Required | Description |
|-------|----------|-------------|
| `traefik.sidecar.enable` | Yes | `true` to enable routing for this service |
| `traefik.sidecar.cross-node` | No | `true` to enable cross-node routing |
| `traefik.sidecar.router.rule` | Yes | Traefik router rule (e.g. `Host(\`app.local\`)`) |
| `traefik.sidecar.router.entrypoints` | No | Entrypoints (default: `websecure`) |
| `traefik.sidecar.router.tls` | No | `true` to enable TLS (default: `true`) |
| `traefik.sidecar.router.middlewares` | No | Comma-separated middleware references |
| `traefik.sidecar.service.port` | Yes | Container port to route traffic to |
| `traefik.sidecar.service.scheme` | No | Protocol scheme (default: `http`) |
| `traefik.sidecar.middleware.<name>.<type>` | No | Inline middleware definition |

The Hub reads these labels from the Docker API and translates them into the corresponding Traefik dynamic configuration YAML.

## Directory Structure

```
/
├── cmd/
│   ├── hub/             # Hub entrypoint
│   └── agent/           # Agent entrypoint
├── internal/
│   ├── api/             # Shared protobuf definitions and gRPC service interfaces
│   ├── hub/             # Hub business logic
│   ├── agent/           # Agent business logic
│   └── config/          # Shared configuration types
├── pkg/
│   └── docker/          # Docker/Swarm API client (Hub only)
├── docs/
│   ├── ARCHITECTURE.md
│   └── adr/
├── docker-compose.yml
├── Dockerfile.hub
├── Dockerfile.agent
├── .github/workflows/
├── CONTEXT.md
├── AGENTS.md
└── README.md
```
