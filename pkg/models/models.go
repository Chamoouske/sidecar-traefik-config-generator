package models

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// ActionType
// =============================================================================

// ActionType representa o tipo de ação a ser executada em um serviço.
type ActionType string

const (
	ActionCreate ActionType = "CREATE"
	ActionUpdate ActionType = "UPDATE"
	ActionDelete ActionType = "DELETE"
)

// =============================================================================
// EventType
// =============================================================================

// EventType representa o tipo de evento do cluster.
type EventType string

const (
	EventServiceCreate EventType = "SERVICE_CREATE"
	EventServiceUpdate EventType = "ServiceUpdate"
	EventServiceRemove EventType = "SERVICE_REMOVE"
	EventTaskDeploy    EventType = "TASK_DEPLOY"
	EventTaskRemove    EventType = "TASK_REMOVE"
	EventNodeUpdate    EventType = "NODE_UPDATE"
)

// ClusterEvent representa um evento ocorrido no cluster Swarm.
type ClusterEvent struct {
	Type      EventType    `json:"type"`
	ServiceID string       `json:"service_id,omitempty"`
	Service   *ServiceMeta `json:"service,omitempty"`
	NodeID    string       `json:"node_id,omitempty"`
	NodeIP    string       `json:"node_ip,omitempty"`
	Timestamp time.Time    `json:"timestamp"`
}

// =============================================================================
// ServiceMeta
// =============================================================================

