#!/bin/sh

# Ajustar UID do usuário node se PUID for fornecido
if [ ! -z "$PUID" ]; then
    if [ "$PUID" != "$(id -u node)" ]; then
        echo "Ajustando UID do usuário node para $PUID"
        usermod -o -u "$PUID" node
    fi
fi

# Ajustar GID do usuário node se GUID for fornecido
if [ ! -z "$GUID" ]; then
    if [ "$GUID" != "$(id -g node)" ]; then
        echo "Ajustando GID do usuário node para $GUID"
        groupmod -o -g "$GUID" node
    fi
fi

# Detectar GID do socket do Docker e adicionar o usuário node ao grupo
if [ -S /var/run/docker.sock ]; then
    DOCKER_GID=$(stat -c '%g' /var/run/docker.sock)
    # Criar um grupo temporário para o docker socket se não existir
    if ! getent group "$DOCKER_GID" > /dev/null; then
        addgroup -g "$DOCKER_GID" dockersock
    fi
    # Adicionar o usuário node ao grupo que possui o socket
    DOCKER_GROUP=$(getent group "$DOCKER_GID" | cut -d: -f1)
    addgroup node "$DOCKER_GROUP"
    echo "Usuário node adicionado ao grupo $DOCKER_GROUP (GID: $DOCKER_GID) para acesso ao Docker socket"
fi

# Garantir permissões nos diretórios de dados se houver mudança de UID/GID
chown -R node:node /app /data/shared /data/local

# Executar o comando principal como usuário node usando su-exec
# Isso permite que o container rode como root inicialmente para configurar permissões
# e depois mude para o usuário node para rodar a aplicação.
exec su-exec node "$@"
