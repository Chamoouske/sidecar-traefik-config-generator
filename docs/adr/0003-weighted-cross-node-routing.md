# ADR-0003: Weighted Cross-Node Routing with Local Preference

**Status:** Accepted

**Date:** 2026-06-11

## Context

When a service has tasks running on multiple nodes, each node's Traefik can route requests:

1. **Locally** — directly to the task on the same node (via Swarm provider)
2. **Cross-node** — forward to another node's Traefik (via host IP)

If Node A has a cross-node route pointing to Node B, and Node B has a cross-node route pointing to Node A for the same service, a request can loop infinitely between them.

A naive solution is: "only route cross-node to services that have NO local task." This would prevent loops but also prevents load distribution across nodes.

## Decision

Use **weighted routing** where local routes have higher weight than cross-node routes:

| Route type | Weight | Percentage |
|-----------|--------|------------|
| Local (Swarm provider) | 9 | 90% |
| Cross-node (to peer) | 1 | 10% |

This works as follows:

- Each node's Traefik has TWO router/service entries for the same host rule
- The local route (via Swarm provider) has weight=9
- The cross-node route (forwarding to peer node) has weight=1
- Traefik's weighted round-robin (WRR) distributes traffic accordingly

**Why this prevents loops:** A request entering the mesh at any node has a 90% chance of being served locally if a task exists on that node, and only a 10% chance of being forwarded. After each forward, the same distribution applies. The probability of a request being forwarded more than 3 times is 0.1%, making infinite loops practically impossible.

## Consequences

Positive:

- **Simple loop prevention** — no need for complex loop detection, TTL headers, or distributed consensus
- **Load distribution** — remote nodes can still serve traffic if the local node is overloaded
- **Graceful degradation** — if all local tasks are busy, 10% bleeds to other nodes

Negative:

- **Suboptimal routing** — 10% of requests are sent cross-node even when local capacity exists
- **Hidden dependency** — requires understanding of Traefik's WRR behavior to debug

## Safety Net (Polling)

As a complementary mechanism, each Agent periodically polls the Hub (default: 60s) with its current route list. The Hub responds with the authoritative route list. The Agent removes any route not present in the response.

This handles the case where:
- The Hub crashed and missed a service removal event
- A gRPC message was lost
- The Agent restarted independently

## Alternatives Considered

### TTL-based loop detection
Add a header like `X-Traefik-Sidecar-Hop: 1` and refuse to forward when hop > N. Requires middleware for every cross-node route. More complex to implement across restart scenarios.

### No cross-node routing if local task exists
Simple, but prevents using remote nodes for capacity or redundancy.

### Full mesh with loop detection
Each node maintains full knowledge of all routes and detects cycles algorithmically. Over-engineered for the actual risk.
