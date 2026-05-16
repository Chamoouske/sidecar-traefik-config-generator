# Arquitetura - Traefik Dynamic Config Generator para Docker Swarm Híbrido

> **Projeto:** `sidecar` — Geração dinâmica de configurações Traefik em cluster Docker Swarm híbrido (Windows + Linux) sem rede overlay.
>
> **Stack:** Go 1.22+, Docker SDK, Traefik File Provider, Syncthing, YAML v3

---

## Índice

1. [Diagrama de Arquitetura](#1-diagrama-de-arquitetura)
2. [Estrutura de Diretórios](#2-estrutura-de-diretórios)
3. [Modelos de Dados (pkg/models)](#3-modelos-de-dados-pkgmodels)
4. [Interfaces e Contratos (ISP/DIP)](#4-interfaces-e-contratos-ispdip)
5. [Especificação dos Pacotes](#5-especificação-dos-pacotes)
6. [Fluxos Detalhados](#6-fluxos-detalhados)
7. [Arquivos YAML Gerados](#7-arquivos-yaml-gerados)
8. [Padrões de Concorrência](#8-padrões-de-concorrência)
9. [Tratamento de Erros e Resiliência](#9-tratamento-de-erros-e-resiliência)
10. [Estratégia de Testes](#10-estratégia-de-testes)

---

## 1. Diagrama de Arquitetura

### 1.1 Visão Geral Hub-and-Spoke

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         DOCKER SWARM CLUSTER                                │
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │                      MANAGER NODE (Hub)                               │  │
│  │                                                                       │  │
│  │  ┌─────────────┐  ┌──────────────┐  ┌─────────────┐                 │  │
│  │  │ Docker       │  │ State        │  │ Agent       │                 │  │
│  │  │ Events       │  │ Manager      │  │ Registry    │                 │  │
│  │  │ Watcher      │─>│              │  │ (Heartbeat) │                 │  │
│  │  │ (type=service│  │ ┌──────────┐ │  └──────┬──────┘                 │  │
│  │  │  + polling)  │  │ │ Diff     │ │         │                        │  │
│  │  └──────┬───────┘  │ │ Engine   │ │         │                        │  │
│  │         │          │ └──────────┘ │         │                        │  │
│  │         │          └──────┬───────┘         │                        │  │
│  │         │                 │                 │                        │  │
│  │         └─────────────────┼─────────────────┘                        │  │
│  │                           │                                          │  │
│  │                           ▼                                          │  │
│  │  ┌────────────────────────────────────────────────────────────┐      │  │
│  │  │                 Notifier (HTTP Push)                        │      │  │
│  │  │  ┌─────────────┐  ┌──────────────┐  ┌──────────────┐      │      │  │
│  │  │  │ Agent 1     │  │ Agent 2      │  │ Agent N      │      │      │  │
│  │  │  │ http://     │  │ http://      │  │ http://      │      │      │  │
│  │  │  │ node1:9090  │  │ node2:9090   │  │ nodeN:9090   │      │      │  │
│  │  │  └──────┬──────┘  └──────┬───────┘  └──────┬───────┘      │      │  │
│  │  └─────────┼────────────────┼──────────────────┼──────────────┘      │  │
│  │            │                │                  │                     │  │
│  │            ▼                ▼                  ▼                     │  │
│  │  ┌────────────────────────────────────────────────────────────┐      │  │
│  │  │              shared/ (Syncthing Volume)                     │      │  │
│  │  │  ┌──────────────────┐  ┌──────────────────┐               │      │  │
│  │  │  │ federation.yaml  │  │ middlewares.yaml  │               │      │  │
│  │  │  │ (roteamento      │  │ (middlewares      │               │      │  │
│  │  │  │  entre nós)      │  │  compartilhados)  │               │      │  │
│  │  │  └──────────────────┘  └──────────────────┘               │      │  │
│  │  └────────────────────────────────────────────────────────────┘      │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌──────────────────────┐    ┌──────────────────────┐    ┌──────────────┐  │
│  │   WORKER NODE 1      │    │   WORKER NODE 2      │    │  WINDOWS     │  │
│  │   (Linux Agent)      │    │   (Linux Agent)      │    │  NODE        │  │
│  │                      │    │                      │    │  (Agent)     │  │
│  │  ┌────────────────┐  │    │  ┌────────────────┐  │    │  ┌────────┐  │  │
│  │  │ sidecar-local  │  │    │  │ sidecar-local  │  │    │  │sidecar │  │  │
│  │  │ ┌────────────┐ │  │    │  │ ┌────────────┐ │  │    │  │-local  │  │  │
│  │  │ │ Receiver   │ │  │    │  │ │ Receiver   │ │  │    │  │────────│  │  │
│  │  │ │ (/notify)  │ │  │    │  │ │ (/notify)  │ │  │    │  │Receiver │  │  │
│  │  │ └──────┬─────┘ │  │    │  │ └──────┬─────┘ │  │    │  │────────│  │  │
│  │  │ ┌──────┴─────┐ │  │    │  │ ┌──────┴─────┐ │  │    │  │Local   │  │  │
│  │  │ │ Local      │ │  │    │  │ │ Local      │ │  │    │  │Watcher │  │  │
│  │  │ │ Watcher    │ │  │    │  │ │ Watcher    │ │  │    │  │────────│  │  │
│  │  │ │ (poll 30s) │ │  │    │  │ │ (poll 30s) │ │  │    │  │Orphan  │  │  │
│  │  │ └──────┬─────┘ │  │    │  │ └──────┬─────┘ │  │    │  │Cleaner │  │  │
│  │  │ ┌──────┴─────┐ │  │    │  │ ┌──────┴─────┐ │  │    │  └────────┘  │  │
│  │  │ │ Generator  │ │  │    │  │ │ Generator  │ │  │    │              │  │
│  │  │ │ (YAML)     │ │  │    │  │ │ (YAML)     │ │  │    │              │  │
│  │  │ └──────┬─────┘ │  │    │  │ └──────┬─────┘ │  │    │              │  │
│  │  │        │       │  │    │  │        │       │  │    │              │  │
│  │  │        ▼       │  │    │  │        ▼       │  │    │              │  │
│  │  │ ┌────────────┐ │  │    │  │ ┌────────────┐ │  │    │              │  │
│  │  │ │ local/     │ │  │    │  │ │ local/     │ │  │    │              │  │
│  │  │ │ routers.yml│ │  │    │  │ │ routers.yml│ │  │    │              │  │
│  │  │ │ services.  │ │  │    │  │ │ services.  │ │  │    │              │  │
│  │  │ │ yml        │ │  │    │  │ │ yml        │ │  │    │              │  │
│  │  │ └────────────┘ │  │    │  │ └────────────┘ │  │    │              │  │
│  │  └────────────────┘  │    │  └────────────────┘  │    │              │  │
│  │                      │    │                      │    │              │  │
│  │  ┌──────────────┐    │    │  ┌──────────────┐    │    │  ┌──────────┐ │  │
│  │  │ Syncthing    │    │    │  │ Syncthing    │    │    │  │Syncthing │ │  │
│  │  │ (shared/     │<───┼────┼──│ (shared/     │<───┼────┼──│ (shared/ │ │  │
│  │  │  sync)       │    │    │  │  sync)       │    │    │  │  sync)   │ │  │
│  │  └──────────────┘    │    │  └──────────────┘    │    │  └──────────┘ │  │
│  └──────────────────────┘    └──────────────────────┘    └──────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 Fluxo de Sincronização de Dados

```
                    ┌──────────────────┐
                    │   sidecar-global │ (Manager Node - 1 réplica)
                    │   (Hub Central)  │
                    │                  │
                    │  Gera:           │
                    │  shared/fed....  │─────┐
                    │  shared/mid....  │     │
                    └──────────────────┘     │
                                             │
                    ┌──────────────────┐     │  Syncthing (P2P sync)
                    │   sync_data      │<────┘
                    │   volume compart.│<────┼────────────────────┐
                    └──────────────────┘     │                    │
                                             │                    │
                    ┌──────────────────┐     │  ┌──────────────┐  │
                    │  sidecar-local   │     │  │ sidecar-local│  │
                    │  (Node 1)        │     │  │ (Node 2)     │  │
                    │                  │     │  │              │  │
                    │  Lê: shared/     │     │  │ Lê: shared/  │  │
                    │  Gera: local/    │     │  │ Gera: local/ │  │
                    └──────────────────┘     │  └──────────────┘  │
                                             │                    │
                    ┌──────────────────┐     │                    │
                    │  Traefik (Node 1)│     │                    │
                    │                  │     │                    │
                    │  Lê:            │     │                    │
                    │  shared/*.yaml  │<────┘                    │
                    │  local/*.yaml   │                          │
                    └──────────────────┘                         │
                                                                 │
                    ┌──────────────────┐                         │
                    │  Traefik (Node 2)│                         │
                    │  Lê:            │                         │
                    │  shared/*.yaml  │<─────────────────────────┘
                    │  local/*.yaml   │
                    └──────────────────┘
```

---

## 2. Estrutura de Diretórios

```
/workspace/
│
├── cmd/                          # Pontos de entrada da aplicação
│   ├── hub/                      #   → sidecar-global (Hub Central)
│   │   └── main.go               #     Manager node, 1 réplica
│   │
│   └── agent/                    #   → sidecar-local (Agente Local)
│       └── main.go               #     Global deploy, cada nó
│
├── internal/                     # Lógica interna (não exportada)
│   │
│   ├── hub/                      # Lógica do Hub Central (manager)
│   │   ├── watcher.go            #   Docker Events + service list poller
│   │   ├── notifier.go           #   HTTP push para Agentes
│   │   ├── state_manager.go      #   Gerencia estado compartilhado
│   │   └── orphan_cleaner.go     #   Limpa configs órfãs globais
│   │
│   ├── agent/                    # Lógica do Agente Local (cada nó)
│   │   ├── receiver.go           #   HTTP server (/notify endpoint)
│   │   ├── local_watcher.go      #   Polling Docker socket local
│   │   └── orphan_cleaner.go     #   Limpa configs locais órfãs
│   │
│   ├── discovery/                # Resolução de IPs (rede bridge)
│   │   ├── node.go               #   Descoberta de IPs dos nós
│   │   └── container.go          #   Descoberta de IPs de containers
│   │
│   ├── config/                   # Geração de YAML e diff
│   │   ├── generator.go          #   Geração de configs Traefik
│   │   ├── diff.go               #   Comparação de estados
│   │   └── state.go              #   Representação de estado interno
│   │
│   ├── writer/                   # Escrita atômica de arquivos
│   │   └── writer.go             #   tempfile + rename pattern
│   │
│   ├── api/                      # Comunicação Hub-Agente
│   │   ├── server.go             #   Servidor HTTP do Agente
│   │   └── client.go             #   Cliente HTTP do Hub
│   │
│   └── events/                   # Detecção de mudanças
│       ├── watcher.go            #   Docker Events listener
│       └── poller.go             #   Polling periódico Swarm API
│
├── pkg/                          # Pacotes exportados (API pública)
│   └── models/                   # Modelos de dados compartilhados
│       ├── service.go            #   ServiceMeta, ServiceState
│       ├── node.go               #   NodeInfo, FederationTarget
│       └── types.go              #   Tipos enumerados, constantes
│
├── test/                         # Testes
│   ├── integration/              # Testes de integração
│   │   └── swarm_test.go         #   Testcontainers + Swarm mock
│   ├── fixtures/                 # Golden files para snapshot testing
│   │   ├── federation.yaml
│   │   ├── middlewares.yaml
│   │   ├── local_services.yaml
│   │   └── local_routers.yaml
│   └── mocks/                    # Mocks gerados (ou manuais)
│       ├── event_listener.go
│       ├── config_writer.go
│       └── node_discovery.go
│
├── Dockerfile.hub                # Dockerfile do Hub Central
├── Dockerfile.agent              # Dockerfile do Agente Local
├── docker-compose.yml            # Stack Swarm completa
├── go.mod
├── go.sum
├── ARCHITECTURE.md               # Este documento
├── README.md
└── plans/                        # Planos de implementação
    └── implementation.md
```

---

## 3. Modelos de Dados (pkg/models)

### 3.1 `types.go` — Enums e Constantes

```go
package models

import "time"

// Action representa o tipo de mudança detectada.
type Action string

const (
    ActionCreate Action = "CREATE"
    ActionUpdate Action = "UPDATE"
    ActionDelete Action = "DELETE"
)

// NodeRole representa o papel do nó no Swarm.
type NodeRole string

const (
    NodeRoleManager NodeRole = "manager"
    NodeRoleWorker  NodeRole = "worker"
)

// ServiceProtocol representa o protocolo do serviço.
type ServiceProtocol string

const (
    ProtocolHTTP  ServiceProtocol = "http"
    ProtocolHTTPS ServiceProtocol = "https"
    ProtocolTCP   ServiceProtocol = "tcp"
    ProtocolUDP   ServiceProtocol = "udp"
)

// Default constants
const (
    DefaultPollInterval     = 10 * time.Second
    DefaultHubPollInterval  = 5 * time.Second
    DefaultAgentPollInterval = 30 * time.Second
    DefaultNotifyPort       = 9090
    DefaultMaxRetries       = 3
    DefaultBackoffBase      = 1 * time.Second
    DefaultBackoffMax       = 30 * time.Second
    DefaultHeartbeatTTL     = 60 * time.Second
)
```

### 3.2 `service.go` — Metadados e Estado de Serviços

```go
package models

// ServiceMeta contém metadados extraídos das labels do serviço no Swarm.
type ServiceMeta struct {
    // Name é o nome do serviço Swarm.
    Name string `yaml:"-" json:"name"`

    // Host é o domínio para roteamento (label: traefik.federation.host).
    Host string `yaml:"host,omitempty" json:"host,omitempty"`

    // Port é a porta do container para onde o tráfego será roteado.
    Port int `yaml:"port,omitempty" json:"port,omitempty"`

    // TLS indica se o serviço requer TLS.
    TLS bool `yaml:"tls,omitempty" json:"tls,omitempty"`

    // Entrypoints lista os entrypoints do Traefik a serem usados.
    Entrypoints []string `yaml:"entrypoints,omitempty" json:"entrypoints,omitempty"`

    // Middlewares lista os middlewares a serem aplicados.
    Middlewares []string `yaml:"middlewares,omitempty" json:"middlewares,omitempty"`

    // Enabled indica se o serviço está habilitado para federação.
    Enabled bool `yaml:"-" json:"enabled"`

    // Protocol define o protocolo do serviço (http, https, tcp, udp).
    Protocol ServiceProtocol `yaml:"protocol,omitempty" json:"protocol,omitempty"`

    // StickySession habilita sessões sticky via cookies.
    StickySession bool `yaml:"stickySession,omitempty" json:"stickySession,omitempty"`

    // Weight é o peso do serviço para load balancing.
    Weight int `yaml:"weight,omitempty" json:"weight,omitempty"`
}

// ServiceState representa o estado completo de um serviço no cluster.
type ServiceState struct {
    // ServiceMeta incorpora os metadados do serviço.
    ServiceMeta `yaml:",inline" json:",inline"`

    // TaskID é o ID da task Swarm.
    TaskID string `yaml:"taskID,omitempty" json:"taskID,omitempty"`

    // ContainerID é o ID do container Docker.
    ContainerID string `yaml:"containerID,omitempty" json:"containerID,omitempty"`

    // NodeID é o ID do nó onde a task está rodando.
    NodeID string `yaml:"nodeID,omitempty" json:"nodeID,omitempty"`

    // BridgeIP é o IP do container na rede bridge local.
    BridgeIP string `yaml:"bridgeIP,omitempty" json:"bridgeIP,omitempty"`

    // NodeAddr é o IP LAN do nó onde a task está rodando.
    NodeAddr string `yaml:"nodeAddr,omitempty" json:"nodeAddr,omitempty"`

    // IsLocal indica se a task está rodando no nó local.
    IsLocal bool `yaml:"-" json:"isLocal"`
}

// ServiceEvent representa um evento de mudança em um serviço.
type ServiceEvent struct {
    Action    Action    `json:"action"`
    Service   string    `json:"service"`
    Timestamp time.Time `json:"timestamp"`
    Details   *ServiceState `json:"details,omitempty"`
}
```

### 3.3 `node.go` — Informações de Nós e Federação

```go
package models

import "net"

// NodeInfo contém informações de um nó do Swarm.
type NodeInfo struct {
    // ID é o ID do nó no Swarm.
    ID string `json:"id" yaml:"id"`

    // Hostname é o hostname do nó.
    Hostname string `json:"hostname" yaml:"hostname"`

    // Addr é o endereço IP LAN do nó.
    Addr net.IP `json:"addr" yaml:"addr"`

    // Labels são as labels Docker do nó.
    Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

    // Role indica se é manager ou worker.
    Role NodeRole `json:"role" yaml:"role"`

    // OS indica o sistema operacional do nó.
    OS string `json:"os,omitempty" yaml:"os,omitempty"`
}

// LocalTaskInfo representa uma task rodando LOCALMENTE no nó.
type LocalTaskInfo struct {
    TaskID      string `json:"taskID" yaml:"taskID"`
    ServiceName string `json:"serviceName" yaml:"serviceName"`
    ContainerID string `json:"containerID" yaml:"containerID"`
    BridgeIP    string `json:"bridgeIP" yaml:"bridgeIP"`
    NodeID      string `json:"nodeID" yaml:"nodeID"`
    NodeHostname string `json:"nodeHostname,omitempty" yaml:"nodeHostname,omitempty"`
}

// FederationTarget representa um destino de federação (serviço em outro nó).
type FederationTarget struct {
    // NodeIP é o IP LAN do nó alvo.
    NodeIP net.IP `json:"nodeIP" yaml:"nodeIP"`

    // NodeID é o ID do nó alvo no Swarm.
    NodeID string `json:"nodeID" yaml:"nodeID"`

    // NodeHostname é o hostname do nó alvo.
    NodeHostname string `json:"nodeHostname,omitempty" yaml:"nodeHostname,omitempty"`

    // ServiceName é o nome do serviço no nó alvo.
    ServiceName string `json:"serviceName" yaml:"serviceName"`

    // Port é a porta publicada do serviço no nó alvo.
    Port int `json:"port,omitempty" yaml:"port,omitempty"`
}

// NotificationPayload é o payload enviado do Hub para o Agente via HTTP.
type NotificationPayload struct {
    Action      Action    `json:"action"`
    ServiceName string    `json:"serviceName"`
    Timestamp   time.Time `json:"timestamp"`
    NodeID      string    `json:"nodeID,omitempty"`
}

// AgentState representa o estado de um Agente registrado no Hub.
type AgentState struct {
    NodeID      string    `json:"nodeID"`
    NodeAddr    net.IP    `json:"nodeAddr"`
    Hostname    string    `json:"hostname"`
    LastSeen    time.Time `json:"lastSeen"`
    Online      bool      `json:"online"`
    OS          string    `json:"os,omitempty"`
}
```

### 3.4 `types.go` — Config Traefik e Diff

```go
package models

import "time"

// TraefikConfig representa a estrutura completa de configuração YAML do Traefik.
type TraefikConfig struct {
    HTTP *HTTPConfig `yaml:"http,omitempty" json:"http,omitempty"`
}

// HTTPConfig contém roteadores, serviços e middlewares HTTP.
type HTTPConfig struct {
    Routers     map[string]*RouterConfig     `yaml:"routers,omitempty" json:"routers,omitempty"`
    Services    map[string]*ServiceConfig    `yaml:"services,omitempty" json:"services,omitempty"`
    Middlewares map[string]*MiddlewareConfig `yaml:"middlewares,omitempty" json:"middlewares,omitempty"`
}

// RouterConfig representa um router HTTP do Traefik.
type RouterConfig struct {
    Rule        string   `yaml:"rule" json:"rule"`
    Service     string   `yaml:"service" json:"service"`
    Entrypoints []string `yaml:"entrypoints" json:"entrypoints"`
    Middlewares []string `yaml:"middlewares,omitempty" json:"middlewares,omitempty"`
    TLS         *TLSConfig `yaml:"tls,omitempty" json:"tls,omitempty"`
    Priority    int      `yaml:"priority,omitempty" json:"priority,omitempty"`
}

// ServiceConfig representa um serviço HTTP do Traefik.
type ServiceConfig struct {
    LoadBalancer *LoadBalancerConfig `yaml:"loadBalancer,omitempty" json:"loadBalancer,omitempty"`
}

// LoadBalancerConfig configura o load balancer do serviço.
type LoadBalancerConfig struct {
    Servers        []ServerConfig        `yaml:"servers" json:"servers"`
    Sticky         *StickyConfig         `yaml:"sticky,omitempty" json:"sticky,omitempty"`
    HealthCheck    *HealthCheckConfig    `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`
    PassHostHeader *bool                 `yaml:"passHostHeader,omitempty" json:"passHostHeader,omitempty"`
}

// ServerConfig representa um servidor backend.
type ServerConfig struct {
    URL string `yaml:"url" json:"url"`
}

// StickyConfig configura sessões sticky.
type StickyConfig struct {
    Cookie *CookieConfig `yaml:"cookie,omitempty" json:"cookie,omitempty"`
}

// CookieConfig configura o cookie de sessão sticky.
type CookieConfig struct {
    Name     string `yaml:"name,omitempty" json:"name,omitempty"`
    Secure   bool   `yaml:"secure,omitempty" json:"secure,omitempty"`
    HTTPOnly bool   `yaml:"httpOnly,omitempty" json:"httpOnly,omitempty"`
}

// HealthCheckConfig configura health check do backend.
type HealthCheckConfig struct {
    Path     string `yaml:"path,omitempty" json:"path,omitempty"`
    Port     int    `yaml:"port,omitempty" json:"port,omitempty"`
    Interval string `yaml:"interval,omitempty" json:"interval,omitempty"`
    Timeout  string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// MiddlewareConfig representa um middleware do Traefik.
type MiddlewareConfig struct {
    Headers        *HeadersConfig        `yaml:"headers,omitempty" json:"headers,omitempty"`
    RateLimit      *RateLimitConfig      `yaml:"rateLimit,omitempty" json:"rateLimit,omitempty"`
    Retry          *RetryConfig          `yaml:"retry,omitempty" json:"retry,omitempty"`
    CircuitBreaker *CircuitBreakerConfig `yaml:"circuitBreaker,omitempty" json:"circuitBreaker,omitempty"`
    BasicAuth      *BasicAuthConfig      `yaml:"basicAuth,omitempty" json:"basicAuth,omitempty"`
}

// TLSConfig configura TLS para um router.
type TLSConfig struct {
    CertResolver string `yaml:"certResolver,omitempty" json:"certResolver,omitempty"`
    Domains      []DomainConfig `yaml:"domains,omitempty" json:"domains,omitempty"`
}

// DomainConfig representa um domínio para certificado TLS.
type DomainConfig struct {
    Main      string   `yaml:"main" json:"main"`
    SANs      []string `yaml:"sans,omitempty" json:"sans,omitempty"`
}

// HeadersConfig configura headers HTTP.
type HeadersConfig struct {
    AccessControlAllowMethods     []string `yaml:"accessControlAllowMethods,omitempty" json:"accessControlAllowMethods,omitempty"`
    AccessControlAllowOrigins     []string `yaml:"accessControlAllowOrigins,omitempty" json:"accessControlAllowOrigins,omitempty"`
    AccessControlAllowHeaders     []string `yaml:"accessControlAllowHeaders,omitempty" json:"accessControlAllowHeaders,omitempty"`
    AddVaryHeader                 *bool    `yaml:"addVaryHeader,omitempty" json:"addVaryHeader,omitempty"`
    CustomRequestHeaders          map[string]string `yaml:"customRequestHeaders,omitempty" json:"customRequestHeaders,omitempty"`
    CustomResponseHeaders         map[string]string `yaml:"customResponseHeaders,omitempty" json:"customResponseHeaders,omitempty"`
}

// RateLimitConfig configura rate limiting.
type RateLimitConfig struct {
    Average     int            `yaml:"average,omitempty" json:"average,omitempty"`
    Burst       int            `yaml:"burst,omitempty" json:"burst,omitempty"`
    Period      string         `yaml:"period,omitempty" json:"period,omitempty"`
    SourceCriterion *SourceCriterion `yaml:"sourceCriterion,omitempty" json:"sourceCriterion,omitempty"`
}

// SourceCriterion define o critério de origem para rate limit.
type SourceCriterion struct {
    IPStrategy *IPStrategy `yaml:"ipStrategy,omitempty" json:"ipStrategy,omitempty"`
}

// IPStrategy define estratégia de extração de IP.
type IPStrategy struct {
    Depth int `yaml:"depth,omitempty" json:"depth,omitempty"`
}

// RetryConfig configura retry.
type RetryConfig struct {
    Attempts int `yaml:"attempts,omitempty" json:"attempts,omitempty"`
}

// CircuitBreakerConfig configura circuit breaker.
type CircuitBreakerConfig struct {
    Expression string `yaml:"expression,omitempty" json:"expression,omitempty"`
}

// BasicAuthConfig configura autenticação básica.
type BasicAuthConfig struct {
    Users     []string `yaml:"users,omitempty" json:"users,omitempty"`
    Realm     string   `yaml:"realm,omitempty" json:"realm,omitempty"`
    RemoveHeader bool  `yaml:"removeHeader,omitempty" json:"removeHeader,omitempty"`
}

// DiffResult representa o resultado da comparação de estados.
type DiffResult struct {
    // HasChanges indica se houve alguma mudança.
    HasChanges bool `json:"hasChanges"`

    // Added são as chaves adicionadas.
    Added []string `json:"added,omitempty"`

    // Removed são as chaves removidas.
    Removed []string `json:"removed,omitempty"`

    // Modified são as chaves modificadas.
    Modified []string `json:"modified,omitempty"`

    // Snapshot anterior e novo (uso interno).
    OldState *ClusterState `json:"-"`
    NewState *ClusterState `json:"-"`
}

// ClusterState representa o estado completo do cluster em um ponto no tempo.
type ClusterState struct {
    Services    []ServiceState           `yaml:"services" json:"services"`
    Nodes       []NodeInfo               `yaml:"nodes" json:"nodes"`
    GeneratedAt time.Time                `yaml:"generatedAt" json:"generatedAt"`
    Federation  []FederationTarget       `yaml:"federation,omitempty" json:"federation,omitempty"`
}
```

---

## 4. Interfaces e Contratos (ISP/DIP)

Todas as interfaces seguem o **Princípio da Segregação de Interfaces (ISP)** e o **Princípio da Inversão de Dependência (DIP)**, garantindo que implementações concretas possam ser trocadas sem impacto nos consumidores.

### 4.1 `ServiceEventListener` — Detecção de Mudanças

```go
package events

// ServiceEventListener notifica sobre mudanças em serviços Swarm.
// Implementações: DockerEventsWatcher (real-time), Poller (fallback).
type ServiceEventListener interface {
    // Events retorna um canal de eventos de serviço.
    // O caller deve consumir o canal para evitar bloqueios.
    Events(ctx context.Context) (<-chan models.ServiceEvent, error)

    // Start inicia a escuta de eventos (bloqueante até ctx cancel).
    Start(ctx context.Context) error

    // Close libera recursos associados ao listener.
    Close() error
}
```

### 4.2 `ConfigWriter` — Escrita Atômica de Arquivos

```go
package writer

// ConfigWriter escreve configurações YAML de forma atômica.
// Usa o padrão: escrever em tempfile → fsync → rename.
type ConfigWriter interface {
    // WriteConfig escreve o conteúdo em 'path' de forma atômica.
    // Garante que o arquivo destino só será atualizado após escrita completa.
    WriteConfig(path string, data []byte) error

    // RemoveConfig remove um arquivo de configuração.
    RemoveConfig(path string) error

    // WriteConfigWithMode escreve config com permissões específicas.
    WriteConfigWithMode(path string, data []byte, perm os.FileMode) error
}
```

### 4.3 `NodeDiscovery` — Descoberta de Nós

```go
package discovery

// NodeDiscovery resolve endereços IP de nós do Swarm.
type NodeDiscovery interface {
    // GetNodeAddr retorna o endereço IP LAN de um nó pelo ID.
    GetNodeAddr(ctx context.Context, nodeID string) (net.IP, error)

    // ListNodeHostnames retorna todos os nós com seus hostnames e IPs.
    ListNodes(ctx context.Context) ([]models.NodeInfo, error)

    // GetCurrentNode retorna informações do nó local.
    GetCurrentNode(ctx context.Context) (*models.NodeInfo, error)

    // ResolveNodeAddrByHostname resolve IP pelo hostname do nó.
    ResolveNodeAddrByHostname(ctx context.Context, hostname string) (net.IP, error)
}
```

### 4.4 `ContainerDiscovery` — Descoberta de Containers

```go
package discovery

// ContainerDiscovery resolve endereços IP de containers na rede bridge local.
type ContainerDiscovery interface {
    // GetBridgeIP retorna o IP do container na rede bridge local.
    // Retorna erro se o container não estiver rodando localmente.
    GetBridgeIP(ctx context.Context, containerID string) (net.IP, error)

    // ListLocalTasks lista todas as tasks Swarm rodando no nó local.
    ListLocalTasks(ctx context.Context) ([]models.LocalTaskInfo, error)

    // GetContainerByService retorna o container local de um serviço.
    GetContainerByService(ctx context.Context, serviceName string) (*models.LocalTaskInfo, error)
}
```

### 4.5 `AgentNotifier` — Notificação Push

```go
package hub

// AgentNotifier envia notificações push do Hub para os Agentes.
type AgentNotifier interface {
    // NotifyAgent envia notificação para um agente específico.
    NotifyAgent(ctx context.Context, agentID string, payload models.NotificationPayload) error

    // NotifyAll envia notificação para todos os agentes registrados.
    NotifyAll(ctx context.Context, payload models.NotificationPayload) error

    // RegisterAgent adiciona um agente à lista de notificação.
    RegisterAgent(nodeID string, addr net.IP, hostname string)

    // UnregisterAgent remove um agente da lista.
    UnregisterAgent(nodeID string)

    // GetAgentState retorna o estado atual de um agente.
    GetAgentState(nodeID string) (*models.AgentState, bool)

    // ListAgents lista todos os agentes registrados.
    ListAgents() []models.AgentState
}
```

### 4.6 `OrphanCleaner` — Limpeza de Órfãos

```go
package hub  // ou agent

// OrphanCleaner remove configurações de serviços que não existem mais.
type OrphanCleaner interface {
    // CleanOrphans remove arquivos de config de serviços não existentes.
    // 'activeServices' é o conjunto de serviços que deveriam ter config.
    CleanOrphans(ctx context.Context, activeServices map[string]bool) error

    // DryRun retorna quais arquivos seriam removidos sem removê-los.
    DryRun(ctx context.Context, activeServices map[string]bool) ([]string, error)

    // ScheduleClean agenda limpeza periódica (goroutine interna).
    ScheduleClean(ctx context.Context, interval time.Duration) error
}
```

### 4.7 `StateStore` — Armazenamento de Estado

```go
package config

// StateStore gerencia o armazenamento e recuperação de estado.
type StateStore interface {
    // SaveClusterState persiste o estado atual do cluster.
    SaveClusterState(ctx context.Context, state *models.ClusterState) error

    // LoadClusterState carrega o último estado persistido.
    LoadClusterState(ctx context.Context) (*models.ClusterState, error)

    // SaveAgentState persiste o estado de um agente.
    SaveAgentState(ctx context.Context, agentID string, state *models.AgentState) error

    // LoadAgentStates carrega estados de todos os agentes.
    LoadAgentStates(ctx context.Context) (map[string]models.AgentState, error)

    // Clear remove todos os dados de estado.
    Clear(ctx context.Context) error
}
```

### 4.8 `ConfigGenerator` — Geração de YAML

```go
package config

// ConfigGenerator gera configurações YAML para o Traefik.
type ConfigGenerator interface {
    // GenerateFederationConfig gera shared/federation.yaml
    // Contém serviços de federação apontando para IPs dos nós remotos.
    GenerateFederationConfig(targets []models.FederationTarget) (*models.TraefikConfig, error)

    // GenerateMiddlewareConfig gera shared/middlewares.yaml
    GenerateMiddlewareConfig(middlewares map[string]*models.MiddlewareConfig) (*models.TraefikConfig, error)

    // GenerateLocalRouterConfig gera local/routers.yaml para o nó local.
    // Cria routers locais OU routers de federação (se serviço não está local).
    GenerateLocalRouterConfig(state *models.ClusterState, localNodeID string) (*models.TraefikConfig, error)

    // GenerateLocalServiceConfig gera local/services.yaml para o nó local.
    // Contém serviços apontando para bridge IPs de containers locais.
    GenerateLocalServiceConfig(localTasks []models.LocalTaskInfo) (*models.TraefikConfig, error)
}
```

### 4.9 `DiffEngine` — Comparação de Estados

```go
package config

// DiffEngine compara dois estados e determina as diferenças.
type DiffEngine interface {
    // Diff compara dois estados do cluster e retorna as diferenças.
    Diff(old, new *models.ClusterState) (*models.DiffResult, error)

    // HasChangesAt compara estados específicos de um arquivo YAML.
    // Ex: federation, middlewares, routers, services.
    HasChangesAt(old, new *models.ClusterState, target string) (bool, error)

    // Snapshot tira um snapshot do estado para comparação futura.
    Snapshot(state *models.ClusterState) (*models.ClusterState, error)
}
```

### 4.10 `StateManager` — Orquestrador Central

```go
package hub

// StateManager orquestra o pipeline: evento → diff → escrita → notificação.
type StateManager interface {
    // HandleEvent processa um evento de serviço.
    HandleEvent(ctx context.Context, event models.ServiceEvent) error

    // SyncFullState dispara uma sincronização completa do estado.
    SyncFullState(ctx context.Context) error

    // GetCurrentState retorna o estado atual do cluster.
    GetCurrentState() *models.ClusterState

    // RegisterAgentCallback registra callback para notificar agente.
    RegisterAgentCallback(fn func(agentID string) error)
}
```

---

## 5. Especificação dos Pacotes

### 5.1 `cmd/hub/main.go` — Ponto de Entrada do Hub

**Responsabilidade:** Inicializar e executar o Hub Central (sidecar-global).

**Fluxo de inicialização:**
1. Parse de flags/ambiente (`MODE=global`)
2. Inicializar Docker client
3. Inicializar `discovery.NodeDiscovery` (Docker SDK)
4. Inicializar `events.Watcher` (Docker Events + Poller)
5. Inicializar `config.StateStore` (arquivo JSON em disco)
6. Inicializar `config.DiffEngine`
7. Inicializar `config.ConfigGenerator`
8. Inicializar `hub.Notifier` (HTTP client pool)
9. Inicializar `hub.StateManager`
10. Inicializar `hub.OrphanCleaner`
11. Iniciar HTTP server para health check e debug
12. Aguardar sinal de término (SIGTERM/SIGINT)

### 5.2 `cmd/agent/main.go` — Ponto de Entrada do Agente

**Responsabilidade:** Inicializar e executar o Agente Local (sidecar-local).

**Fluxo de inicialização:**
1. Parse de flags/ambiente (`MODE=local`)
2. Inicializar Docker client local
3. Inicializar `discovery.ContainerDiscovery`
4. Inicializar `discovery.NodeDiscovery` (para identificar nó local)
5. Inicializar `events.Poller` (polling local Docker)
6. Inicializar `config.ConfigGenerator`
7. Inicializar `api.Server` (HTTP server em `:9090`)
8. Inicializar `agent.Receiver` (manipula `/notify`)
9. Inicializar `agent.LocalWatcher` (polling periódico)
10. Inicializar `agent.OrphanCleaner`
11. Aguardar sinal de término

### 5.3 `internal/hub/watcher.go` — Docker Events Watcher

**Responsabilidade:** Escutar eventos do Docker Swarm relacionados a serviços.

**Funcionalidades:**
- Conecta ao Docker socket via `docker events --filter type=service`
- Usa `events.Watcher` para eventos em tempo real
- Usa `events.Poller` como fallback a cada 5-10s
- Filtra eventos relevantes (create, update, remove)
- Extrai metadados das labels (`traefik.federation.*`)
- Publica eventos no canal do `ServiceEventListener`

### 5.4 `internal/hub/notifier.go` — Notificador Push

**Responsabilidade:** Enviar notificações HTTP para Agentes.

**Funcionalidades:**
- Mantém registro de Agentes (NodeID → IP:Port)
- Worker pool para envio concorrente (max 5 workers)
- Retry com backoff exponencial (1s, 2s, 4s, 8s, max 30s)
- Marca agente como offline após falhas consecutivas
- HTTP POST com `NotificationPayload` JSON
- Timeout de 5s por requisição

### 5.5 `internal/hub/state_manager.go` — Gerenciador de Estado

**Responsabilidade:** Orquestrar o pipeline completo de detecção a notificação.

**Funcionalidades:**
- Recebe eventos do `ServiceEventListener`
- Consulta Swarm API para estado atualizado
- Gera `ClusterState` via `ConfigGenerator`
- Compara com estado anterior via `DiffEngine`
- Se houver mudanças: escreve YAMLs atômicos, notifica Agentes
- Mantém mutex para acesso seguro ao estado

### 5.6 `internal/hub/orphan_cleaner.go` — Limpeza de Órfãos (Hub)

**Responsabilidade:** Remover configurações globais de serviços que não existem mais no Swarm.

**Funcionalidades:**
- Varre diretório `shared/generated/` periodicamente
- Compara com lista atual de serviços Swarm
- Remove arquivos YAML de serviços extintos
- Suporta dry-run para auditoria

### 5.7 `internal/agent/receiver.go` — Receptor de Notificações

**Responsabilidade:** Processar notificações push do Hub.

**Funcionalidades:**
- Endpoint HTTP `POST /notify`
- Recebe `NotificationPayload` e dispara reconfiguração
- Aciona `LocalWatcher` para polling imediato (fora do ciclo normal)
- Responde com `202 Accepted` imediatamente
- Loga payload recebido para auditoria

### 5.8 `internal/agent/local_watcher.go` — Watcher Local

**Responsabilidade:** Monitorar serviços locais e gerar configurações.

**Funcionalidades:**
- Polling a cada 30s no Docker socket local
- Lista containers locais via Docker API
- Para cada serviço habilitado:
  - Se container local → router + service com bridge IP
  - Se não local → router apontando para federação
- Gera `local/routers.yaml` e `local/services.yaml`
- Usa `writer.ConfigWriter` para escrita atômica

### 5.9 `internal/agent/orphan_cleaner.go` — Limpeza de Órfãos (Agente)

**Responsabilidade:** Remover configurações locais de serviços que não têm container rodando no nó.

**Funcionalidades:**
- Varre `local/generated/` periodicamente
- Compara com containers rodando localmente
- Remove YAMLs de serviços sem task local
- Previne acúmulo de configs obsoletas

### 5.10 `internal/discovery/node.go` — Descoberta de Nós

**Responsabilidade:** Resolver IPs LAN dos nós do Swarm.

**Implementação:**
- Usa Docker SDK → `docker node inspect`
- Extrai `Status.Addr` (IP LAN) de cada nó
- Cache com TTL de 60s para reduzir chamadas
- Fallback: resolução DNS do hostname do nó

### 5.11 `internal/discovery/container.go` — Descoberta de Containers

**Responsabilidade:** Resolver IPs de containers na rede bridge local.

**Implementação:**
- Usa Docker SDK → `docker inspect <container>`
- Extrai `NetworkSettings.Networks.bridge.IPAddress`
- Lista tasks Swarm locais via `TaskList` com filtro de nó
- Mapeia serviceName → containerID → bridgeIP

### 5.12 `internal/config/generator.go` — Gerador de Configs

**Responsabilidade:** Produzir structs `TraefikConfig` prontas para serialização YAML.

**Implementação:**
- `GenerateFederationConfig`: para cada `FederationTarget`, cria `ServiceConfig` com URL `http://<nodeIP>:<port>`
- `GenerateMiddlewareConfig`: transforma `map[string]*MiddlewareConfig` em `TraefikConfig`
- `GenerateLocalRouterConfig`: decide se router aponta para serviço local ou federado
- Gera nomes padronizados: `<serviceName>-local`, `<serviceName>-federation`, `<serviceName>-router`

### 5.13 `internal/config/diff.go` — Motor de Diff

**Responsabilidade:** Comparar dois estados de cluster e determinar mudanças.

**Implementação:**
- Comparação campo a campo entre `ClusterState` antigo e novo
- Identifica serviços adicionados, removidos e modificados
- Usa hash SHA256 do conteúdo serializado para detectar mudanças
- Suporta diff granular por tipo de config (federation, middleware, router, service)

### 5.14 `internal/config/state.go` — Estado Interno

**Responsabilidade:** Serialização/deserialização do estado do cluster.

**Funcionalidades:**
- `ClusterState` → JSON para persistência
- Métodos para merge incremental de estado
- Snapshot para rollback em caso de erro

### 5.15 `internal/writer/writer.go` — Escritor Atômico

**Responsabilidade:** Garantir escrita atômica de arquivos YAML.

**Implementação:**
```
1. Criar arquivo temporário no mesmo diretório (path.tmp)
2. Escrever conteúdo completo
3. Fsync() para garantir flush em disco
4. Rename() atômico (os.Rename) sobrescrevendo original
5. Se qualquer passo falhar → remove tempfile, retorna erro
```

### 5.16 `internal/api/server.go` — Servidor HTTP (Agente)

**Responsabilidade:** Servir endpoints HTTP do Agente Local.

**Endpoints:**
- `POST /notify` — Receber notificações do Hub
- `GET /health` — Health check
- `GET /services` — Listar serviços locais gerenciados
- `GET /debug/state` — Estado interno (debug)

### 5.17 `internal/api/client.go` — Cliente HTTP (Hub)

**Responsabilidade:** Enviar requisições HTTP para Agentes.

**Funcionalidades:**
- Pool de conexões HTTP reutilizáveis
- Timeout configurável
- Retry com backoff exponencial
- Métodos: `NotifyAgent`, `HealthCheck`

### 5.18 `internal/events/watcher.go` — Docker Events Watcher

**Responsabilidade:** Escutar eventos do Docker em tempo real.

**Implementação:**
- Usa `client.Events()` do Docker SDK
- Filtro: `{"Type": "service"}`
- Decodifica eventos e mapeia para `ServiceEvent`
- Reconecta automaticamente em caso de falha

### 5.19 `internal/events/poller.go` — Poller Periódico

**Responsabilidade:** Polling da API Swarm como fallback.

**Implementação:**
- Timer periódico (5-10s no Hub, 30s no Agente)
- Lista todos os serviços via `client.ServiceList()`
- Para cada serviço, extrai `ServiceMeta` das labels
- Compara com snapshot anterior e emite eventos de mudança

---

## 6. Fluxos Detalhados

### 6.1 Hub Central — Fluxo Completo

```
┌──────────┐   ┌───────────┐   ┌──────────────┐   ┌──────────┐   ┌───────────┐
│ Docker   │   │ Event     │   │ State        │   │ Diff     │   │ Notifier  │
│ Events   │   │ Listener  │   │ Manager      │   │ Engine   │   │           │
└────┬─────┘   └─────┬─────┘   └──────┬───────┘   └────┬─────┘   └─────┬─────┘
     │                │                │                │               │
     │ docker events  │                │                │               │
     │ type=service   │                │                │               │
     │───────────────>│                │                │               │
     │                │ ServiceEvent   │                │               │
     │                │───────────────>│                │               │
     │                │                │ Consultar      │               │
     │                │                │ Swarm API      │               │
     │                │                │─────┐          │               │
     │                │                │     │          │               │
     │                │                │<────┘          │               │
     │                │                │                │               │
     │                │                │ Gerar ClusterState            │
     │                │                │─────┐          │               │
     │                │                │     │          │               │
     │                │                │<────┘          │               │
     │                │                │                │               │
     │                │                │ Diff(old, new) │               │
     │                │                │───────────────>│               │
     │                │                │                │               │
     │                │                │   DiffResult   │               │
     │                │                │<───────────────│               │
     │                │                │                │               │
     │                │                │ Se HasChanges  │               │
     │                │                │─────┐          │               │
     │                │                │     │          │               │
     │                │                │<────┘          │               │
     │                │                │                │               │
     │                │                │ Escrever YAMLs │               │
     │                │                │ (escrita       │               │
     │                │                │  atômica)      │               │
     │                │                │─────┐          │               │
     │                │                │     │          │               │
     │                │                │<────┘          │               │
     │                │                │                │               │
     │                │                │ NotifyAll()    │               │
     │                │                │───────────────>│               │
     │                │                │                │               │
     │                │                │                │ HTTP POST     │
     │                │                │                │ /notify       │
     │                │                │                │───> Agentes   │
     │                │                │                │               │
```

### 6.2 Agente Local — Fluxo Completo

```
┌───────────┐   ┌──────────┐   ┌──────────────┐   ┌──────────┐   ┌──────────┐
│ Receiver  │   │ Local    │   │ Config       │   │ Writer   │   │ Traefik  │
│ (/notify) │   │ Watcher  │   │ Generator    │   │ (Atomic) │   │ (File    │
└─────┬─────┘   └────┬─────┘   └──────┬───────┘   └────┬─────┘   │ Provider)│
      │               │                │                │         └────┬─────┘
      │               │                │                │              │
      │ Notificação   │                │                │              │
      │──────────────>│                │                │              │
      │               │                │                │              │
      │               │ Polling 30s    │                │              │
      │               │ (ou imediato)  │                │              │
      │               │ Docker socket  │                │              │
      │               │─────┐          │                │              │
      │               │     │          │                │              │
      │               │<────┘          │                │              │
      │               │                │                │              │
      │               │ LocalTasks     │                │              │
      │               │───────────────>│                │              │
      │               │                │                │              │
      │               │                │ Gerar configs  │              │
      │               │                │─────┐          │              │
      │               │                │     │          │              │
      │               │                │<────┘          │              │
      │               │                │                │              │
      │               │                │ WriteConfig()  │              │
      │               │                │───────────────>│              │
      │               │                │                │              │
      │               │                │  Temp+Sync+    │              │
      │               │                │  Rename        │              │
      │               │                │─────┐          │              │
      │               │                │     │          │              │
      │               │                │<────┘          │              │
      │               │                │                │              │
      │               │                │                │ watch=true   │
      │               │                │                │ detecta      │
      │               │                │                │ mudança      │
      │               │                │                │──────────────>
      │               │                │                │              │
      │               │                │                │  Recarrega   │
      │               │                │                │  rotas       │
      │               │                │                │<──────────────│
```

### 6.3 Fluxo de Decisão: Router Local vs Federado

```
                    ┌─────────────────────┐
                    │  Serviço habilitado  │
                    │  tem task rodando    │
                    │  neste nó?           │
                    └──────┬──────┬───────┘
                           │      │
                        Sim│      │Não
                           │      │
                           ▼      ▼
              ┌──────────────────────┐
              │    LOCAL             │
              │                      │
              │  routers.yaml:       │
              │  rule: Host(`s.app`) │
              │  service: s-local    │
              │                      │
              │  services.yaml:      │
              │  s-local:            │
              │    url: http://      │
              │       10.0.0.2:8080  │
              │    (bridge IP)       │
              └──────────────────────┘
                                     │
                                     ▼
                    ┌──────────────────────┐
                    │    FEDERAÇÃO         │
                    │                      │
                    │  routers.yaml:       │
                    │  rule: Host(`s.app`) │
                    │  service: s-fed      │
                    │                      │
                    │  services.yaml:      │
                    │  s-fed: (JÁ existe   │
                    │   em federation.yaml │
                    │   gerado pelo Hub)   │
                    │                      │
                    │  federation.yaml     │
                    │  (syncthing):        │
                    │  s-fed:              │
                    │    url: http://      │
                    │     192.168.1.10:80  │
                    └──────────────────────┘
```

### 6.4 Fluxo de Heartbeat e Registro de Agentes

```
  ┌───── Agente ─────┐          ┌────── Hub ──────┐
  │                   │          │                  │
  │  Inicializa       │          │                  │
  │  HTTP server      │          │                  │
  │  :9090            │          │                  │
  │                   │          │                  │
  │  POST /register   │          │                  │
  │  {nodeID, addr,   │─────────>│  RegisterAgent() │
  │   hostname, os}   │          │                  │
  │                   │          │                  │
  │                   │          │  Adiciona à      │
  │                   │          │  lista de        │
  │                   │          │  notificação     │
  │                   │          │                  │
  │  ─ ─ ─ ─ ─ ─ ─ ─ ┼ ─ ─ ─ ─ >│                  │
  │  Heartbeat a cada │          │  Atualiza        │
  │  30s              │          │  LastSeen        │
  │                   │          │                  │
  │                   │          │  Se LastSeen >   │
  │                   │          │  60s → offline   │
  │                   │          │                  │
  │  ─ ─ ─ ─ ─ ─ ─ ─ ┼ ─ ─ ─ ─ >│                  │
  │  (offline)        │          │  Marca como      │
  │                   │          │  offline         │
  │                   │          │                  │
  │                   │          │  Não envia       │
  │                   │          │  notificações    │
  │                   │          │                  │
```

---

## 7. Arquivos YAML Gerados

### 7.1 `shared/federation.yaml` — Serviços de Federação

**Gerado por:** Hub Central (sidecar-global)
**Sincronizado via:** Syncthing para todos os nós
**Propósito:** Definir serviços Traefik que apontam para IPs de nós remotos.

```yaml
# shared/federation.yaml
# Gerado automaticamente pelo sidecar-global em: 2026-05-15T20:00:00Z
# Não edite manualmente — alterações serão sobrescritas.

http:
  services:
    nginx-federation:
      loadBalancer:
        servers:
          - url: "http://192.168.1.10:80"
        passHostHeader: true

    app-federation:
      loadBalancer:
        servers:
          - url: "http://192.168.1.20:3000"
        passHostHeader: true
        sticky:
          cookie:
            name: "_sticky_app"
            httpOnly: true

    whoami-federation:
      loadBalancer:
        servers:
          - url: "http://10.0.0.5:8080"
        passHostHeader: true
```

### 7.2 `shared/middlewares.yaml` — Middlewares Compartilhados

**Gerado por:** Hub Central (sidecar-global)
**Sincronizado via:** Syncthing para todos os nós
**Propósito:** Definir middlewares compartilhados por todo o cluster.

```yaml
# shared/middlewares.yaml
# Gerado automaticamente pelo sidecar-global em: 2026-05-15T20:00:00Z

http:
  middlewares:
    cors:
      headers:
        accessControlAllowMethods:
          - "GET"
          - "POST"
          - "PUT"
          - "DELETE"
          - "OPTIONS"
        accessControlAllowOrigins:
          - "*"
        accessControlAllowHeaders:
          - "Content-Type"
          - "Authorization"
        addVaryHeader: true

    rate-limit-global:
      rateLimit:
        average: 100
        burst: 50
        period: "1m"
        sourceCriterion:
          ipStrategy:
            depth: 1

    auth-basic:
      basicAuth:
        users:
          - "admin:$2y$10$..."
        realm: "Restricted"
```

### 7.3 `local/routers.yaml` — Roteadores Locais

**Gerado por:** Agente Local (sidecar-local) — específico por nó
**Propósito:** Definir regras de roteamento locais.

```yaml
# local/routers.yaml
# Gerado automaticamente pelo sidecar-local (node: worker-1)
# em: 2026-05-15T20:00:05Z

http:
  routers:
    nginx-router:
      rule: "Host(`nginx.app.local`)"
      service: "nginx-federation"    # Aponta para federação se não houver task local
      entrypoints:
        - "web"
      middlewares:
        - "cors@file"
        - "rate-limit-global@file"
      priority: 10

    app-router:
      rule: "Host(`app.internal`) && Headers(`X-Internal`, `true`)"
      service: "app-local"
      entrypoints:
        - "web"
      middlewares: []

    whoami-router:
      rule: "Host(`whoami.local`)"
      service: "whoami-federation"
      entrypoints:
        - "web"
      middlewares: []
      tls:
        certResolver: "letsencrypt"
```

### 7.4 `local/services.yaml` — Serviços Locais

**Gerado por:** Agente Local (sidecar-local) — específico por nó
**Propósito:** Definir serviços que apontam para containers rodando localmente.

```yaml
# local/services.yaml
# Gerado automaticamente pelo sidecar-local (node: worker-1)
# em: 2026-05-15T20:00:05Z

http:
  services:
    app-local:
      loadBalancer:
        servers:
          - url: "http://10.0.0.2:3000"  # Bridge IP do container local
        passHostHeader: true
        healthCheck:
          path: "/health"
          interval: "10s"
          timeout: "3s"
```

### 7.5 `shared/tls.yaml` — Configuração TLS (referenciada pelo Traefik)

**Gerenciado por:** Hub Central ou manual
**Propósito:** Configuração TLS global (já referenciada no `docker-compose.yml`).

```yaml
# shared/tls.yaml
tls:
  certificates:
    - certFile: /certs/cert.pem
      keyFile: /certs/key.pem
  options:
    default:
      minVersion: "VersionTLS12"
```

---

## 8. Padrões de Concorrência

### 8.1 State Mutex

```go
type StateManager struct {
    mu          sync.RWMutex
    currentState *models.ClusterState
    store       config.StateStore
    generator   config.ConfigGenerator
    diffEngine  config.DiffEngine
    notifier    AgentNotifier
}

func (sm *StateManager) GetCurrentState() *models.ClusterState {
    sm.mu.RLock()
    defer sm.mu.RUnlock()
    return sm.currentState
}
```

### 8.2 Pipeline com Channels

```go
// Pipeline: eventos → processamento → notificação
func (sm *StateManager) Start(ctx context.Context) error {
    eventCh := make(chan models.ServiceEvent, 100)
    diffCh := make(chan *models.DiffResult, 10)
    notifyCh := make(chan *models.NotificationPayload, 50)

    // Stage 1: Escuta eventos
    go sm.eventListener.Start(ctx, eventCh)

    // Stage 2: Processa eventos
    go sm.processEvents(ctx, eventCh, diffCh)

    // Stage 3: Escreve configs
    go sm.writeConfigs(ctx, diffCh, notifyCh)

    // Stage 4: Notifica agentes
    go sm.notifyAgents(ctx, notifyCh)

    return nil
}
```

### 8.3 Worker Pool para Notificações

```go
type Notifier struct {
    workers   int
    jobQueue  chan notifyJob
    agentList sync.Map  // map[string]*AgentState
}

func (n *Notifier) Start(ctx context.Context) {
    for i := 0; i < n.workers; i++ {
        go n.worker(ctx, i)
    }
}

func (n *Notifier) worker(ctx context.Context, id int) {
    for job := range n.jobQueue {
        err := n.sendWithBackoff(job.ctx, job.agentID, job.payload)
        if err != nil {
            log.Printf("[worker %d] falha ao notificar %s: %v", id, job.agentID, err)
        }
    }
}
```

### 8.4 Escrita Atômica

```go
type AtomicWriter struct{}

func (w *AtomicWriter) WriteConfig(path string, data []byte) error {
    dir := filepath.Dir(path)
    tmpFile := filepath.Join(dir, "."+filepath.Base(path)+".tmp")

    // Escreve em tempfile
    if err := os.WriteFile(tmpFile, data, 0644); err != nil {
        os.Remove(tmpFile)
        return fmt.Errorf("write temp: %w", err)
    }

    // Garante flush em disco
    f, _ := os.Open(tmpFile)
    if err := f.Sync(); err != nil {
        f.Close()
        os.Remove(tmpFile)
        return fmt.Errorf("fsync: %w", err)
    }
    f.Close()

    // Renomeia atomicamente
    if err := os.Rename(tmpFile, path); err != nil {
        os.Remove(tmpFile)
        return fmt.Errorf("rename: %w", err)
    }

    return nil
}
```

---

## 9. Tratamento de Erros e Resiliência

### 9.1 Matriz de Resiliência

| Cenário | Impacto | Ação | Recuperação |
|---------|---------|------|-------------|
| Hub offline | Agentes sem notificações push | Agente mantém última config válida + polling próprio | Reconexão automática quando Hub voltar |
| Agente offline | Notificação falha | Hub retry com backoff (1s, 2s, 4s, 8s, 16s, 30s max) | Agente faz polling ao reiniciar |
| Docker socket indisponível | Sem descoberta de containers | Retry com backoff, log warning | Tentativa a cada ciclo de polling |
| Syncthing atrasado | shared/ desatualizado | Agente usa última config conhecida | Próximo sync resolve |
| Escrita parcial (crash) | Arquivo corrompido | Atomic write garante que só arquivo completo substitui | Tempfile é limpo no próximo write |
| Rede particionada | Nó inalcançável | Hub marca agente offline, remove federação | Heartbeat re-registra quando volta |
| Serviço removido | Configs órfãs | OrphanCleaner remove periodicamente | N/A |

### 9.2 Backoff Exponencial

```go
package internal

import (
    "math"
    "time"
)

// BackoffStrategy define a estratégia de backoff.
type BackoffStrategy struct {
    Base   time.Duration
    Max    time.Duration
    Factor float64
}

var DefaultBackoff = BackoffStrategy{
    Base:   1 * time.Second,
    Max:    30 * time.Second,
    Factor: 2.0,
}

func (b BackoffStrategy) Duration(attempt int) time.Duration {
    d := float64(b.Base) * math.Pow(b.Factor, float64(attempt))
    if d > float64(b.Max) {
        return b.Max
    }
    return time.Duration(d)
}
```

### 9.3 Circuit Breaker para Notificações

```go
// CircuitBreaker previne envio contínuo para agentes offline.
type CircuitBreaker struct {
    failures    int
    maxFailures int
    state       State  // Closed, HalfOpen, Open
    lastFailure time.Time
    resetTimeout time.Duration
}

func (cb *CircuitBreaker) AllowRequest() bool {
    switch cb.state {
    case Closed:
        return true
    case Open:
        if time.Since(cb.lastFailure) > cb.resetTimeout {
            cb.state = HalfOpen
            return true
        }
        return false
    case HalfOpen:
        return true
    }
    return false
}
```

---

## 10. Estratégia de Testes

### 10.1 Pirâmide de Testes

```
         ┌──────────┐
         │   E2E    │  (Testcontainers + Swarm real/mock)
         │  (poucos)│
        ┌┴──────────┴┐
        │ Integração  │  (Testcontainers, Docker SDK mockado)
        │  (médios)   │
       ┌┴─────────────┴┐
       │   Unitários    │  (Go testing + mocks)
       │   (muitos)     │
       └────────────────┘
```

### 10.2 Testes Unitários

| Pacote | O que testar | Abordagem |
|--------|-------------|-----------|
| `pkg/models` | Serialização YAML/JSON, validação de campos | Golden files, table-driven tests |
| `config/generator` | Geração correta de YAMLs para cada cenário | Compare com fixtures |
| `config/diff` | Detecção de add/remove/modify, hash comparison | Table-driven com estados pré-definidos |
| `config/state` | Save/Load/Clear, merge incremental | Temp dir isolado |
| `writer` | Atomic write, falhas de escrita, concorrência | Temp dir, simulate fs errors |
| `events` | Parse de eventos Docker, filtros | Mock Docker client |
| `discovery` | Resolução de IPs, cache, fallback | Mock Docker SDK |
| `hub/notifier` | HTTP push, retry, backoff, circuit breaker | httptest.Server |
| `agent/receiver` | Parse de payload, resposta HTTP | httptest.Server |

### 10.3 Testes de Integração

```go
// test/integration/swarm_test.go
package integration

import (
    "context"
    "testing"

    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/compose"
)

// TestSwarmFederation_HubAgentCommunication
// Valida que Hub consegue notificar Agente via HTTP.
func TestSwarmFederation_HubAgentCommunication(t *testing.T) {
    // Setup: Docker Compose com sidecar-global + sidecar-local
    // Usa testcontainers para subir o ambiente
    // ...
}

// TestSwarmFederation_ConfigGeneration
// Valida que Agente gera YAMLs corretos ao receber notificação.
func TestSwarmFederation_ConfigGeneration(t *testing.T) {
    // Setup: Agente isolado com Docker socket mock
    // Envia notificação → verifica YAMLs gerados
    // ...
}

// TestSwarmFederation_OrphanCleaner
// Valida que configs órfãs são removidas.
func TestSwarmFederation_OrphanCleaner(t *testing.T) {
    // Setup: Diretório com configs velhas
    // Executa cleaner → verifica remoção
    // ...
}
```

### 10.4 Fixtures / Golden Files

```yaml
# test/fixtures/federation.yaml
# Golden file para teste de geração de federação
http:
  services:
    nginx-federation:
      loadBalancer:
        servers:
          - url: "http://192.168.1.10:80"
```

```yaml
# test/fixtures/local_services.yaml
# Golden file para teste de geração de serviços locais
http:
  services:
    app-local:
      loadBalancer:
        servers:
          - url: "http://10.0.0.2:3000"
        passHostHeader: true
```

### 10.5 Mocks

```go
// test/mocks/event_listener.go
type MockEventListener struct {
    events chan models.ServiceEvent
    err    error
}

func (m *MockEventListener) Events(ctx context.Context) (<-chan models.ServiceEvent, error) {
    return m.events, m.err
}

func (m *MockEventListener) Start(ctx context.Context) error {
    return nil
}

func (m *MockEventListener) Close() error {
    return nil
}
```

```go
// test/mocks/config_writer.go
type MockConfigWriter struct {
    written map[string][]byte
    mu      sync.Mutex
}

func (m *MockConfigWriter) WriteConfig(path string, data []byte) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.written == nil {
        m.written = make(map[string][]byte)
    }
    m.written[path] = data
    return nil
}

func (m *MockConfigWriter) GetWritten(path string) []byte {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.written[path]
}
```

### 10.6 Roteiro de Testes

| Fase | O que testar | Prioridade |
|------|-------------|-----------|
| 1 | Geração de YAML (generator_test.go) | Crítica |
| 2 | Escrita atômica (writer_test.go) | Crítica |
| 3 | Diff engine (diff_test.go) | Crítica |
| 4 | Descoberta de containers (container_test.go) | Alta |
| 5 | Notificador com HTTP mock (notifier_test.go) | Alta |
| 6 | Receptor HTTP (receiver_test.go) | Alta |
| 7 | Orphan cleaner (orphan_cleaner_test.go) | Média |
| 8 | Integração Hub-Agente (swarm_test.go) | Média |
| 9 | State manager (state_manager_test.go) | Média |
| 10 | Poller + Watcher (watcher_test.go) | Baixa |

---

## 11. Labels do Docker Swarm (Convenção)

As labels seguem o padrão `traefik.federation.*` para serem distintas das labels nativas do Traefik (`traefik.*`).

```yaml
# Exemplo completo de serviço com labels de federação
version: "3.8"
services:
  myapp:
    image: myapp:latest
    deploy:
      replicas: 2
      labels:
        # Habilita este serviço para federação (obrigatório)
        - "traefik.federation.enable=true"

        # Host de roteamento (obrigatório)
        - "traefik.federation.host=myapp.internal"

        # Porta do container (obrigatório)
        - "traefik.federation.port=3000"

        # Protocolo (opcional, padrão: http)
        - "traefik.federation.protocol=http"

        # Entrypoints (opcional, padrão: ["web"])
        - "traefik.federation.entrypoints=web,websecure"

        # Middlewares (opcional, formato: nome@provider)
        - "traefik.federation.middlewares=cors@file,rate-limit-global@file"

        # TLS (opcional, padrão: false)
        - "traefik.federation.tls=true"

        # Sticky session (opcional, padrão: false)
        - "traefik.federation.sticky=true"
        - "traefik.federation.sticky.cookie=_sticky_myapp"

        # Health check path (opcional)
        - "traefik.federation.healthcheck.path=/health"
        - "traefik.federation.healthcheck.interval=10s"

        # Peso no load balancer (opcional, padrão: 1)
        - "traefik.federation.weight=3"
```

---

## 12. Variáveis de Ambiente

### 12.1 Hub Central (sidecar-global)

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `MODE` | `global` | Modo de operação |
| `SHARED_OUTPUT_PATH` | `/data/shared/generated` | Diretório para YAMLs compartilhados |
| `LOCAL_OUTPUT_PATH` | `/data/local/generated` | Diretório para YAMLs locais (não usado no hub) |
| `REGISTRY_PATH` | `/data/shared/registry` | Diretório para registro de agentes |
| `POLL_INTERVAL` | `5` | Intervalo de polling da Swarm API (segundos) |
| `LOG_LEVEL` | `info` | Nível de log (debug, info, warn, error) |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Endpoint Docker |
| `NOTIFY_PORT` | `9090` | Porta dos agentes para notificação |
| `NOTIFY_TIMEOUT` | `5s` | Timeout de notificação |
| `NOTIFY_MAX_RETRIES` | `3` | Máximo de tentativas de notificação |
| `HEARTBEAT_TTL` | `60s` | TTL para considerar agente offline |

### 12.2 Agente Local (sidecar-local)

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `MODE` | `local` | Modo de operação |
| `SHARED_OUTPUT_PATH` | `/data/shared/generated` | Diretório para YAMLs compartilhados (leitura) |
| `LOCAL_OUTPUT_PATH` | `/data/local/generated` | Diretório para YAMLs locais (escrita) |
| `REGISTRY_PATH` | `/data/shared/registry` | Diretório para registro do agente |
| `POLL_INTERVAL` | `30` | Intervalo de polling local (segundos) |
| `LOG_LEVEL` | `info` | Nível de log |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Endpoint Docker local |
| `AGENT_PORT` | `9090` | Porta do servidor HTTP |
| `HUB_ADDR` | (auto-descoberto) | Endereço do Hub para registro |

---

## 13. Considerações de Segurança

1. **Mínimo privilégio:** O container `sidecar-local` monta o Docker socket como `read-only`
2. **Syncthing:** Opera na rede host, sem expor portas desnecessárias para o Swarm
3. **Comunicação Hub-Agente:** Deve operar em rede interna confiável (VLAN/mesma LAN)
4. **Atomic writes:** Previnem leitura de arquivos parcialmente escritos
5. **Validação de labels:** Labels inválidas são ignoradas com log de warning
6. **Rate limiting:** Middleware `rate-limit-global` protege contra abuso

---

## 14. Glossário

| Termo | Definição |
|-------|-----------|
| **Hub** | Nó manager que centraliza descoberta de serviços e gera configs compartilhadas |
| **Agente** | Nó worker (ou manager) que gerencia configs locais do Traefik |
| **Federação** | Roteamento de tráfego entre nós sem overlay network |
| **Syncthing** | Ferramenta P2P de sincronização de arquivos substituindo shared volumes |
| **Bridge IP** | IP do container na rede bridge local (ex: 10.0.0.2) |
| **Atomic Write** | Padrão tempfile + fsync + rename para escrita segura |
| **Orphan Cleaner** | Rotina que remove configs de serviços que não existem mais |
| **Golden File** | Arquivo de referência para testes de snapshot |

---

*Documento de Arquitetura v1.0 — Gerado em 2026-05-15*