// ServiceMeta contém metadados extraídos das labels do service Docker Swarm.
type ServiceMeta struct {
	Name        string            `json:"name" yaml:"name"`
	Host        string            `json:"host,omitempty" yaml:"host,omitempty"`
	Port        int               `json:"port,omitempty" yaml:"port,omitempty"`
	TLS         bool              `json:"tls,omitempty" yaml:"tls,omitempty"`
	Entrypoints []string          `json:"entrypoints,omitempty" yaml:"entrypoints,omitempty"`
	Middlewares []string          `json:"middlewares,omitempty" yaml:"middlewares,omitempty"`
	Enabled     bool              `json:"enabled" yaml:"enabled"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	NodeID      string            `json:"node_id,omitempty" yaml:"node_id,omitempty"`
	TaskID      string            `json:"task_id,omitempty" yaml:"task_id,omitempty"`
}

// ParseServiceMeta extrai ServiceMeta das labels do Docker Swarm service/task.
// Labels seguem o padrão: traefik.federation.enabled, traefik.federation.host, etc.
func ParseServiceMeta(labels map[string]string) ServiceMeta {
	meta := ServiceMeta{
		Labels:  labels,
		Enabled: isLabelTrue(labels, "traefik.federation.enabled"),
		Host:    labels["traefik.federation.host"],
		TLS:     isLabelTrue(labels, "traefik.federation.tls"),
	}

	// Extrai porta do label traefik.federation.port
	if portStr, ok := labels["traefik.federation.port"]; ok {
		fmt.Sscanf(portStr, "%d", &meta.Port)
	}

	// Extrai entrypoints do label traefik.federation.entrypoints (separados por vírgula)
	if ep, ok := labels["traefik.federation.entrypoints"]; ok {
		meta.Entrypoints = splitAndTrim(ep)
	}

	// Extrai middlewares do label traefik.federation.middlewares
	if mw, ok := labels["traefik.federation.middlewares"]; ok {
		meta.Middlewares = splitAndTrim(mw)
	}

	// Extrai nome do serviço do label traefik.federation.name ou usa o valor original
	if name, ok := labels["traefik.federation.name"]; ok {
		meta.Name = name
	}

	return meta
}

// isLabelTrue verifica se um label booleano é verdadeiro.
func isLabelTrue(labels map[string]string, key string) bool {
	v, ok := labels[key]
	if !ok {
		return false
	}
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "true" || v == "1" || v == "yes"
}

// splitAndTrim divide uma string por vírgulas e remove espaços de cada parte.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// =============================================================================
// NodeInfo
// =============================================================================

// NodeInfo contém informações de um nó do cluster Docker Swarm.
type NodeInfo struct {
	ID         string            `json:"id" yaml:"id"`
	Hostname   string            `json:"hostname" yaml:"hostname"`
	Addr       string            `json:"addr" yaml:"addr"`                                   // IP LAN
	BridgeAddr string            `json:"bridge_addr,omitempty" yaml:"bridge_addr,omitempty"` // IP na bridge local
	Role       string            `json:"role" yaml:"role"`                                   // manager ou worker
	Labels     map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	IsManager  bool              `json:"is_manager" yaml:"is_manager"`
}

// =============================================================================
// LocalTaskInfo
// =============================================================================

// LocalTaskInfo contém informações de uma task Docker Swarm executando no nó local.
type LocalTaskInfo struct {
	TaskID      string `json:"task_id" yaml:"task_id"`
	ServiceName string `json:"service_name" yaml:"service_name"`
	ContainerID string `json:"container_id" yaml:"container_id"`
	BridgeIP    string `json:"bridge_ip" yaml:"bridge_ip"`
	NodeID      string `json:"node_id" yaml:"node_id"`
	NodeAddr    string `json:"node_addr" yaml:"node_addr"`
	Status      string `json:"status" yaml:"status"` // running, pending, etc.
}

// =============================================================================
// FederationTarget
// =============================================================================

// FederationTarget representa um serviço remoto descoberto via federação.
type FederationTarget struct {
	ServiceName string `json:"service_name" yaml:"service_name"`
	NodeIP      string `json:"node_ip" yaml:"node_ip"`
	NodeID      string `json:"node_id" yaml:"node_id"`
	Port        int    `json:"port" yaml:"port"` // Porta do Traefik (default 80)
	TLS         bool   `json:"tls" yaml:"tls"`
}

// =============================================================================
// NotificationPayload
// =============================================================================

// NotificationPayload é a estrutura enviada em notificações do Hub para os Agentes.
type NotificationPayload struct {
	Action      ActionType `json:"action" yaml:"action"`
	ServiceName string     `json:"service_name" yaml:"service_name"`
	NodeID      string     `json:"node_id,omitempty" yaml:"node_id,omitempty"`
	HubAddr     string     `json:"hub_addr,omitempty" yaml:"hub_addr,omitempty"` // NOVO: IP:porta do Hub para os agentes usarem em vez de DNS
	Timestamp   time.Time  `json:"timestamp" yaml:"timestamp"`
}

// =============================================================================
// AgentState
// =============================================================================

// AgentState representa o estado de um agente no cluster.
type AgentState struct {
	NodeID   string    `json:"node_id" yaml:"node_id"`
	Addr     string    `json:"addr" yaml:"addr"` // IP do nó onde o agente roda
	Port     int       `json:"port" yaml:"port"` // Porta do agente (ex: 9090)
	LastSeen time.Time `json:"last_seen" yaml:"last_seen"`
	Online   bool      `json:"online" yaml:"online"`
	Version  string    `json:"version,omitempty" yaml:"version,omitempty"`
}

// AgentStatusResponse é a resposta do endpoint /status do agente.
type AgentStatusResponse struct {
	NodeID          string    `json:"node_id"`
	Hostname        string    `json:"hostname"`
	Uptime          string    `json:"uptime"`
	LocalServices   int       `json:"local_services"`
	GeneratedConfig bool      `json:"generated_config"`
	LastUpdate      time.Time `json:"last_update"`
}

// =============================================================================
// TraefikConfig - Serialização YAML do Traefik File Provider
// =============================================================================

// TraefikConfig representa a configuração dinâmica do Traefik (File Provider).
type TraefikConfig struct {
	HTTP *HTTPConfig `json:"http,omitempty" yaml:"http,omitempty"`
	TCP  *TCPConfig  `json:"tcp,omitempty" yaml:"tcp,omitempty"`
}

// HTTPConfig contém as definições HTTP (routers, services, middlewares).
type HTTPConfig struct {
	Routers     map[string]*RouterConfig     `json:"routers,omitempty" yaml:"routers,omitempty"`
	Services    map[string]*ServiceConfig    `json:"services,omitempty" yaml:"services,omitempty"`
	Middlewares map[string]*MiddlewareConfig `json:"middlewares,omitempty" yaml:"middlewares,omitempty"`
}

// RouterConfig define um router HTTP do Traefik.
type RouterConfig struct {
	Rule        string     `json:"rule" yaml:"rule"`
	Service     string     `json:"service" yaml:"service"`
	EntryPoints []string   `json:"entryPoints,omitempty" yaml:"entryPoints,omitempty"`
	TLS         *TLSConfig `json:"tls,omitempty" yaml:"tls,omitempty"`
	Middlewares []string   `json:"middlewares,omitempty" yaml:"middlewares,omitempty"`
}

// TLSConfig define configurações de TLS para um router.
type TLSConfig struct {
	CertResolver string `json:"certResolver,omitempty" yaml:"certResolver,omitempty"`
}

// ServiceConfig define um service HTTP do Traefik.
type ServiceConfig struct {
	LoadBalancer *LoadBalancerConfig `json:"loadBalancer,omitempty" yaml:"loadBalancer,omitempty"`
}

// LoadBalancerConfig define a configuração de load balancer de um service.
type LoadBalancerConfig struct {
	Servers        []*ServerConfig `json:"servers,omitempty" yaml:"servers,omitempty"`
	PassHostHeader *bool           `json:"passHostHeader,omitempty" yaml:"passHostHeader,omitempty"`
}

// ServerConfig define um servidor backend do load balancer.
type ServerConfig struct {
	URL string `json:"url" yaml:"url"`
}

// MiddlewareConfig define um middleware do Traefik.
type MiddlewareConfig struct {
	Headers        *HeadersConfig        `json:"headers,omitempty" yaml:"headers,omitempty"`
	RateLimit      *RateLimitConfig      `json:"rateLimit,omitempty" yaml:"rateLimit,omitempty"`
	Retry          *RetryConfig          `json:"retry,omitempty" yaml:"retry,omitempty"`
	CircuitBreaker *CircuitBreakerConfig `json:"circuitBreaker,omitempty" yaml:"circuitBreaker,omitempty"`
}

// HeadersConfig define configurações de headers para o middleware de headers.
type HeadersConfig struct {
	AccessControlAllowMethods []string `json:"accessControlAllowMethods,omitempty" yaml:"accessControlAllowMethods,omitempty"`
	AccessControlAllowOrigins []string `json:"accessControlAllowOrigins,omitempty" yaml:"accessControlAllowOrigins,omitempty"`
}

// RateLimitConfig define configurações de rate limiting.
type RateLimitConfig struct {
	Average int `json:"average,omitempty" yaml:"average,omitempty"`
	Burst   int `json:"burst,omitempty" yaml:"burst,omitempty"`
}

// RetryConfig define configurações de retry.
type RetryConfig struct {
	Attempts int `json:"attempts,omitempty" yaml:"attempts,omitempty"`
}

// CircuitBreakerConfig define configurações de circuit breaker.
type CircuitBreakerConfig struct {
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty"`
}

