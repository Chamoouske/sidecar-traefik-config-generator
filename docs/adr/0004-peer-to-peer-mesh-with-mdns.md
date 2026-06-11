# ADR-0004: Peer-to-Peer Agent Mesh with mDNS Discovery

**Status:** Accepted

**Date:** 2026-06-11

**Supersedes:** ADR-0002

## Context

The original architecture used a Hub as the central coordinator. The Hub:

- Was the sole consumer of the Docker/Swarm API
- Maintained global state of all services and tasks
- Computed all routing configurations
- Sent RouteCommands to per-node Agents via gRPC

This design had several drawbacks:

1. **Single point of failure** — if the Hub went down, no new cross-node routes could be created
2. **Dependency on Swarm** — the Hub relied on Swarm API concepts (services, tasks, nodes)
3. **Network complexity** — needed overlay network connectivity for all Agent→Hub streams
4. **Operational overhead** — requires managing a Hub service, placement constraints, health checks

After dissolving the Swarm in favor of standalone Docker hosts managed via Portainer, the Hub-centric design no longer fits the architecture.

## Decision

Replace the Hub with a **peer-to-peer mesh of Agents**, using **mDNS** for automatic discovery.

### Key characteristics

- **No Hub** — each Agent is fully autonomous
- **mDNS discovery** — Agents find each other on the LAN via `_traefik-sidecar._tcp` service type. No configuration needed for new nodes
- **Full mesh topology** — every Agent connects to every other Agent via gRPC bidirectional streams
- **Container reports only** — Agents exchange `ContainerReport` messages (no RouteCommands). Each Agent independently computes its own Traefik configuration
- **Each Agent mounts Docker socket** — no centralized Docker API access
- **Distributed state** — each Agent maintains a local registry of host → containers, built from peer reports

### Protocol

The gRPC bidirectional stream (established per ADR-0001) is retained but now between Agents:

```
Agent A                              Agent B
  │                                    │
  │──── ContainerReport(A) ──────────►│
  │◄─── ContainerReport(B) ──────────│
  │                                    │
  │──── (on Docker event) ──────────►│
  │◄─── (on Docker event) ──────────│
```

Each Agent sends its own local containers. No route commands are exchanged.

### Safety Net

Periodic full-state exchange (default: 60s) between all connected peers ensures eventual consistency.

## Consequences

Positive:

1. **No single point of failure** — the mesh continues to operate if any single Agent goes down
2. **No Swarm dependency** — works with any Docker Engine deployment
3. **Zero-config discovery** — new nodes join automatically via mDNS
4. **Simpler deployment** — only one component type per node (Agent)
5. **Local autonomy** — each Agent reacts instantly to local Docker events without waiting for a coordinator
6. **Better scalability** — compute is distributed across all Agents instead of centralized in the Hub

Negative:

1. **N×(N-1)/2 connections** — full mesh creates quadratic connection count; acceptable for small-medium clusters (<20 nodes)
2. **No global authority** — if two Agents disagree, there is no arbiter. Relies on the Docker API as the source of truth per node
3. **mDNS limited to LAN** — does not work across subnets or WAN without additional configuration
4. **Increased Docker socket exposure** — every node now mounts the Docker socket (previously only Hub), increasing attack surface per node

## Mitigations

- mDNS scope limitation is acceptable: the target deployment is a single LAN (or VPN) per environment
- Each Agent only has access to its own node's Docker socket, not the entire cluster
- Safety net polling detects and corrects disagreements within 60s maximum

## Alternatives Considered

### Hub with failover

A standby Hub replica that takes over if primary fails. Adds complexity (leader election, state replication) without eliminating the Swarm dependency.

### Gossip protocol (SWIM, memberlist)

More scalable than full mesh but adds significant complexity. Full mesh is simpler and sufficient for the expected scale (<20 nodes).

### Static configuration (hosts file)

List all peer IPs in environment variables. Works but requires reconfiguration every time a host is added or removed. mDNS eliminates this operational friction.

### Centralized database (etcd, Consul)

Adds infrastructure complexity and operational overhead. Overkill for the problem.
