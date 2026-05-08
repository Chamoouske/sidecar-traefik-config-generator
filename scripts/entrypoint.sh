#!/bin/sh

# Ajustar UID do usuário node se PUID for fornecido
if [ ! -z "$PUID" ]; then
    if [ "$PUID" != "$(id -u node)" ]; then
        echo "Ajustando UID do usuário node para $PUID"
        usermod -o -u "$PUID" node
    fi
fi

# Ajustar GID do usuário node se GUID ou PGID for fornecido
if [ ! -z "$GUID" ] || [ ! -z "$PGID" ]; then
    TARGET_GID=${GUID:-$PGID}
    if [ "$TARGET_GID" != "$(id -g node)" ]; then
        echo "Ajustando GID do usuário node para $TARGET_GID"
        groupmod -o -g "$TARGET_GID" node
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

# Logar pontos de montagem e permissões para debug
echo "--- Informações de Debug de Volume ---"
echo "ID atual: $(id)"
echo "Conteúdo de /data:"
ls -la /data
echo "Mounts ativos em /data:"
mount | grep /data
echo "--------------------------------------"

# Garantir permissões nos diretórios de dados
# Se /data for um volume, queremos garantir que o usuário node possa escrever nele
if [ -d /data ]; then
    echo "Ajustando permissões em /data recursivamente..."
    chown -R node:node /data
fi
chown -R node:node /app

# Executar o comando principal como usuário node usando su-exec
# Isso permite que o container rode como root inicialmente para configurar permissões
# e depois mude para o usuário node para rodar a aplicação.
exec su-exec node "$@"
