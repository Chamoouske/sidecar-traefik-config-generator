# ADR-0001: gRPC Bidirectional Streams for Hub-Agent Communication

**Status:** Accepted

**Date:** 2026-06-11

## Context

The previous version of Traefik Sidecar used HTTP for Hub-Agent communication. The Hub acted as an HTTP client that sent requests to each Agent (HTTP server on port 9090). Agents also needed to send requests back to the Hub (HTTP server on port 8080).

In production, Agents consistently failed to reach the Hub with `connection refused`, while the Hub could reach Agents successfully. This asymmetric failure occurred because:

1. Agent → Hub connections crossed the Docker overlay network in a direction where Swarm networking had issues
2. The Hub's port 8080 was unreachable from Agents despite being reachable from outside the overlay

The Agents' inability to communicate back prevented status reporting and error recovery.

## Decision

Replace HTTP request-response with **gRPC bidirectional streams**, where the **Agent dials out** to the Hub and maintains a persistent connection.

Key characteristics:

- **Agent-initiated connection** — Agent dials `hub:8080` via gRPC, solving the asymmetric reachability problem
- **Persistent stream** — single TCP connection stays open for the Agent's lifetime; no repeated handshakes
- **Bidirectional** — Hub pushes commands (upsert/delete routes) and Agent pushes status/acks on the same stream
- **Protocol Buffers** — strongly typed contract shared between Hub and Agent, enforced at build time

## Consequences

Positive:

- Eliminates the `connection refused` failure mode: only the Agent needs outbound connectivity to the Hub
- Single persistent TCP connection per Agent (lower overhead than HTTP polling)
- Type-safe API contract via protobuf
- gRPC's built-in keepalive detects dead Agents without custom health checks

Negative:

- Requires protobuf code generation step in the build pipeline
- Adds learning curve for developers unfamiliar with gRPC
- Debugging gRPC traffic is slightly harder than HTTP (need grpcurl or similar)

## Alternatives Considered

### HTTP with polling
Agents poll Hub periodically. Simple but increases latency (route changes take up to one poll interval to propagate) and wastes bandwidth.

### WebSocket
Similar persistent connection model, but without type-safe contracts. Adds complexity without significant benefit over gRPC.

### Message queue (NATS, RabbitMQ)
Adds an external infrastructure dependency. Overkill for a two-component system.
