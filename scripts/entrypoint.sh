#!/bin/sh

PUID=${PUID:-1000}
PGID=${PGID:-1000}

if [ -n "$PUID" ] && [ "$PUID" != "0" ]; then
  groupmod -o -g "$PGID" abc 2>/dev/null || true
  usermod -o -u "$PUID" abc 2>/dev/null || true
  chown -R abc:abc /etc/traefik-sidecar 2>/dev/null || true
  exec su-exec abc "$@"
else
  exec "$@"
fi
