# Traefik Sidecar 🚀

Gerador de configurações dinâmicas do Traefik para clusters Docker Swarm híbridos (Windows + Linux) **sem rede overlay**, utilizando o padrão **Hub-and-Spoke com Observer**.

## 📋 Tabela de Conteúdos

- [Arquitetura](#arquitetura)
- [Como Funciona](#como-funciona)
- [Pré-requisitos](#pré-requisitos)
- [Deploy no Swarm](#deploy-no-swarm)
- [Estrutura do Projeto](#estrutura-do-projeto)
- [Configuração](#configuração)
- [Testes](#testes)
- [Monitoramento](#monitoramento)
- [Resiliência](#resiliência)
- [Contribuição](#contribuição)
- [Licença](#licença)

## 🏗️ Arquitetura

### Hub-and-Spoke com Observer

```
┌─────────────────────────────────────────────────────────────┐
│                    Docker Swarm Cluster                      │
│                                                              │
│  ┌──────────┐    Observer Push    ┌──────────────────┐      │
│  │   Hub    │────────────────────▶│   Agent (Nó 1)   │      │
│  │ Central  │◀────────────────────│                  │      │
│  │(Manager) │   Pull on Demand    │  ┌────────────┐  │      │
│  │          │                     │  │  Traefik   │  │      │
│  │  ┌─────┐ │                     │  │  Instância │  │      │
│  │  │Docker│ │                     │  └────────────┘  │      │
│  │  │Events│ │                     └──────────────────┘      │
│  │  └─────┘ │                                                │
│  │          │    Observer Push    ┌──────────────────┐      │
│  │  ┌─────┐ │────────────────────▶│   Agent (Nó 2)   │      │
│  │  │Poll  │ │◀────────────────────│                  │      │
│  │  │Swarm │ │   Pull on Demand    │  ┌────────────┐  │      │
│  │  └─────┘ │                     │  │  Traefik   │  │      │
│  │          │                     │  │  Instância │  │      │
│  └──────────┘                     └──────────────┘  │      │
│                                           └────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### Componentes

| Componente | Descrição |
|------------|-----------|
| **Hub Central** | Roda no nó manager. Escuta eventos Docker + polling Swarm. Gera configs compartilhadas (federation/middlewares). Notifica agentes via push HTTP. |
| **Agente Local** | Roda em cada nó (modo global). Recebe notificações, faz pull seletivo, monitora containers locais, gera configs locais (routers/services). |
| **Traefik (File Provider)** | Uma instância por nó (modo global). Lê configs do diretório dinâmico com `watch: true`. |

## 🔄 Como Funciona

### Fluxo de Detecção de Mudanças

1. **Hub detecta mudança** via Docker Events (tempo real) ou polling Swarm (fallback a cada 10s)
2. **Hub gera configs compartilhadas** (`shared/federation.yaml`, `shared/middlewares.yaml`) com diff incremental
3. **Hub notifica agentes** via HTTP POST para `/notify` com backoff exponencial (3 tentativas)
4. **Agente recebe notificação** e faz pull seletivo (`GET /services/<name>` ou `GET /state`)
5. **Agente gera configs locais**:
   - Se container **está local**: router aponta para IP da bridge local (`http://10.0.0.2:8080`)
   - Se container **não está local**: router aponta para federação (`http://<node-ip>:80` — cascata)
6. **Traefik detecta mudança** no YAML (File Provider com `watch: true`) e recarrega

### Cascata de Traefiks

```
Cliente ──▶ http://nginx.app.local
                │
                ▼
        ┌───────────────┐
        │  Traefik Nó A │ ← Router local: Host(`nginx.app.local`)
        │  (requisição  │     → Service: nginx-federation
        │   chega aqui) │         → http://192.168.1.20:80 (Traefik Nó B)
        └───────┬───────┘
                │ http://192.168.1.20:80 (preserva Host header)
                ▼
        ┌───────────────┐
        │  Traefik Nó B │ ← Router: Host(`nginx.app.local`)
        │  (container   │     → Service: nginx-local
        │   está aqui)  │         → http://10.0.0.5:8080 (container na bridge)
        └───────────────┘
```

### Polling Local do Agente

A cada 30s, o Agente verifica:

- Containers Swarm que apareceram no nó (tasks que migraram)
- Containers que desapareceram (tasks finalizadas)
- IPs na bridge que mudaram (DHCP, recriação)
- Remove arquivos YAML órfãos

## 📋 Pré-requisitos

- Docker Engine 24+ com Swarm inicializado
- Docker Compose 2.20+
- Go 1.22+ (para desenvolvimento)
- Acesso ao Docker socket em todos os nós
- Rede bridge com mesmo nome em todos os nós (ex: `traefik_bridge`)

### Rede Bridge

```bash
# Criar em CADA nó (mesmo nome, mesmo driver)
docker network create -d bridge --scope swarm --attachable traefik_bridge
```

### Labels dos Serviços

Para um serviço ser gerenciado, adicione estas labels:

```yaml
services:
  meu-servico:
    deploy:
      labels:
        - "traefik.federation.enabled=true"
        - "traefik.federation.host=meuservico.app.local"
        - "traefik.federation.port=3000"
        - "traefik.federation.tls=false"
        - "traefik.federation.entrypoints=web,websecure"
        - "traefik.federation.middlewares=cors,ratelimit"
```

## 🚀 Deploy no Swarm

### 1. Build das Imagens

```bash
# Build Hub
docker build -t traefik-sidecar-hub:latest -f Dockerfile.hub .

# Build Agent
docker build -t traefik-sidecar-agent:latest -f Dockerfile.agent .
```

### 2. Deploy da Stack

```bash
docker stack deploy -c docker-compose.yml traefik-sidecar
```

### 3. Verificar Deploy

```bash
# Verificar serviços
docker stack services traefik-sidecar

# Verificar logs do Hub
docker service logs traefik-sidecar_hub

# Verificar logs do Agent
docker service logs traefik-sidecar_agent
```

### 4. Verificar Configs Geradas

```bash
# Configs compartilhadas (volumes)
docker exec $(docker ps -f name=hub -q) ls -la /etc/traefik-sidecar/shared/

# Configs locais
docker exec $(docker ps -f name=agent -q) ls -la /etc/traefik-sidecar/local/
```

### Ambiente Híbrido Windows + Linux

Para ambientes Windows:

1. Substitua `unix:///var/run/docker.sock` por `npipe:////./pipe/docker_engine` nos serviços
2. Para o Traefik no Windows, use imagem `traefik:windows-ltsc2022` ou similar
3. A rede bridge pode ser criada com driver `nat` no Windows:

   ```powershell
   docker network create -d nat --scope swarm --attachable traefik_bridge
   ```

## 📁 Estrutura do Projeto

```
├── cmd/
│   ├── hub/main.go         # Ponto de entrada do Hub Central
│   └── agent/main.go       # Ponto de entrada do Agente Local
├── internal/
│   ├── hub/
│   │   └── hub.go          # Lógica do Hub (eventos, notificações, órfãos)
│   ├── agent/
│   │   ├── agent.go        # Lógica do Agente
│   │   ├── local_watcher.go # Polling de containers locais
│   │   └── orphan_cleaner.go # Limpeza de YAMLs órfãos
│   ├── api/
│   │   ├── server.go       # HTTP Server (Agente + Hub)
│   │   ├── client.go       # HTTP Client (Hub → Agente)
│   │   └── api_test.go     # Testes unitários da API
│   ├── config/
│   │   ├── diff.go         # Detecção incremental de mudanças
│   │   ├── generator.go    # Geração de YAML do Traefik
│   │   └── state.go        # Gerenciamento de estado
│   ├── discovery/
│   │   ├── node.go         # Resolução de IPs de nós Swarm
│   │   └── container.go    # Resolução de IPs de containers
│   ├── events/
│   │   ├── watcher.go      # Docker Events em tempo real
│   │   └── poller.go       # Polling periódico da API Swarm
│   └── writer/
│       └── writer.go       # Escrita atômica de arquivos
├── pkg/models/
│   └── models.go           # Modelos compartilhados e interfaces
├── test/
│   ├── fixtures/           # Golden files para testes
│   └── integration/
│       └── swarm_test.go   # Testes de integração
├── Dockerfile.hub          # Build do Hub
├── Dockerfile.agent        # Build do Agente
├── docker-compose.yml      # Deploy Swarm
└── README.md
```

## ⚙️ Configuração

### Variáveis de Ambiente

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `TRAEFIK_SIDECAR_CONFIG_DIR` | `/etc/traefik-sidecar/shared` | Diretório de configs |
| `TRAEFIK_SIDECAR_TRAEFIK_PORT` | `80` | Porta do Traefik |
| `TRAEFIK_SIDECAR_BRIDGE_NAME` | `traefik_bridge` | Nome da bridge local |
| `TRAEFIK_SIDECAR_HUB_ADDR` | `:8080` | Endereço do Hub |
| `TRAEFIK_SIDECAR_AGENT_PORT` | `9090` | Porta do Agente |
| `TRAEFIK_SIDECAR_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker socket |
| `TRAEFIK_SIDECAR_LOG_LEVEL` | `info` | Nível de log |

## 🧪 Testes

### Testes Unitários

```bash
# Executar todos os testes unitários
go test ./...

# Ver cobertura
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

**Cobertura atual:** > 70% (models: 100%, config: 89%, writer: 81%, api: 87%)

### Testes de Integração

Os testes de integração usam Testcontainers e requerem Docker Engine.

```bash
# Executar testes de integração (Linux/macOS)
go test -tags=integration -v ./test/integration/

# Executar apenas testes unitários (pular integração)
go test -short ./...
```

**Cobertura dos testes de integração:**

- Geração de federação (`federation.yaml`)
- Config local com container presente (bridge IP)
- Config cascata com container ausente (federation)
- Notificação push Hub → Agente
- Diff incremental de mudanças
- Orphan cleanup
- Escrita atômica consistente
- API endpoints (`/health`, `/services`, `/state`)

## 📊 Monitoramento

### Endpoints HTTP

#### Hub Central

| Endpoint | Método | Descrição |
|----------|--------|-----------|
| `/health` | GET | Healthcheck |
| `/services/{name}` | GET | Metadata de serviço |
| `/state` | GET | Estado completo do cluster |

#### Agente Local

| Endpoint | Método | Descrição |
|----------|--------|-----------|
| `/notify` | POST | Recebe notificações push do Hub |
| `/status` | GET | Status do agente |

### Logs

Todos os logs em formato JSON. Nível configurável via `TRAEFIK_SIDECAR_LOG_LEVEL`.

```json
{"component":"hub","level":"info","msg":"federation updated","changes":{"added":["nginx"],"removed":null,"modified":null,"has_changes":true},"time":"2024-01-01T00:00:00Z"}
{"component":"agent","level":"debug","msg":"received notification","action":"UPDATE","service":"nginx","time":"2024-01-01T00:00:01Z"}
```

## 🔒 Resiliência

| Cenário | Comportamento |
|---------|---------------|
| Hub offline | Agente mantém última config válida. Polling local continua. |
| Agente offline | Hub tenta reenviar com backoff (1s, 2s, 4s). Marca como offline e tenta reconectar a cada 30s. |
| Docker socket indisponível | Retry com backoff exponencial. Log de erro. |
| Escrita atômica | Tempfile + rename garantem consistência mesmo em falha. |
| Notificação perdida | Polling local do Agente (30s) detecta mudanças como fallback. |
| IP de nó mudou (DHCP) | Hub detecta no próximo ciclo de polling e atualiza federação. |
| Container recriado | Polling local detecta novo IP na bridge e atualiza configs. |

## 🤝 Contribuição

1. Fork o projeto
2. Crie sua branch (`git checkout -b feature/amazing-feature`)
3. Commit suas mudanças (`git commit -m 'feat: add amazing feature'`)
4. Push para a branch (`git push origin feature/amazing-feature`)
5. Abra um Pull Request

## 📄 Licença

MIT License — veja o arquivo LICENSE para detalhes.
