# ============================================================
# Dockerfile - Sidecar Traefik
# Multi-stage build: Go application -> minimal Alpine runtime
# ============================================================

# ---------- Stage 1: Builder ----------
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Cache dependencies in a separate layer
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/sidecar ./cmd/sidecar

# ---------- Stage 2: Runtime ----------
FROM alpine:3.21

# Install runtime dependencies (CA certificates for TLS, tzdata for timezone)
RUN apk --no-cache add ca-certificates tzdata

# Copy the compiled binary from builder stage
COPY --from=builder /app/sidecar /sidecar

# OCI container labels
LABEL org.opencontainers.image.source="https://github.com/chamoouske/sidecar"
LABEL org.opencontainers.image.description="Sidecar service for Traefik dynamic configuration generation"
LABEL org.opencontainers.image.version="latest"
LABEL org.opencontainers.image.title="Sidecar Traefik"

ENTRYPOINT ["/sidecar"]
