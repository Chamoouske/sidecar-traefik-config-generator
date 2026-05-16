#!/bin/sh

# Detectar GID do socket do Docker e adicionar o usuário node ao grupo
if [ -S /var/run/docker.sock ]; then
    DOCKER_GID=$(stat -c '%g' /var/run/docker.sock)

    if ! getent group "$DOCKER_GID" > /dev/null; then
        addgroup -g "$DOCKER_GID" dockersock
    fi

    DOCKER_GROUP=$(getent group "$DOCKER_GID" | cut -d: -f1)
    addgroup $USER "$DOCKER_GROUP"
    echo "Usuário $USER adicionado ao grupo $DOCKER_GROUP (GID: $DOCKER_GID) para acesso ao Docker socket"
fi

exec su-exec node "$@"