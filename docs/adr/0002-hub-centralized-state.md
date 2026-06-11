# ADR-0002: Centralized Docker API Access via Hub

**Status:** Accepted

**Date:** 2026-06-11

## Context

In the previous design, both Hub and Agent mounted the Docker socket. The Agent used it to:

1. Get detailed task/container information on the local node
2. Potentially detect services independently

This created two problems:

- **Security surface** — every node had a container with Docker API access (bind-mounted socket)
- **State inconsistency** — Hub and Agent could disagree about the Swarm state, leading to stale or conflicting configuration files

The Docker socket on Windows nodes (Docker Desktop via WSL2) also introduces subtle path differences.

## Decision

Centralize all Docker API access in the **Hub only**. The Hub:

- Is the **sole consumer** of the Docker/Swarm API
- Subscribes to Docker events for reactive updates
- Maintains the authoritative global state

The Agent:

- Does **not** mount the Docker socket
- Learns about the Swarm state exclusively through gRPC commands from the Hub
- Only reads the local filesystem (to verify written configs) and writes configuration files

## Consequences

Positive:

- **Reduced attack surface** — only the Hub container (running on the manager node under placement constraints) has Docker API access
- **Single source of truth** — no possibility of Hub and Agent disagreeing about the Swarm state
- **Simpler Agent** — no Docker client code, no event handling, no Swarm API queries
- **Easier cross-platform support** — no Docker socket path differences between Windows and Linux to handle in the Agent

Negative:

- **Hub is a single point of failure for state** — if the Hub is down, no new cross-node routes are created until it recovers
- **Network partition** — if an Agent loses gRPC connectivity to the Hub, it cannot create new routes
- **Latency** — Agent cannot react instantly; it depends on Hub detecting the Docker event and pushing a command

## Mitigations

- Hub restart is fast: it rebuilds state from the Docker API on startup
- Safety net polling (ADR-0003) ensures eventual consistency if notifications are missed
- gRPC keepalive and auto-reconnect handle temporary network interruptions
