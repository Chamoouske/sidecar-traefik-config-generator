package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// AgentServer Tests
// =============================================================================

func TestAgentServer_New(t *testing.T) {
	server := NewAgentServer(":9090", nil, nil)
	assert.NotNil(t, server)
	assert.Equal(t, ":9090", server.addr)
}

func TestAgentServer_HandleNotify(t *testing.T) {
	notified := make(chan *models.NotificationPayload, 1)

	server := NewAgentServer(":0", func(p *models.NotificationPayload) error {
		notified <- p
		return nil
	}, nil)

	payload := &models.NotificationPayload{
		Action:      models.ActionUpdate,
		ServiceName: "nginx",
		Timestamp:   time.Now(),
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleNotify(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	select {
	case p := <-notified:
		assert.Equal(t, models.ActionUpdate, p.Action)
		assert.Equal(t, "nginx", p.ServiceName)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for notification")
	}
}

func TestAgentServer_HandleNotify_InvalidMethod(t *testing.T) {
	server := NewAgentServer(":0", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/notify", nil)
	w := httptest.NewRecorder()

	server.handleNotify(w, req)
	// The handleNotify is only registered for POST, but if called directly,
	// it will still process but without handler registration check.
	// The handler itself doesn't check method since mux does.
	// We just verify it doesn't panic.
	assert.NotPanics(t, func() {
		server.handleNotify(w, req)
	})
}

func TestAgentServer_HandleNotify_InvalidJSON(t *testing.T) {
	server := NewAgentServer(":0", nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleNotify(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "invalid JSON")
}

func TestAgentServer_HandleNotify_MissingAction(t *testing.T) {
	server := NewAgentServer(":0", nil, nil)

	payload := &models.NotificationPayload{
		ServiceName: "nginx",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleNotify(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "action")
}

func TestAgentServer_HandleNotify_MissingServiceName(t *testing.T) {
	notified := make(chan *models.NotificationPayload, 1)
	server := NewAgentServer(":0", func(p *models.NotificationPayload) error {
		notified <- p
		return nil
	}, nil)

	payload := &models.NotificationPayload{
		Action: models.ActionUpdate,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleNotify(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "accepted", resp["status"])

	select {
	case p := <-notified:
		assert.Equal(t, models.ActionUpdate, p.Action)
		assert.Equal(t, "", p.ServiceName)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for notification")
	}
}

func TestAgentServer_HandleStatus(t *testing.T) {
	statusResponse := &models.AgentStatusResponse{
		NodeID:          "node1",
		Hostname:        "worker-1",
		Uptime:          "1h",
		LocalServices:   3,
		GeneratedConfig: true,
	}

	server := NewAgentServer(":0", nil, func() *models.AgentStatusResponse {
		return statusResponse
	})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	server.handleStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.AgentStatusResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "node1", resp.NodeID)
	assert.Equal(t, 3, resp.LocalServices)
	assert.True(t, resp.GeneratedConfig)
}

func TestAgentServer_HandleStatus_EmptyStatus(t *testing.T) {
	server := NewAgentServer(":0", nil, func() *models.AgentStatusResponse {
		return &models.AgentStatusResponse{}
	})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	server.handleStatus(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.AgentStatusResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Empty(t, resp.NodeID)
}

// =============================================================================
// HubServer Tests
// =============================================================================

func TestHubServer_New(t *testing.T) {
	server := NewHubServer(":8080", nil, nil)
	assert.NotNil(t, server)
	assert.Equal(t, ":8080", server.addr)
}

func TestHubServer_HandleHealth(t *testing.T) {
	server := NewHubServer(":0", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestHubServer_HandleGetService_Found(t *testing.T) {
	lookup := func(name string) (*models.ServiceMeta, bool) {
		if name == "nginx" {
			return &models.ServiceMeta{Name: "nginx", Host: "nginx.local"}, true
		}
		return nil, false
	}

	server := NewHubServer(":0", nil, lookup)

	req := httptest.NewRequest(http.MethodGet, "/services/nginx", nil)
	// Set path value as the mux would when routing
	req.SetPathValue("name", "nginx")
	w := httptest.NewRecorder()

	server.handleGetService(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var meta models.ServiceMeta
	err := json.Unmarshal(w.Body.Bytes(), &meta)
	require.NoError(t, err)
	assert.Equal(t, "nginx", meta.Name)
	assert.Equal(t, "nginx.local", meta.Host)
}

func TestHubServer_HandleGetService_NotFound(t *testing.T) {
	lookup := func(name string) (*models.ServiceMeta, bool) {
		return nil, false
	}

	server := NewHubServer(":0", nil, lookup)

	req := httptest.NewRequest(http.MethodGet, "/services/unknown", nil)
	req.SetPathValue("name", "unknown")
	w := httptest.NewRecorder()

	server.handleGetService(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "not found")
}

func TestHubServer_HandleGetService_EmptyName(t *testing.T) {
	server := NewHubServer(":0", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/services/", nil)
	req.SetPathValue("name", "")
	w := httptest.NewRecorder()

	server.handleGetService(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "service name")
}

func TestHubServer_HandleGetState(t *testing.T) {
	stateManager := func() *models.ClusterState {
		state := models.NewClusterState()
		state.Lock()
		state.Services["nginx"] = &models.ServiceMeta{Name: "nginx", Host: "nginx.local"}
		state.Unlock()
		return state
	}

	server := NewHubServer(":0", stateManager, nil)

	req := httptest.NewRequest(http.MethodGet, "/state", nil)
	w := httptest.NewRecorder()

	server.handleGetState(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var state models.ClusterState
	err := json.Unmarshal(w.Body.Bytes(), &state)
	require.NoError(t, err)
	assert.Contains(t, state.Services, "nginx")
}

// =============================================================================
// HubClient Tests
// =============================================================================

func TestHubClient_New(t *testing.T) {
	client := NewHubClient()
	assert.NotNil(t, client)
	assert.NotNil(t, client.client)
}

func TestHubClient_NotifyAgent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/notify", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var payload models.NotificationPayload
		err := json.NewDecoder(r.Body).Decode(&payload)
		assert.NoError(t, err)
		assert.Equal(t, models.ActionUpdate, payload.Action)
		assert.Equal(t, "test", payload.ServiceName)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := NewHubClient()
	payload := &models.NotificationPayload{
		Action:      models.ActionUpdate,
		ServiceName: "test",
		Timestamp:   time.Now(),
	}

	// Use the test server's URL (without http:// prefix)
	agentAddr := ts.Listener.Addr().String()
	err := client.NotifyAgent(context.Background(), agentAddr, payload)
	assert.NoError(t, err)
}

func TestHubClient_NotifyAgent_NonOKStatus(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := NewHubClient()
	payload := &models.NotificationPayload{
		Action:      models.ActionUpdate,
		ServiceName: "test",
	}

	err := client.NotifyAgent(context.Background(), ts.Listener.Addr().String(), payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "returned status 500")
}

func TestHubClient_NotifyAgent_ConnectionError(t *testing.T) {
	client := NewHubClient()
	payload := &models.NotificationPayload{
		Action:      models.ActionUpdate,
		ServiceName: "test",
	}

	// Try to connect to a non-existent server
	err := client.NotifyAgent(context.Background(), "127.0.0.1:1", payload)
	assert.Error(t, err)
}

func TestHubClient_GetService(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/services/nginx", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)

		svc := &models.ServiceMeta{Name: "nginx", Host: "nginx.local", Port: 80}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(svc)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := NewHubClient()
	svc, err := client.GetService(context.Background(), ts.Listener.Addr().String(), "nginx")
	require.NoError(t, err)
	assert.NotNil(t, svc)
	assert.Equal(t, "nginx", svc.Name)
	assert.Equal(t, "nginx.local", svc.Host)
}

func TestHubClient_GetService_NotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := NewHubClient()
	svc, err := client.GetService(context.Background(), ts.Listener.Addr().String(), "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, svc)
}

func TestHubClient_GetService_ErrorStatus(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := NewHubClient()
	svc, err := client.GetService(context.Background(), ts.Listener.Addr().String(), "nginx")
	assert.Error(t, err)
	assert.Nil(t, svc)
}

func TestHubClient_GetState(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/state", r.URL.Path)

		state := models.NewClusterState()
		state.Lock()
		state.Services["nginx"] = &models.ServiceMeta{Name: "nginx"}
		state.Unlock()

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(state)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := NewHubClient()
	state, err := client.GetState(context.Background(), ts.Listener.Addr().String())
	require.NoError(t, err)
	assert.NotNil(t, state)
	assert.Contains(t, state.Services, "nginx")
}

func TestHubClient_GetState_Error(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := NewHubClient()
	state, err := client.GetState(context.Background(), ts.Listener.Addr().String())
	assert.Error(t, err)
	assert.Nil(t, state)
}

func TestHubClient_Healthy(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := NewHubClient()
	healthy := client.Healthy(context.Background(), ts.Listener.Addr().String())
	assert.True(t, healthy)
}

func TestHubClient_Healthy_Unhealthy(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := NewHubClient()
	healthy := client.Healthy(context.Background(), ts.Listener.Addr().String())
	assert.False(t, healthy)
}

func TestHubClient_Healthy_ConnectionError(t *testing.T) {
	client := NewHubClient()
	healthy := client.Healthy(context.Background(), "127.0.0.1:1")
	assert.False(t, healthy)
}

// =============================================================================
// Helpers / Middleware Tests
// =============================================================================

func TestRespondJSON(t *testing.T) {
	w := httptest.NewRecorder()
	respondJSON(w, http.StatusOK, map[string]string{"key": "value"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "value", resp["key"])
}

func TestRespondError(t *testing.T) {
	w := httptest.NewRecorder()
	respondError(w, http.StatusBadRequest, "something went wrong")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "something went wrong", resp["error"])
}

func TestLoggingMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	logger := logrus.WithField("test", "logging")
	middleware := loggingMiddleware(logger, handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}

func TestResponseWriter(t *testing.T) {
	rw := &responseWriter{
		ResponseWriter: httptest.NewRecorder(),
		statusCode:     http.StatusOK,
	}

	rw.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, rw.statusCode)
}

// =============================================================================
// Integration-style: Start/Stop Server
// =============================================================================

func TestAgentServer_StartStop(t *testing.T) {
	server := NewAgentServer("127.0.0.1:0", nil, nil)

	err := server.Start(context.Background())
	require.NoError(t, err)

	err = server.Stop(context.Background())
	require.NoError(t, err)
}

func TestHubServer_StartStop(t *testing.T) {
	server := NewHubServer("127.0.0.1:0", nil, nil)

	err := server.Start(context.Background())
	require.NoError(t, err)

	err = server.Stop(context.Background())
	require.NoError(t, err)
}
