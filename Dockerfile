# Stage 1: Build
FROM node:22-alpine AS builder

WORKDIR /app

# Copiar arquivos de dependências primeiro para aproveitar o cache das camadas
COPY package.json package-lock.json ./

# Instalar todas as dependências (incluindo devDependencies para o build)
RUN npm ci

# Copiar o restante do código fonte e arquivos de configuração
COPY tsconfig.json ./
COPY src/ ./src/

# Executar o build do TypeScript
RUN npm run build

# Stage 2: Runtime
FROM node:22-alpine

# Instalar dependências para o entrypoint (ajuste de UID/GID e troca de usuário)
RUN apk add --no-cache su-exec shadow

# Definir variáveis de ambiente
ENV NODE_ENV=production

WORKDIR /app

# Criar diretórios de dados
RUN mkdir -p /data/shared /data/local

# Copiar apenas os arquivos necessários para instalação de produção
COPY package.json package-lock.json ./

# Instalar dependências de produção
RUN npm ci --omit=dev

# Copiar o código compilado do estágio anterior
COPY --from=builder /app/dist ./dist

# Copiar script de entrypoint
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# O usuário deve gerenciar volumes via docker-compose ou docker run.
# Evitamos declarar VOLUME aqui para não criar volumes anônimos que podem conflitar
# com montagens no diretório pai (/data).

# Usar o script de entrypoint para gerenciar permissões
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# Comando de inicialização (será passado como argumentos para o entrypoint)
CMD ["node", "dist/index.js"]
