# Arquitetura - Traefik Sidecar Config Generator

> **Projeto:** Gerenciamento dinâmico de configurações Traefik em cluster Docker Swarm híbrido (Windows + Linux) sem rede overlay.
>
> **Stack:** Go 1.26+, Docker SDK v28, Traefik File Provider, YAML v3, Sirupsen Logrus

---

## Índice

1. [Visão Geral do Sistema](#1-visão-geral-do-sistema)
2. [Diagrama de Arquitetura](#2-diagrama-de-arquitetura)
3. [Componentes Principais](#3-componentes-principais)
4. [Estrutura de Diretórios](#4-estrutura-de-diretórios)
5. [Modelos de Dados (pkg/models)](#5-modelos-de-dados-pkgmodels)
6. [Fluxos Detalhados](#6-fluxos-detalhados)
7. [API HTTP (internal/api)](#7-api-http-internalapi)
8. [Configuração do Traefik](#8-configuração-do-traefik)
9. [Variáveis de Ambiente](#9-variáveis-de-ambiente)
10. [Docker Swarm Labels](#10-docker-swarm-labels)
11. [Considerações de Segurança](#11-considerações-de-segurança)
12. [Glossário](#12-glossário)

---

## 1. Visão Geral do Sistema

O **Traefik Sidecar Config Generator** é um sistema de dois componentes (Hub e Agent) que automatiza a geração de configurações Traefik em clusters Docker Swarm heterogêneos (Windows + Linux) sem dependência de redes overlay.

### Arquitetura Hub-and-Spoke

```
                    ┌─────────────────────────────────────────────────────┐
                    │                   DOCKER SWARM                      │
                    │                                                      │
                    │  ┌───────────────────────────────────────────────┐  │
                    │  │              MANAGER NODE (Hub)                │  │
                    │  │                                               │  │
                    │  │  ┌───────────┐  ┌───────────┐  ┌───────────┐  │  │
                    │  │  │ Docker    │  │ State     │  │ Hub       │  │  │
                    │  │  │ Events    │──│ Manager   │──│ Server    │  │  │
                    │  │  │ Watcher   │  │           │  │ :8080     │  │  │
                    │  │  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘  │  │
                    │  │        │              │              │        │  │
                    │  │        └──────────────┼──────────────┘        │  │
                    │  │                         │                       │  │
                    │  │         ┌──────────────┴──────────────┐       │  │
                    │  │         │                             │       │  │
                    │  │    ┌─────┴─────────────────────────────┴────┐  │  │
                    │  │    │         Notifier (HTTP Push)           │  │  │
                    │  │    │                                       │  │  │
                    │  │    │  Agent1:9090  Agent2:9090  AgentN:9090│  │  │
                    │  │    └─────────────────┬─────────────────────┘  │  │
                    │  └──────────────────────┼────────────────────────┘  │
                    └─────────────────────────┼────────────────────────────┘
                                              │
                    ┌─────────────────────────┼────────────────────────────┐
                    │                         ▼                             │
                    │  ┌───────────────────────────────────────────────┐  │
                    │  │          sync_data (Volume Compartilhado)     │  │
                    │  │                                               │  │
                    │  │  shared/                                        │  │
                    │  │  ├── federation.yaml  (serviços federados)     │  │
                    │  │  ├── middlewares.yaml (middlewares globais)    │  │
                    │  │  └── local/                                    │  │
                    │  │      ├── routers/  (routers por serviço)        │  │
                    │  │      └── services/ (services por serviço)      │  │
                    │  └───────────────────────────────────────────────┘  │
                    │                                                      │
  ┌─────────────────┴────────────────────────────────────────────────────┐  │
  │              WORKER NODE 1 (Linux Agent)                            │  │
  │  ┌─────────────────────────────────────────────────────────────┐  │  │
  │  │  sidecar-agent (:9090)                                     │  │  │
  │  │                                                             │  │  │
  │  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │  │  │
  │  │  │ Agent    │  │ Local    │  │ Config   │  │ Orphan   │  │  │  │
  │  │  │ Receiver │──│ Watcher  │──│ Generator│──│ Cleaner  │  │  │  │
  │  │  │ /notify  │  │ (30s)    │  │          │  │          │  │  │  │
  │  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘  │  │  │
  │  └─────────────────────────────────────────────────────────────┘  │  │
  └────────────────────────────────────────────────────────────────────┘  │
                                                                       │
  ┌────────────────────────────────────────────────────────────────────┐  │
  │              WORKER NODE 2 (Linux Agent)                           │  │
  │  ┌─────────────────────────────────────────────────────────────┐  │  │
  │  │  sidecar-agent (:9090)                                     │  │  │
  │  └─────────────────────────────────────────────────────────────┘  │  │
  └────────────────────────────────────────────────────────────────────┘  │
                                                                       │
  ┌────────────────────────────────────────────────────────────────────┐  │
  │              WINDOWS NODE (Agent)                                  │  │
  │  ┌─────────────────────────────────────────────────────────────┐  │  │
  │  │  sidecar-agent (:9090)                                     │  │  │
  └────────────────────────────────────────────────────────────────────┘  │
                                                                       │
                    ┌─────────────────────────────────────────────────────┐
                    │                    TRAEFIK v3.7                   │
                    │    (File Provider + Docker Swarm Provider)         │
                    │                                                      │
                    │  ┌──────────────────────────────────────────────┐  │
                    │  │  EntryPoints: web(:80), websecure(:443)       │  │
                    │  │  Providers:                                    │  │
                    │  │  ├── file (/dynamic/shared/*.yaml)           │  │
                    │  │  └── swarm (docker.sock)                      │  │
                    │  └──────────────────────────────────────────────┘  │
                    └─────────────────────────────────────────────────────┘
```

### Fluxo de Dados

```
┌─────────────┐    docker events     ┌─────────────┐
│   Docker    │──────────────────────│    Hub      │
│   Swarm     │    service/node/task │  (Manager)  │
└─────────────┘                      └──────┬──────┘
                                           │
                    ┌──────────────────────┼──────────────────────┐
                    │         ┌────────────┴────────────┐         │
                    │         │    State Manager       │         │
                    │         │  - SetService()        │         │
                    │         │  - DeleteService()    │         │
                    │         │  - GetFederation()    │         │
                    │         └────────────┬────────────┘         │
                    │                      │                      │
                    │         ┌────────────┴────────────┐         │
                    │         │     Config Generator   │         │
                    │         │  - federation.yaml     │         │
                    │         │  - middlewares.yaml   │         │
                    │         └────────────┬────────────┘         │
                    │                      │                      │
                    │         ┌────────────┴────────────┐         │
                    │         │    Atomic Writer        │         │
                    │         │  (tempfile + rename)   │         │
                    │         └────────────┬────────────┘         │
                    └──────────────────────┼──────────────────────┘
                                           │
                    ┌──────────────────────┼──────────────────────┐
                    │         ┌────────────┴────────────┐         │
                    │         │   sync_data volume     │         │
                    │         │   (compartilhado)      │         │
                    │         └────────────┬────────────┘         │
                    │                      │                      │
                    │     ┌───────────────┼───────────────┐      │
                    │ ┌───┴───┐      ┌───┴───┐      ┌───┴───┐ │  │
                    │ │Agent 1│      │Agent 2│      │Agent N│ │  │
                    │ │Pull   │◄─────│Pull   │◄─────│Pull   │ │  │
                    │ │State  │      │State  │      │State  │ │  │
                    │ └───────┘      └───────┘      └───────┘ │  │
                    └────────────────────────────────────────────┘
```

---

## 2. Diagrama de Arquitetura

### 2.1 Hub Central (Manager Node)

O Hub executa no nó manager e é responsável por:

1. **Descoberta de Serviços**: Monitora eventos Docker (create/update/remove de serviços)
2. **Geração de Configurações**: Cria `federation.yaml` e `middlewares.yaml` para serviços federados
3. **Notificação de Agentes**: Envia HTTP POST para todos os agentes registrados
4. **Persistência de Estado**: Mantém estado do cluster em arquivo JSON

```
┌─────────────────────────────────────────────────────────────┐
│                         HUB                                 │
│  ┌─────────────────────────────────────────────────────┐   │
│  │                  Hub Server (:8080)                  │   │
│  │  GET /state     - retorna estado completo           │   │
│  │  GET /services  - lista serviços                     │   │
│  │  GET /health    - health check                      │   │
│  └─────────────────────────────────────────────────────┘   │
│                            │                                │
│  ┌─────────────────────────┼─────────────────────────────┐  │
│  │                   Event Loop                          │  │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  │  │
│  │  │ Docker     │  │ Service    │  │ Notifier   │  │  │
│  │  │ Events     │──│ Poller     │──│ (HTTP)     │  │  │
│  │  │ Watcher    │  │ (10s)      │  │            │  │  │
│  │  └────────────┘  └────────────┘  └────────────┘  │  │
│  └─────────────────────────┬─────────────────────────┘  │
│                            │                             │
│  ┌─────────────────────────┼─────────────────────────────┐│
│  │              State Manager                            ││
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐           ││
│  │  │ Generator│  │ Diff     │  │ File     │           ││
│  │  │          │  │ Engine   │  │ Store    │           ││
│  │  └──────────┘  └──────────┘  └──────────┘           ││
│  └─────────────────────────┬─────────────────────────────┘│
│                            │                               │
│  ┌─────────────────────────┼─────────────────────────────┐│
│  │              Atomic Writer                            ││
│  │  /etc/traefik-sidecar/shared/                         ││
│  │  ├── federation.yaml                                  ││
│  │  ├── middlewares.yaml                                 ││
│  │  └── .hub-state.json                                  ││
│  └───────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

### 2.2 Agente Local (Worker Node)

Cada nó worker executa um agente que:

1. **Recebe Notificações**: Endpoint HTTP POST /notify do Hub
2. **Monitora Containers Locais**: Polling periódico de containers
3. **Gera Configs Locais**: Cria arquivos `local/routers/` e `local/services/`
4. **Limpa Órfãos**: Remove configurações de serviços removidos

```
┌─────────────────────────────────────────────────────────────┐
│                        AGENT                                │
│  ┌─────────────────────────────────────────────────────┐   │
│  │             Agent Server (:9090)                     │   │
│  │  POST /notify   - receber notificações do Hub       │   │
│  │  GET /status    - status do agente                  │   │
│  │  GET /health    - health check                       │   │
│  └─────────────────────────────────────────────────────┘   │
│                            │                                │
│  ┌─────────────────────────┼─────────────────────────────┐  │
│  │                  Agent Handler                       │  │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  │  │
│  │  │ Hub Client │  │ Local      │  │ Container  │  │  │
│  │  │ (pull)     │──│ Watcher    │──│ Resolver   │  │  │
│  │  └────────────┘  └────────────┘  └────────────┘  │  │
│  └─────────────────────────┬─────────────────────────┘  │
│                            │                             │
│  ┌─────────────────────────┼─────────────────────────────┐│
│  │              State Manager                            ││
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐           ││
│  │  │ Generator│  │ Diff     │  │ State    │           ││
│  │  │          │  │ Engine   │  │ File     │           ││
│  │  └──────────┘  └──────────┘  └──────────┘           ││
│  └─────────────────────────┬─────────────────────────────┘│
│                            │                               │
│  ┌─────────────────────────┼─────────────────────────────┐│
│  │              Atomic Writer                            ││
│  │  /etc/traefik-sidecar/shared/local/                   ││
│  │  ├── routers/                                       ││
│  │  │   └── {service}.yaml                            ││
│  │  ├── services/                                      ││
│  │  │   └── {service}.yaml                            ││
│  │  └── .agent-state.json                              ││
│  └───────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

---

## 3. Componentes Principais

### 3.1 Hub (cmd/hub/main.go)

**Responsabilidade**: Coordenação central, descoberta de serviços, notificação de agentes.

**Flags**:
- `-config-dir`: Diretório de configurações (default: `/etc/traefik-sidecar/shared`)
- `-state-file`: Arquivo de estado (default: `<config-dir>/.hub-state.json`)
- `-traefik-port`: Porta do Traefik (default: 80)
- `-bridge-name`: Nome da bridge Docker (default: `traefik_bridge`)
- `-hub-addr`: Endereço do servidor HTTP (default: `:8080`)
- `-advertise-addr`: Endereço IP:porta para anunciar aos agentes (auto-descoberta se vazio)
- `-docker-host`: Endpoint Docker (default: `unix:///var/run/docker.sock`)
- `-log-level`: Nível de log (default: `info`)

**Dependências**:
- `internal/hub` - Lógica do Hub
- `internal/events` - Docker Events Watcher e Service Poller
- `internal/config` - Generator, Diff Engine, State Manager
- `internal/writer` - Atomic Writer
- `internal/api` - HubServer e HubClient
- `internal/discovery` - Node e Container Resolver

### 3.2 Agent (cmd/agent/main.go)

**Responsabilidade**: Receber notificações, monitorar containers locais, gerar configurações.

**Flags**:
- `-config-dir`: Diretório de configs locais (default: `/etc/traefik-sidecar/local`)
- `-bridge-name`: Nome da bridge (default: `traefik_bridge`)
- `-hub-addr`: Endereço do Hub (default: `localhost:8080`)
- `-agent-port`: Porta do servidor HTTP (default: 9090)
- `-traefik-port`: Porta do Traefik (default: 80)
- `-docker-host`: Endpoint Docker (default: `unix:///var/run/docker.sock`)
- `-log-level`: Nível de log (default: `info`)

**Dependências**:
- `internal/agent` - Lógica do Agente
- `internal/config` - Generator, Diff Engine, State Manager
- `internal/writer` - Atomic Writer
- `internal/api` - AgentServer e HubClient
- `internal/discovery` - Container Resolver

---

## 4. Estrutura de Diretórios

```
.
├── cmd/
│   ├── agent/
│   │   └── main.go              # Entry point do agente
│   └── hub/
│       └── main.go              # Entry point do hub
├── internal/
│   ├── agent/
│   │   ├── agent.go             # Lógica principal do agente
│   │   ├── local_watcher.go     # Monitoramento de containers locais
│   │   └── orphan_cleaner.go    # Limpeza de configs órfãs
│   ├── api/
│   │   ├── api.go               # Interface shared do API
│   │   ├── server.go            # Servidor HTTP (AgentServer/HubServer)
│   │   ├── client.go            # Cliente HTTP (HubClient)
│   │   └── api_test.go          # Testes unitários
│   ├── config/
│   │   ├── config.go            # Interface shared
│   │   ├── generator.go         # Geração de configs YAML
│   │   ├── diff.go              # Engine de diff
│   │   ├── state.go             # State Manager
│   │   └── config_test.go       # Testes
│   ├── discovery/
│   │   ├── discovery.go         # Interface shared
│   │   ├── node.go              # Resolução de nós Swarm
│   │   └── container.go         # Resolução de IPs de containers
│   ├── events/
│   │   ├── events.go            # Interface shared e tipos
│   │   ├── watcher.go           # Docker Events Watcher
│   │   └── poller.go            # Service Poller (polling)
│   ├── hub/
│   │   ├── hub.go               # Lógica principal do hub
│   │   └── hub_notifier.go      # Notificação de agentes
│   └── writer/
│       ├── writer.go            # Atomic Writer
│       └── writer_test.go       # Testes unitários
├── pkg/
│   └── models/
│       ├── models.go            # Modelos de dados e interfaces
│       └── models_test.go       # Testes unitários
├── test/
│   ├── fixtures/                # Arquivos de referência para testes
│   └── integration/             # Testes de integração
├── Dockerfile.hub               # Build do Hub
├── Dockerfile.agent             # Build do Agent
├── docker-compose.yml           # Orquestração local
├── go.mod                       # Dependências Go
├── ARCHITECTURE.md              # Este documento
└── README.md                    # Documentação principal
```

---

## 5. Modelos de Dados (pkg/models)

### 5.1 Entidades Principais

| Estrutura | Descrição |
|-----------|-----------|
| `ServiceMeta` | Metadados extraídos das labels Docker (host, port, tls, entrypoints, middlewares) |
| `NodeInfo` | Informações de um nó Swarm (ID, hostname, IP, role) |
| `LocalTaskInfo` | Task local com IP na bridge |
| `FederationTarget` | Alvo de federamento (IP do nó remoto) |
| `ClusterState` | Estado completo do cluster (serviços, nós, tasks, agentes) |

### 5.2 Eventos e Ações

| Tipo | Descrição |
|------|-----------|
| `ActionCreate` | Serviço criado |
| `ActionUpdate` | Serviço atualizado |
| `ActionDelete` | Serviço removido |
| `EventServiceCreate/Update/Remove` | Eventos Docker de serviço |
| `EventTaskDeploy/Remove` | Eventos Docker de task |

### 5.3 Configuração Traefik

| Estrutura | Descrição |
|-----------|-----------|
| `TraefikConfig` | Configuração YAML completa (HTTP + TCP) |
| `HTTPConfig` | Routers, Services e Middlewares HTTP |
| `RouterConfig` | Regra de roteamento (Host, PathPrefix, etc) |
| `ServiceConfig` | Load balancer com servers |
| `MiddlewareConfig` | Middleware (headers, rate limit, retry) |

---

## 6. Fluxos Detalhados

### 6.1 Hub: Descoberta e Notificação

```
1. Hub.Start()
   ├── Inicia HubServer (:8080)
   ├── Descobre HubAddr (advertise-addr ou auto-descoberta)
   ├── Carrega estado anterior do disco
   └── Descobre agentes via Swarm API

2. Event Loop
   ├── Espera eventos do Docker Events Watcher
   ├── Ou espera eventos do Service Poller (10s)
   ├── Para cada evento:
   │   ├── ParseServiceMeta (extrai labels)
   │   ├── StateManager.SetService() / DeleteService()
   │   └── Generator.GenerateFederationConfig()
   └── AtomicWriter.WriteConfig("federation.yaml")

3. Notificação
   └── Para cada agente online:
       └── HubClient.NotifyAgent() via HTTP POST
```

### 6.2 Agent: Recebimento de Notificação

```
1. Agent.Start()
   ├── Inicia AgentServer (:9090)
   ├── Carrega estado anterior do disco
   ├── Limpa configs legados (routers.yaml, services.yaml)
   └── Inicia LocalWatcher (polling 30s)

2. POST /notify (do Hub)
   ├── Parse NotificationPayload
   │   └── Action: CREATE, UPDATE, DELETE
   ├── Se CREATE/UPDATE:
   │   ├── HubClient.GetService() → ServiceMeta
   │   └── StateManager.SetService()
   ├── Se DELETE:
   │   └── StateManager.DeleteService()
   └── generateLocalConfigs()
       ├── ListLocalContainers() → LocalTasks
       └── Para cada serviço:
           ├── Se local: GenerateLocalConfig() → bridge IP
           └── Se remote: GenerateFederationRouterConfig()

3. LocalWatcher (30s)
   ├── ListLocalContainers()
   ├── Compare com estado anterior
   └── generateLocalConfigs() se mudou
```

### 6.3 Geração de Configurações

**Hub**: `federation.yaml` - Services apontando para nós remotos

```yaml
http:
  services:
    service-app-federation:
      loadBalancer:
        servers:
          - url: "http://192.168.1.10:80"
```

**Agent Local**: `local/{service}/routers.yaml` + `local/{service}/services.yaml`

```yaml
# local/app/routers.yaml
http:
  routers:
    app-local-router:
      rule: "Host(`app.internal`)"
      service: app-local-service
      entryPoints:
        - web

# local/app/services.yaml
http:
  services:
    app-local-service:
      loadBalancer:
        servers:
          - url: "http://10.0.0.2:3000"
```

---

## 7. API HTTP (internal/api)

### 7.1 HubServer (/:8080)

| Endpoint | Método | Descrição |
|----------|--------|-----------|
| `/health` | GET | Health check |
| `/state` | GET | Retorna ClusterState completo |
| `/services/{name}` | GET | Retorna ServiceMeta específico |

### 7.2 AgentServer (/:9090)

| Endpoint | Método | Descrição |
|----------|--------|-----------|
| `/health` | GET | Health check |
| `/notify` | POST | Recebe notificação do Hub |
| `/status` | GET | Retorna status do agente |

### 7.3 Notificação (Hub → Agent)

**Request** (Hub → Agent):
```json
{
  "action": "UPDATE",
  "service_name": "myapp",
  "hub_addr": "192.168.1.10:8080",
  "timestamp": "2026-05-22T12:00:00Z"
}
```

**Response** (Agent → Hub):
```json
{
  "status": "ok",
  "configs_updated": true
}
```

---

## 8. Configuração do Traefik

### 8.1 EntryPoints

```yaml
entryPoints:
  web: ":80"
  websecure: ":443"
```

### 8.2 Providers

```yaml
providers:
  file:
    directory: "/dynamic/shared"
    watch: true
  
  swarm:
    endpoint: "unix:///var/run/docker.sock"
    exposedByDefault: false
```

### 8.3 Arquivos Dinâmicos

```
/dynamic/shared/
├── federation.yaml        # Services federados (Hub)
├── middlewares.yaml       # Middlewares globais (Hub)
└── local/
    ├── routers/
    │   └── {service}.yaml # Routers locais (Agent)
    └── services/
        └── {service}.yaml # Services locais (Agent)
```

---

## 9. Variáveis de Ambiente

### 9.1 Hub

| Variável | Default | Descrição |
|----------|---------|-----------|
| `TRAEFIK_SIDECAR_CONFIG_DIR` | `/etc/traefik-sidecar/shared` | Diretório de configs |
| `TRAEFIK_SIDECAR_STATE_FILE` | `.hub-state.json` | Arquivo de estado |
| `TRAEFIK_SIDECAR_TRAEFIK_PORT` | `80` | Porta do Traefik |
| `TRAEFIK_SIDECAR_BRIDGE_NAME` | `traefik_bridge` | Nome da bridge |
| `TRAEFIK_SIDECAR_HUB_ADDR` | `:8080` | Endereço do Hub |
| `TRAEFIK_SIDECAR_HUB_ADVERTISE_ADDR` | `` | Endereço para agentes |
| `TRAEFIK_SIDECAR_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker socket |
| `TRAEFIK_SIDECAR_LOG_LEVEL` | `info` | Nível de log |

### 9.2 Agent

| Variável | Default | Descrição |
|----------|---------|-----------|
| `TRAEFIK_SIDECAR_CONFIG_DIR` | `/etc/traefik-sidecar/local` | Diretório de configs |
| `TRAEFIK_SIDECAR_BRIDGE_NAME` | `traefik_bridge` | Nome da bridge |
| `TRAEFIK_SIDECAR_HUB_ADDR` | `localhost:8080` | Endereço do Hub |
| `TRAEFIK_SIDECAR_AGENT_PORT` | `9090` | Porta do agente |
| `TRAEFIK_SIDECAR_TRAEFIK_PORT` | `80` | Porta do Traefik |
| `TRAEFIK_SIDECAR_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker socket |
| `TRAEFIK_SIDECAR_LOG_LEVEL` | `info` | Nível de log |

---

## 10. Docker Swarm Labels

### 10.1 Labels de Federação

| Label | Obrigatório | Descrição |
|-------|-------------|-----------|
| `traefik.federation.enable` | Sim | Habilita o serviço para federação |
| `traefik.federation.host` | Sim | Host de roteamento |
| `traefik.federation.port` | Sim | Porta do container |
| `traefik.federation.protocol` | Não | Protocolo (http/https), default: http |
| `traefik.federation.entrypoints` | Não | Entrypoints (web,websecure) |
| `traefik.federation.middlewares` | Não | Middlewares (name@file) |
| `traefik.federation.tls` | Não | Habilita TLS |

### 10.2 Exemplo Completo

```yaml
services:
  myapp:
    image: myapp:latest
    deploy:
      labels:
        - "traefik.federation.enable=true"
        - "traefik.federation.host=myapp.internal"
        - "traefik.federation.port=3000"
        - "traefik.federation.entrypoints=web,websecure"
        - "traefik.federation.middlewares=cors@file,rate-limit@file"
        - "traefik.federation.tls=true"
```

---

## 11. Considerações de Segurança

1. **Mínimo Privilégio**: Containers montam Docker socket como read-only
2. **Atomic Writes**: Previnem leitura de arquivos parcialmente escritos
3. **Validação de Labels**: Labels inválidas são ignoradas com log warning
4. **Timeouts**: Operações HTTP têm timeout configurável
5. **Graceful Shutdown**: Componentes finalizam corretamente com SIGTERM

---

## 12. Glossário

| Termo | Definição |
|-------|-----------|
| **Hub** | Nó manager que centraliza descoberta e notificação |
| **Agent** | Nó worker que gera configs locais |
| **Federação** | Roteamento entre nós sem overlay network |
| **Bridge IP** | IP do container na rede bridge local |
| **Atomic Write** | Padrão tempfile + fsync + rename |
| **Orphan Cleaner** | Limpeza de configs de serviços removidos |
| **Service Poller** | Polling periódico da Swarm API |

---

*Documento de Arquitetura v2.0 — Atualizado em 2026-05-22*