// TCPConfig contém as definições TCP (routers e services).
type TCPConfig struct {
	Routers  map[string]*TCPRouterConfig  `json:"routers,omitempty" yaml:"routers,omitempty"`
	Services map[string]*TCPServiceConfig `json:"services,omitempty" yaml:"services,omitempty"`
}

// TCPRouterConfig define um router TCP do Traefik.
type TCPRouterConfig struct {
	Rule        string     `json:"rule" yaml:"rule"`
	Service     string     `json:"service" yaml:"service"`
	EntryPoints []string   `json:"entryPoints,omitempty" yaml:"entryPoints,omitempty"`
	TLS         *TLSConfig `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// TCPServiceConfig define um service TCP do Traefik.
type TCPServiceConfig struct {
	LoadBalancer *TCPLoadBalancerConfig `json:"loadBalancer,omitempty" yaml:"loadBalancer,omitempty"`
}

// TCPLoadBalancerConfig define a configuração de load balancer TCP.
type TCPLoadBalancerConfig struct {
	Servers []*ServerConfig `json:"servers,omitempty" yaml:"servers,omitempty"`
}

// =============================================================================
// DiffResult
// =============================================================================

// DiffResult contém o resultado de uma comparação entre dois estados.
type DiffResult struct {
	HasChanges bool     `json:"has_changes"`
	Added      []string `json:"added,omitempty"`
	Removed    []string `json:"removed,omitempty"`
	Modified   []string `json:"modified,omitempty"`
}

// IsEmpty retorna true se não há mudanças.
func (d *DiffResult) IsEmpty() bool {
	return !d.HasChanges
}

// =============================================================================
// ClusterState
// =============================================================================

// ClusterState mantém o estado completo do cluster Swarm com proteção concorrente.
type ClusterState struct {
	mu          sync.RWMutex
	Services    map[string]*ServiceMeta      `json:"services"`
	Nodes       map[string]*NodeInfo         `json:"nodes"`
	Tasks       map[string]*LocalTaskInfo    `json:"tasks"`
	Agents      map[string]*AgentState       `json:"agents"`
	Federations map[string]*FederationTarget `json:"federations"`
}

// NewClusterState cria uma nova instância de ClusterState inicializada.
func NewClusterState() *ClusterState {
	return &ClusterState{
		Services:    make(map[string]*ServiceMeta),
		Nodes:       make(map[string]*NodeInfo),
		Tasks:       make(map[string]*LocalTaskInfo),
		Agents:      make(map[string]*AgentState),
		Federations: make(map[string]*FederationTarget),
	}
}

// Lock bloqueia o mutex para escrita.
func (s *ClusterState) Lock() {
	s.mu.Lock()
}

// Unlock desbloqueia o mutex de escrita.
func (s *ClusterState) Unlock() {
	s.mu.Unlock()
}

// RLock bloqueia o mutex para leitura.
func (s *ClusterState) RLock() {
	s.mu.RLock()
}

// RUnlock desbloqueia o mutex de leitura.
func (s *ClusterState) RUnlock() {
	s.mu.RUnlock()
}

// =============================================================================
// Constantes
// =============================================================================

// DefaultTraefikHTTPPort é a porta padrão do Traefik para HTTP.
const DefaultTraefikHTTPPort = 80

// DefaultTraefikHTTPSPort é a porta padrão do Traefik para HTTPS.
const DefaultTraefikHTTPSPort = 443

// =============================================================================
// Funções Helper
// =============================================================================

// FederationServiceName gera o nome do service de federação.
// Exemplo: "service-nginx-federation".
func FederationServiceName(serviceName string) string {
	return fmt.Sprintf("service-%s-federation", serviceName)
}

// LocalRouterName gera o nome do router local.
// Exemplo: "nginx-local-router".
func LocalRouterName(serviceName string) string {
	return fmt.Sprintf("%s-local-router", serviceName)
}

// LocalServiceName gera o nome do service local.
// Exemplo: "nginx-local-service".
func LocalServiceName(serviceName string) string {
	return fmt.Sprintf("%s-local-service", serviceName)
}

// FederationRouterName gera o nome do router federado.
// Exemplo: "nginx-federation-router".
func FederationRouterName(serviceName string) string {
	return fmt.Sprintf("%s-federation-router", serviceName)
}

// =============================================================================
// Interfaces
// =============================================================================

// ServiceEventListener detecta mudanças em serviços Swarm.
type ServiceEventListener interface {
	Events() <-chan *ClusterEvent
	Start(ctx interface{}) error
	Stop() error
}

// ConfigWriter permite escrita atômica de YAML de configuração do Traefik.
type ConfigWriter interface {
	WriteConfig(path string, config *TraefikConfig) error
	RemoveConfig(path string) error
}

// NodeDiscovery permite resolução de IPs de nós do cluster Swarm.
type NodeDiscovery interface {
	GetNodeIP(ctx interface{}, nodeID string) (string, error)
	ListNodes(ctx interface{}) ([]*NodeInfo, error)
	WatchNodes(ctx interface{}) (<-chan *NodeInfo, error)
}

// ContainerDiscovery permite resolução de IPs de containers na bridge local.
type ContainerDiscovery interface {
	GetContainerBridgeIP(ctx interface{}, containerID string, bridgeName string) (string, error)
	ListLocalContainers(ctx interface{}) ([]*LocalTaskInfo, error)
}

// AgentNotifier permite push notifications do Hub para os Agentes.
type AgentNotifier interface {
	Notify(ctx interface{}, agent *AgentState, payload *NotificationPayload) error
	Broadcast(ctx interface{}, payload *NotificationPayload) error
	RegisterAgent(agent *AgentState)
	UnregisterAgent(nodeID string)
}

// OrphanCleaner permite limpeza de configs órfãs do Traefik.
type OrphanCleaner interface {
	CleanOrphans(ctx interface{}) error
}

// StateStore permite armazenamento e recuperação de estado.
type StateStore interface {
	Save(path string, data interface{}) error
	Load(path string, data interface{}) error
	Remove(path string) error
}

// ConfigGenerator gera configurações YAML do Traefik.
type ConfigGenerator interface {
	GenerateFederationConfig(services map[string]*FederationTarget) *TraefikConfig
	GenerateLocalConfig(tasks []*LocalTaskInfo, meta *ServiceMeta, isLocal bool) *TraefikConfig
	GenerateMiddlewareConfig(services map[string]*ServiceMeta) *TraefikConfig
}

// DiffEngine compara dois estados e retorna as diferenças.
type DiffEngine interface {
	Diff(previous, current interface{}) (*DiffResult, error)
	HasChanged(previous, current interface{}) bool
}

// StateManager gerencia o estado interno do cluster.
type StateManager interface {
	GetService(name string) (*ServiceMeta, bool)
	SetService(service *ServiceMeta)
	DeleteService(name string)
	GetNode(nodeID string) (*NodeInfo, bool)
	SetNode(node *NodeInfo)
	ListServices() []*ServiceMeta
	ListNodes() []*NodeInfo
	GetFederationTarget(serviceName string) (*FederationTarget, bool)
	SetFederationTarget(target *FederationTarget)
	DeleteFederationTarget(serviceName string)
}
