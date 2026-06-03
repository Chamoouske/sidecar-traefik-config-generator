package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// =============================================================================
// HubClient - Cliente HTTP do Hub Central
// =============================================================================

// HubClient é o cliente HTTP do Hub para comunicação com Agentes e consultas ao Hub.
type HubClient struct {
	client *http.Client
	logger *logrus.Entry
}

// NewHubClient cria um novo cliente HTTP com configurações:
// - Timeout: 10s
// - Transport com keep-alive
func NewHubClient() *HubClient {
	transport := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: false,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	return &HubClient{
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
		logger: logrus.WithField("component", "api.hub-client"),
	}
}

// NotifyAgent envia notificação push para um agente específico.
// URL: http://<agent-addr>/notify
// Body: JSON do NotificationPayload
// Retorna erro se timeout ou falha de conexão.
func (c *HubClient) NotifyAgent(ctx context.Context, agentAddr string, payload *models.NotificationPayload) error {
	url := fmt.Sprintf("http://%s/notify", agentAddr)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal notification payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	c.logger.WithFields(logrus.Fields{
		"agent_addr":   agentAddr,
		"action":       payload.Action,
		"service_name": payload.ServiceName,
	}).Debug("sending notification to agent")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to notify agent %s: %w", agentAddr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent %s returned status %d: %s", agentAddr, resp.StatusCode, string(bodyBytes))
	}

	c.logger.WithField("agent_addr", agentAddr).Debug("notification sent successfully")
	return nil
}

// GetService obtém metadados de um serviço do Hub.
// Usado pelo Agente para fazer pull após notificação.
// URL: http://<hub-addr>/services/<name>
func (c *HubClient) GetService(ctx context.Context, hubAddr string, serviceName string) (*models.ServiceMeta, error) {
	url := fmt.Sprintf("http://%s/services/%s", hubAddr, serviceName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.logger.WithFields(logrus.Fields{
		"hub_addr":     hubAddr,
		"service_name": serviceName,
	}).Debug("fetching service from hub")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get service %s from hub %s: %w", serviceName, hubAddr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("hub %s returned status %d for service %s: %s",
			hubAddr, resp.StatusCode, serviceName, string(bodyBytes))
	}

	var service models.ServiceMeta
	if err := json.NewDecoder(resp.Body).Decode(&service); err != nil {
		return nil, fmt.Errorf("failed to decode service response: %w", err)
	}

	return &service, nil
}

// GetState obtém o estado completo do cluster do Hub.
// URL: http://<hub-addr>/state
func (c *HubClient) GetState(ctx context.Context, hubAddr string) (*models.ClusterState, error) {
	url := fmt.Sprintf("http://%s/state", hubAddr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.logger.WithField("hub_addr", hubAddr).Debug("fetching cluster state from hub")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get state from hub %s: %w", hubAddr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("hub %s returned status %d: %s",
			hubAddr, resp.StatusCode, string(bodyBytes))
	}

	var state models.ClusterState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return nil, fmt.Errorf("failed to decode cluster state: %w", err)
	}

	return &state, nil
}

// GetFederationConfig obtém a configuração de federação do Hub.
// URL: http://<hub-addr>/shared/federation
// Retorna o TraefikConfig com serviços federados (para Traefik File Provider).
func (c *HubClient) GetFederationConfig(ctx context.Context, hubAddr string) (*models.TraefikConfig, error) {
	url := fmt.Sprintf("http://%s/shared/federation", hubAddr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.logger.WithField("hub_addr", hubAddr).Debug("fetching federation config from hub")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get federation config from hub %s: %w", hubAddr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("hub %s returned status %d: %s",
			hubAddr, resp.StatusCode, string(bodyBytes))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read federation config: %w", err)
	}

	var config models.TraefikConfig
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal federation config: %w", err)
	}

	return &config, nil
}

// GetMiddlewareConfig obtém a configuração de middlewares do Hub.
// URL: http://<hub-addr>/shared/middlewares
// Retorna o TraefikConfig com middlewares globais (para Traefik File Provider).
func (c *HubClient) GetMiddlewareConfig(ctx context.Context, hubAddr string) (*models.TraefikConfig, error) {
	url := fmt.Sprintf("http://%s/shared/middlewares", hubAddr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.logger.WithField("hub_addr", hubAddr).Debug("fetching middlewares config from hub")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get middlewares config from hub %s: %w", hubAddr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("hub %s returned status %d: %s",
			hubAddr, resp.StatusCode, string(bodyBytes))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read middlewares config: %w", err)
	}

	var config models.TraefikConfig
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal middlewares config: %w", err)
	}

	return &config, nil
}

// Healthy verifica se o hub está acessível.
// URL: http://<hub-addr>/health
func (c *HubClient) Healthy(ctx context.Context, hubAddr string) bool {
	url := fmt.Sprintf("http://%s/health", hubAddr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		c.logger.WithError(err).WithField("hub_addr", hubAddr).Warn("failed to create health check request")
		return false
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.WithError(err).WithField("hub_addr", hubAddr).Debug("hub health check failed")
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
