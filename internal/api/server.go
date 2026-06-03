package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// =============================================================================
// AgentServer - Servidor HTTP do Agente Local
// =============================================================================

// AgentServer é o servidor HTTP do agente local.
// Endpoints:
// POST /notify - recebe notificações push do Hub
// GET  /status - retorna status do agente
type AgentServer struct {
	addr          string
	notifyHandler func(*models.NotificationPayload) error
	statusHandler func() *models.AgentStatusResponse
	server        *http.Server
	listener      net.Listener
	logger        *logrus.Entry
}

// NewAgentServer cria um novo servidor HTTP para o agente.
// addr: ":9090" (porta configurável via flag/env)
// notifyHandler: função chamada quando chega POST /notify
// statusHandler: função chamada quando chega GET /status
func NewAgentServer(addr string,
	notifyHandler func(*models.NotificationPayload) error,
	statusHandler func() *models.AgentStatusResponse) *AgentServer {
	return &AgentServer{
		addr:          addr,
		notifyHandler: notifyHandler,
		statusHandler: statusHandler,
		logger:        logrus.WithField("component", "api.agent-server"),
	}
}

// Start inicia o servidor HTTP em uma goroutine.
// Retorna erro se não conseguir escutar na porta.
func (s *AgentServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /notify", s.handleNotify)
	mux.HandleFunc("GET /status", s.handleStatus)

	handler := loggingMiddleware(s.logger, mux)

	s.server = &http.Server{
		Addr:    s.addr,
		Handler: handler,
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = listener

	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.WithError(err).Error("agent server error")
		}
	}()

	s.logger.WithField("addr", s.addr).Info("agent server started")
	return nil
}

// Stop interrompe o servidor gracefulmente (shutdown com timeout).
func (s *AgentServer) Stop(ctx context.Context) error {
	s.logger.Info("stopping agent server")
	return s.server.Shutdown(ctx)
}

// Port retorna a porta real em que o servidor está escutando.
// Útil para testes com porta aleatória.
func (s *AgentServer) Port() int {
	if s.listener == nil {
		return 0
	}
	return s.listener.Addr().(*net.TCPAddr).Port
}

// handleNotify processa POST /notify.
// Decodifica JSON do body como NotificationPayload.
// Valida campos obrigatórios (Action, ServiceName).
// Chama notifyHandler em goroutine separada (não bloquear resposta).
// Retorna 200 imediatamente após validação.
func (s *AgentServer) handleNotify(w http.ResponseWriter, r *http.Request) {
	var payload models.NotificationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if payload.Action == "" {
		respondError(w, http.StatusBadRequest, "action is required")
		return
	}

	// Processa em goroutine separada para não bloquear a resposta
	go func() {
		if err := s.notifyHandler(&payload); err != nil {
			s.logger.WithError(err).
				WithFields(logrus.Fields{
					"action":       payload.Action,
					"service_name": payload.ServiceName,
				}).Error("notify handler failed")
		}
	}()

	respondJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// handleStatus processa GET /status.
// Chama statusHandler e retorna JSON.
func (s *AgentServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := s.statusHandler()
	if status == nil {
		respondError(w, http.StatusInternalServerError, "status handler returned nil")
		return
	}
	respondJSON(w, http.StatusOK, status)
}

// =============================================================================
// HubServer - Servidor HTTP do Hub Central
// =============================================================================

// HubServer é o servidor HTTP do Hub Central.
// Endpoints:
// GET  /services/<name>       - retorna metadata de um serviço
// GET  /state                 - retorna estado completo do cluster
// GET  /shared/federation     - retorna config de federação (serviços remotos)
// GET  /shared/middlewares    - retorna config de middlewares globais
// GET  /health                - healthcheck
type HubServer struct {
	addr                   string
	stateManager           func() *models.ClusterState
	serviceLookup          func(name string) (*models.ServiceMeta, bool)
	sharedFederationHandler func() *models.TraefikConfig
	sharedMiddlewaresHandler func() *models.TraefikConfig
	server                 *http.Server
	listener               net.Listener
	logger                 *logrus.Entry
}

// NewHubServer cria um novo servidor HTTP para o Hub Central.
func NewHubServer(addr string,
	stateManager func() *models.ClusterState,
	serviceLookup func(name string) (*models.ServiceMeta, bool),
	sharedFederationHandler func() *models.TraefikConfig,
	sharedMiddlewaresHandler func() *models.TraefikConfig) *HubServer {
	return &HubServer{
		addr:                    addr,
		stateManager:            stateManager,
		serviceLookup:           serviceLookup,
		sharedFederationHandler: sharedFederationHandler,
		sharedMiddlewaresHandler: sharedMiddlewaresHandler,
		logger:                  logrus.WithField("component", "api.hub-server"),
	}
}

// Start inicia o servidor HTTP do Hub em uma goroutine.
// Retorna erro se não conseguir escutar na porta.
func (s *HubServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /services/{name}", s.handleGetService)
	mux.HandleFunc("GET /state", s.handleGetState)
	mux.HandleFunc("GET /shared/federation", s.handleSharedFederation)
	mux.HandleFunc("GET /shared/middlewares", s.handleSharedMiddlewares)
	mux.HandleFunc("GET /health", s.handleHealth)

	handler := loggingMiddleware(s.logger, mux)

	s.server = &http.Server{
		Addr:    s.addr,
		Handler: handler,
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = listener

	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.WithError(err).Error("hub server error")
		}
	}()

	s.logger.WithField("addr", s.addr).Info("hub server started")
	return nil
}

// Stop interrompe o servidor do Hub gracefulmente (shutdown com timeout).
func (s *HubServer) Stop(ctx context.Context) error {
	s.logger.Info("stopping hub server")
	return s.server.Shutdown(ctx)
}

// Addr retorna o endereço configurado do servidor (ex: ":8080").
func (s *HubServer) Addr() string {
	return s.addr
}

// Port retorna a porta real em que o servidor está escutando.
// Útil para testes com porta aleatória.
func (s *HubServer) Port() int {
	if s.listener == nil {
		return 0
	}
	return s.listener.Addr().(*net.TCPAddr).Port
}

// handleGetService processa GET /services/{name}.
// Retorna metadata do serviço ou 404 se não encontrado.
func (s *HubServer) handleGetService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		respondError(w, http.StatusBadRequest, "service name is required")
		return
	}

	service, ok := s.serviceLookup(name)
	if !ok {
		respondError(w, http.StatusNotFound, "service not found")
		return
	}

	respondJSON(w, http.StatusOK, service)
}

// handleGetState processa GET /state.
// Retorna o estado completo do cluster.
func (s *HubServer) handleGetState(w http.ResponseWriter, r *http.Request) {
	state := s.stateManager()
	respondJSON(w, http.StatusOK, state)
}

// handleSharedFederation processa GET /shared/federation.
// Retorna o TraefikConfig de federação (serviços remotos) como YAML.
func (s *HubServer) handleSharedFederation(w http.ResponseWriter, r *http.Request) {
	if s.sharedFederationHandler == nil {
		respondError(w, http.StatusNotFound, "federation handler not available")
		return
	}
	config := s.sharedFederationHandler()
	if config == nil {
		respondJSON(w, http.StatusOK, &models.TraefikConfig{})
		return
	}
	respondYAML(w, http.StatusOK, config)
}

// handleSharedMiddlewares processa GET /shared/middlewares.
// Retorna o TraefikConfig de middlewares globais como YAML.
func (s *HubServer) handleSharedMiddlewares(w http.ResponseWriter, r *http.Request) {
	if s.sharedMiddlewaresHandler == nil {
		respondError(w, http.StatusNotFound, "middlewares handler not available")
		return
	}
	config := s.sharedMiddlewaresHandler()
	if config == nil {
		respondJSON(w, http.StatusOK, &models.TraefikConfig{})
		return
	}
	respondYAML(w, http.StatusOK, config)
}

// handleHealth processa GET /health.
// Retorna 200 com {"status": "ok"}.
func (s *HubServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Helpers
// =============================================================================

// respondJSON escreve uma resposta JSON com o status code e dados fornecidos.
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logrus.WithError(err).Error("failed to encode JSON response")
	}
}

// respondYAML escreve uma resposta YAML com o status code e dados fornecidos.
func respondYAML(w http.ResponseWriter, status int, data interface{}) {
	raw, err := yaml.Marshal(data)
	if err != nil {
		logrus.WithError(err).Error("failed to encode YAML response")
		respondError(w, http.StatusInternalServerError, "failed to encode YAML")
		return
	}
	w.Header().Set("Content-Type", "application/x-yaml")
	w.WriteHeader(status)
	w.Write(raw)
}

// respondError escreve uma resposta de erro JSON.
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// =============================================================================
// Middleware
// =============================================================================

// responseWriter wrappea http.ResponseWriter para capturar o status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// loggingMiddleware loga method, path, status code e duração de cada requisição.
func loggingMiddleware(logger *logrus.Entry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)

		logger.WithFields(logrus.Fields{
			"method":   r.Method,
			"type":     "api",
			"path":     r.URL.Path,
			"status":   rw.statusCode,
			"duration": duration,
		}).Debug("request completed")
	})
}
