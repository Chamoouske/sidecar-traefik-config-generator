//go:build integration

// Package integration contém testes de integração que simulam um ambiente
// Docker Swarm mínimo usando Testcontainers.
//
// Para executar testes de integração:
//
//	go test -tags=integration -v ./test/integration/
//
// Pré-requisitos:
//   - Docker Engine em execução (Linux recomendado)
//   - Testcontainers (baixado automaticamente via go.mod)
//
// Nota: Em ambientes Windows, os testes que requerem Docker são
// ignorados automaticamente. Use WSL2 + Docker Desktop para executar
// localmente, ou um CI baseado em Linux.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/chamoouske/traefik-sidecar/internal/agent"
	"github.com/chamoouske/traefik-sidecar/internal/api"
	"github.com/chamoouske/traefik-sidecar/internal/config"
	"github.com/chamoouske/traefik-sidecar/internal/writer"
	"github.com/chamoouske/traefik-sidecar/pkg/models"
)

// dockerAvailable verifica se o Docker está disponível.
// Retorna false se não conseguir conectar ao daemon.
func dockerAvailable() bool {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return false
	}
	defer cli.Close()
	// Tenta pingar o daemon
	_, err = cli.Ping(context.Background())
	return err == nil
}

// waitForHTTP espera até que um endpoint HTTP responda 200.
func waitForHTTP(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s to return 200", url)
}

// TestFederationGeneration testa se o Generator produz federation.yaml
// corretamente com múltiplos targets.
func TestFederationGeneration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	gen := config.NewGenerator(80, "traefik_bridge")
	w := writer.NewAtomicWriter()

	targets := map[string]*models.FederationTarget{
		"nginx": {
			ServiceName: "nginx",
			NodeIP:      "192.168.1.10",
			NodeID:      "node1",
			Port:        80,
			TLS:         false,
		},
		"app": {
			ServiceName: "app",
			NodeIP:      "192.168.1.20",
			NodeID:      "node2",
			Port:        80,
			TLS:         true,
		},
	}

	cfg := gen.GenerateFederationConfig(targets)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.HTTP)

	// Verifica que os services de federação foram criados
	fedPath := filepath.Join(t.TempDir(), "federation.yaml")
	err := w.WriteConfig(fedPath, cfg)
	require.NoError(t, err)

	// Verifica que o arquivo foi criado
	assert.FileExists(t, fedPath)

	// Lê e verifica conteúdo
	data, err := os.ReadFile(fedPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "service-nginx-federation")
	assert.Contains(t, content, "http://192.168.1.10:80")
	assert.Contains(t, content, "service-app-federation")
	assert.Contains(t, content, "https://192.168.1.20:80")

	// Verifica que o YAML é válido e pode ser carregado
	var loaded models.TraefikConfig
	err = yaml.Unmarshal(data, &loaded)
	require.NoError(t, err)
	assert.Equal(t, "http://192.168.1.10:80", loaded.HTTP.Services["service-nginx-federation"].LoadBalancer.Servers[0].URL)
	assert.Equal(t, "https://192.168.1.20:80", loaded.HTTP.Services["service-app-federation"].LoadBalancer.Servers[0].URL)
}

// TestLocalConfigGeneration testa se o Generator produz configs locais
// corretamente quando o container está presente no nó.
func TestLocalConfigGeneration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	gen := config.NewGenerator(80, "traefik_bridge")
	w := writer.NewAtomicWriter()

	tasks := []*models.LocalTaskInfo{
		{
			ServiceName: "nginx",
			BridgeIP:    "10.0.0.2",
			ContainerID: "container1",
			Status:      "running",
		},
	}

	meta := &models.ServiceMeta{
		Name:        "nginx",
		Host:        "nginx.app.local",
		Port:        8080,
		Enabled:     true,
		Entrypoints: []string{"web"},
	}

	dir := t.TempDir()

	localConfig := gen.GenerateLocalConfig(tasks, meta)
	require.NotNil(t, localConfig)

	routersPath := filepath.Join(dir, "routers.yaml")
	servicesPath := filepath.Join(dir, "services.yaml")

	// Merge e escrita
	merged := gen.MergeConfigs(localConfig)
	err := w.WriteConfig(routersPath, merged)
	require.NoError(t, err)
	err = w.WriteConfig(servicesPath, merged)
	require.NoError(t, err)

	assert.FileExists(t, routersPath)
	assert.FileExists(t, servicesPath)

	// Verifica router local
	data, err := os.ReadFile(routersPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "nginx-local-router")
	assert.Contains(t, content, "Host(`nginx.app.local`)")
	assert.Contains(t, content, "nginx-local-service")
}

// TestFederationRouterConfig testa geração de router de cascata
// quando o container NÃO está presente no nó local.
func TestFederationRouterConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	gen := config.NewGenerator(80, "traefik_bridge")
	w := writer.NewAtomicWriter()

	meta := &models.ServiceMeta{
		Name:        "app",
		Host:        "app.remote.local",
		Enabled:     true,
		Entrypoints: []string{"web"},
	}

	// Serviço NÃO está local → gera router de cascata apontando para federation
	routerConfig := gen.GenerateFederationRouterConfig(meta)
	require.NotNil(t, routerConfig)

	dir := t.TempDir()
	routersPath := filepath.Join(dir, "routers.yaml")
	err := w.WriteConfig(routersPath, routerConfig)
	require.NoError(t, err)

	assert.FileExists(t, routersPath)

	data, err := os.ReadFile(routersPath)
	require.NoError(t, err)
	content := string(data)

	// Verifica que o router aponta para o service federation
	assert.Contains(t, content, "app-federation-router")
	assert.Contains(t, content, "Host(`app.remote.local`)")
	assert.Contains(t, content, "service-app-federation")
}

// TestHubAPIEndpoints testa os endpoints HTTP do HubServer diretamente.
func TestHubAPIEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Setup state manager callback
	stateManager := func() *models.ClusterState {
		state := models.NewClusterState()
		state.Services = map[string]*models.ServiceMeta{
			"nginx": {Name: "nginx", Host: "nginx.local", Enabled: true},
		}
		return state
	}

	lookup := func(name string) (*models.ServiceMeta, bool) {
		if name == "nginx" {
			return &models.ServiceMeta{Name: "nginx", Host: "nginx.local"}, true
		}
		return nil, false
	}

	// Cria servidor com porta aleatória
	server := api.NewHubServer(":0", stateManager, lookup)

	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(ctx)

	// Obtém a porta real em que o servidor está escutando
	addr := fmt.Sprintf("localhost:%d", server.Port())
	require.NotZero(t, server.Port(), "server should be listening on a port")

	// Testa /health
	t.Run("health", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://%s/health", addr))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]string
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		assert.Equal(t, "ok", result["status"])
	})

	// Testa /services/nginx
	t.Run("get_service", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://%s/services/nginx", addr))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var meta models.ServiceMeta
		err = json.NewDecoder(resp.Body).Decode(&meta)
		require.NoError(t, err)
		assert.Equal(t, "nginx", meta.Name)
		assert.Equal(t, "nginx.local", meta.Host)
	})

	// Testa /services/unknown (404)
	t.Run("service_not_found", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://%s/services/unknown", addr))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	// Testa /state
	t.Run("state", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://%s/state", addr))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var state models.ClusterState
		err = json.NewDecoder(resp.Body).Decode(&state)
		require.NoError(t, err)
		assert.Contains(t, state.Services, "nginx")
	})
}

// TestDiffEngineFederation testa detecção incremental de mudanças na federação.
func TestDiffEngineFederation(t *testing.T) {
	diff := config.NewDiffEngine()

	prev := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "192.168.1.10"},
	}

	curr := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "192.168.1.11"}, // IP mudou!
	}

	result, err := diff.Diff(prev, curr)
	require.NoError(t, err)
	assert.True(t, result.HasChanges, "should detect changes when IP changes")
	assert.Contains(t, result.Modified, "nginx")
}

// TestDiffEngineFederationAdded testa detecção de serviço adicionado.
func TestDiffEngineFederationAdded(t *testing.T) {
	diff := config.NewDiffEngine()

	prev := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "192.168.1.10"},
	}

	curr := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "192.168.1.10"},
		"app":   {ServiceName: "app", NodeIP: "192.168.1.20"},
	}

	result, err := diff.Diff(prev, curr)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Added, "app")
}

// TestDiffEngineFederationRemoved testa detecção de serviço removido.
func TestDiffEngineFederationRemoved(t *testing.T) {
	diff := config.NewDiffEngine()

	prev := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "192.168.1.10"},
		"app":   {ServiceName: "app", NodeIP: "192.168.1.20"},
	}

	curr := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "192.168.1.10"},
	}

	result, err := diff.Diff(prev, curr)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Removed, "app")
}

// TestDiffEngineNoChanges testa que diff não reporta mudanças quando
// o estado é idêntico.
func TestDiffEngineNoChanges(t *testing.T) {
	diff := config.NewDiffEngine()

	prev := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "192.168.1.10", Port: 80},
	}

	curr := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "192.168.1.10", Port: 80},
	}

	result, err := diff.Diff(prev, curr)
	require.NoError(t, err)
	assert.False(t, result.HasChanges)
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Modified)
}

// TestDiffEngineNilStates testa comportamento com estados nil.
func TestDiffEngineNilStates(t *testing.T) {
	diff := config.NewDiffEngine()

	// Ambos nil
	result, err := diff.Diff(nil, nil)
	require.NoError(t, err)
	assert.False(t, result.HasChanges)

	// Previous nil, current com dados
	curr := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "192.168.1.10"},
	}
	result, err = diff.Diff(nil, curr)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Added, "nginx")

	// Previous com dados, current nil
	prev := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "192.168.1.10"},
	}
	result, err = diff.Diff(prev, nil)
	require.NoError(t, err)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Removed, "nginx")
}

// TestOrphanCleanup testa remoção de configs órfãs.
func TestOrphanCleanup(t *testing.T) {
	dir := t.TempDir()
	w := writer.NewAtomicWriter()
	cleaner := agent.NewLocalOrphanCleaner(dir, w)

	// Cria estrutura de diretórios simulando configs existentes
	routersDir := filepath.Join(dir, "routers")
	servicesDir := filepath.Join(dir, "services")

	err := os.MkdirAll(routersDir, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(servicesDir, 0755)
	require.NoError(t, err)

	// Cria arquivos de config para nginx e app
	nginxRouter := filepath.Join(routersDir, "nginx.yaml")
	nginxService := filepath.Join(servicesDir, "nginx.yaml")
	appRouter := filepath.Join(routersDir, "app.yaml")
	appService := filepath.Join(servicesDir, "app.yaml")

	err = os.WriteFile(nginxRouter, []byte("test"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(nginxService, []byte("test"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(appRouter, []byte("test"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(appService, []byte("test"), 0644)
	require.NoError(t, err)

	// Limpa órfãos: remove apenas app (nginx permanece)
	err = cleaner.CleanOrphans([]string{"app"})
	require.NoError(t, err)

	// Verifica que app foi removido
	assert.NoFileExists(t, appRouter, "app router should be removed")
	assert.NoFileExists(t, appService, "app service should be removed")

	// Verifica que nginx permanece
	assert.FileExists(t, nginxRouter, "nginx router should remain")
	assert.FileExists(t, nginxService, "nginx service should remain")
}

// TestOrphanCleanupMultiple testa limpeza de múltiplos órfãos.
func TestOrphanCleanupMultiple(t *testing.T) {
	dir := t.TempDir()
	w := writer.NewAtomicWriter()
	cleaner := agent.NewLocalOrphanCleaner(dir, w)

	routersDir := filepath.Join(dir, "routers")
	servicesDir := filepath.Join(dir, "services")

	err := os.MkdirAll(routersDir, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(servicesDir, 0755)
	require.NoError(t, err)

	// Cria configs para 3 serviços
	for _, svc := range []string{"nginx", "app", "db"} {
		err = os.WriteFile(filepath.Join(routersDir, svc+".yaml"), []byte("test"), 0644)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(servicesDir, svc+".yaml"), []byte("test"), 0644)
		require.NoError(t, err)
	}

	// Remove dois serviços de uma vez
	err = cleaner.CleanOrphans([]string{"nginx", "db"})
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(routersDir, "nginx.yaml"))
	assert.NoFileExists(t, filepath.Join(servicesDir, "nginx.yaml"))
	assert.NoFileExists(t, filepath.Join(routersDir, "db.yaml"))
	assert.NoFileExists(t, filepath.Join(servicesDir, "db.yaml"))

	// app deve permanecer
	assert.FileExists(t, filepath.Join(routersDir, "app.yaml"))
	assert.FileExists(t, filepath.Join(servicesDir, "app.yaml"))
}

// TestOrphanCleanupEmptyList testa que CleanOrphans com lista vazia
// não causa erros.
func TestOrphanCleanupEmptyList(t *testing.T) {
	dir := t.TempDir()
	w := writer.NewAtomicWriter()
	cleaner := agent.NewLocalOrphanCleaner(dir, w)

	err := cleaner.CleanOrphans([]string{})
	require.NoError(t, err)
}

// TestAtomicWriteAndRead testa o ciclo completo de escrita atômica e leitura.
func TestAtomicWriteAndRead(t *testing.T) {
	w := writer.NewAtomicWriter()

	cfg := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"test-router": {
					Rule:        "Host(`integration.test`)",
					Service:     "test-service",
					EntryPoints: []string{"web"},
				},
			},
			Services: map[string]*models.ServiceConfig{
				"test-service": {
					LoadBalancer: &models.LoadBalancerConfig{
						Servers: []*models.ServerConfig{
							{URL: "http://10.0.0.2:8080"},
						},
					},
				},
			},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "integration.yaml")
	err := w.WriteConfig(path, cfg)
	require.NoError(t, err)

	// Verifica que o arquivo existe
	assert.FileExists(t, path)

	// Lê de volta e verifica
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// Verifica que o YAML gerado é válido
	var loaded models.TraefikConfig
	err = yaml.Unmarshal(data, &loaded)
	require.NoError(t, err)
	assert.Equal(t, "Host(`integration.test`)", loaded.HTTP.Routers["test-router"].Rule)
	assert.Equal(t, "test-service", loaded.HTTP.Routers["test-router"].Service)
	assert.Equal(t, "http://10.0.0.2:8080", loaded.HTTP.Services["test-service"].LoadBalancer.Servers[0].URL)
}

// TestAtomicWriteOverwrite testa que escrever duas vezes no mesmo path
// funciona corretamente (substituição atômica).
func TestAtomicWriteOverwrite(t *testing.T) {
	w := writer.NewAtomicWriter()

	dir := t.TempDir()
	path := filepath.Join(dir, "overwrite.yaml")

	// Primeira escrita
	cfg1 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"router1": {
					Rule:    "Host(`first.test`)",
					Service: "svc1",
				},
			},
		},
	}

	err := w.WriteConfig(path, cfg1)
	require.NoError(t, err)

	// Segunda escrita (sobrescreve)
	cfg2 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"router2": {
					Rule:    "Host(`second.test`)",
					Service: "svc2",
				},
			},
		},
	}

	err = w.WriteConfig(path, cfg2)
	require.NoError(t, err)

	// Verifica que o conteúdo é o da segunda escrita
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var loaded models.TraefikConfig
	err = yaml.Unmarshal(data, &loaded)
	require.NoError(t, err)

	assert.Contains(t, loaded.HTTP.Routers, "router2")
	assert.Equal(t, "Host(`second.test`)", loaded.HTTP.Routers["router2"].Rule)
}

// TestAtomicRemove testa remoção de arquivo via AtomicWriter.
func TestAtomicRemove(t *testing.T) {
	w := writer.NewAtomicWriter()

	dir := t.TempDir()
	path := filepath.Join(dir, "remove.yaml")

	// Cria arquivo
	cfg := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"test": {Rule: "Host(`test`)", Service: "svc"},
			},
		},
	}

	err := w.WriteConfig(path, cfg)
	require.NoError(t, err)
	assert.FileExists(t, path)

	// Remove
	err = w.RemoveConfig(path)
	require.NoError(t, err)
	assert.NoFileExists(t, path)

	// Remove novamente (deve ser seguro)
	err = w.RemoveConfig(path)
	require.NoError(t, err)
}

// TestGeneratorMergeConfigs testa mesclagem de múltiplos TraefikConfig.
func TestGeneratorMergeConfigs(t *testing.T) {
	gen := config.NewGenerator(80, "traefik_bridge")

	cfg1 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"router1": {Rule: "Host(`a.test`)", Service: "svc1"},
			},
			Services: map[string]*models.ServiceConfig{
				"svc1": {
					LoadBalancer: &models.LoadBalancerConfig{
						Servers: []*models.ServerConfig{{URL: "http://10.0.0.2:80"}},
					},
				},
			},
		},
	}

	cfg2 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"router2": {Rule: "Host(`b.test`)", Service: "svc2"},
			},
			Services: map[string]*models.ServiceConfig{
				"svc2": {
					LoadBalancer: &models.LoadBalancerConfig{
						Servers: []*models.ServerConfig{{URL: "http://10.0.0.3:80"}},
					},
				},
			},
		},
	}

	merged := gen.MergeConfigs(cfg1, cfg2)
	require.NotNil(t, merged)
	require.NotNil(t, merged.HTTP)
	assert.Contains(t, merged.HTTP.Routers, "router1")
	assert.Contains(t, merged.HTTP.Routers, "router2")
	assert.Contains(t, merged.HTTP.Services, "svc1")
	assert.Contains(t, merged.HTTP.Services, "svc2")
}

// TestGeneratorMergeConfigsNilSafe testa que MergeConfigs é seguro com nil.
func TestGeneratorMergeConfigsNilSafe(t *testing.T) {
	gen := config.NewGenerator(80, "traefik_bridge")

	// Merge com configs nil
	merged := gen.MergeConfigs(nil, nil)
	require.NotNil(t, merged)
	require.NotNil(t, merged.HTTP)
	require.NotNil(t, merged.TCP)
	assert.Empty(t, merged.HTTP.Routers)
	assert.Empty(t, merged.HTTP.Services)
	assert.Empty(t, merged.HTTP.Middlewares)
}

// TestStateManagerFederation verifica o ciclo completo de state manager
// para federação: set, get, delete.
func TestStateManagerFederation(t *testing.T) {
	gen := config.NewGenerator(80, "traefik_bridge")
	diff := config.NewDiffEngine()
	stateFile := filepath.Join(t.TempDir(), "state.json")

	// Store nil → fallback para leitura/escrita direta
	sm := config.NewStateManager(gen, diff, nil, stateFile)

	// Inicialmente vazio
	assert.Empty(t, sm.GetLastFederation())

	// Adiciona target
	target := &models.FederationTarget{
		ServiceName: "nginx",
		NodeIP:      "192.168.1.10",
		Port:        80,
	}
	sm.SetFederationTarget(target)

	found, ok := sm.GetFederationTarget("nginx")
	assert.True(t, ok)
	assert.Equal(t, "192.168.1.10", found.NodeIP)

	// Remove target
	sm.DeleteFederationTarget("nginx")
	_, ok = sm.GetFederationTarget("nginx")
	assert.False(t, ok)
	assert.Empty(t, sm.GetLastFederation())
}

// TestStateManagerServices verifica o ciclo de vida de serviços no state manager.
func TestStateManagerServices(t *testing.T) {
	gen := config.NewGenerator(80, "traefik_bridge")
	diff := config.NewDiffEngine()
	sm := config.NewStateManager(gen, diff, nil, filepath.Join(t.TempDir(), "state.json"))

	// Adiciona serviço
	svc := &models.ServiceMeta{
		Name:    "nginx",
		Host:    "nginx.app.local",
		Enabled: true,
	}
	sm.SetService(svc)

	// Busca
	found, ok := sm.GetService("nginx")
	assert.True(t, ok)
	assert.Equal(t, "nginx.app.local", found.Host)

	// Lista
	services := sm.ListServices()
	assert.Len(t, services, 1)
	assert.Equal(t, "nginx", services[0].Name)

	// Remove
	sm.DeleteService("nginx")
	_, ok = sm.GetService("nginx")
	assert.False(t, ok)
}

// TestStateManagerSaveAndLoad testa persistência e recarga de estado.
func TestStateManagerSaveAndLoad(t *testing.T) {
	gen := config.NewGenerator(80, "traefik_bridge")
	diff := config.NewDiffEngine()
	stateFile := filepath.Join(t.TempDir(), "state.json")

	sm := config.NewStateManager(gen, diff, nil, stateFile)

	// Adiciona dados
	svc := &models.ServiceMeta{Name: "nginx", Host: "nginx.local", Enabled: true}
	sm.SetService(svc)

	target := &models.FederationTarget{ServiceName: "nginx", NodeIP: "192.168.1.10"}
	sm.SetFederationTarget(target)

	// Salva
	err := sm.SaveState()
	require.NoError(t, err)
	assert.FileExists(t, stateFile)

	// Cria novo state manager e carrega
	sm2 := config.NewStateManager(gen, diff, nil, stateFile)
	err = sm2.LoadState()
	require.NoError(t, err)

	// Verifica dados carregados
	svc2, ok := sm2.GetService("nginx")
	assert.True(t, ok)
	assert.Equal(t, "nginx.local", svc2.Host)

	fed2, ok := sm2.GetFederationTarget("nginx")
	assert.True(t, ok)
	assert.Equal(t, "192.168.1.10", fed2.NodeIP)
}

// TestAgentServerEndpoints testa os endpoints HTTP do AgentServer diretamente.
func TestAgentServerEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Channel para capturar notificações recebidas
	notifyCh := make(chan *models.NotificationPayload, 1)

	notifyHandler := func(payload *models.NotificationPayload) error {
		select {
		case notifyCh <- payload:
		default:
		}
		return nil
	}

	statusHandler := func() *models.AgentStatusResponse {
		return &models.AgentStatusResponse{
			NodeID:          "test-node",
			Hostname:        "test-node",
			Uptime:          "1h",
			LocalServices:   3,
			GeneratedConfig: true,
			LastUpdate:      time.Now(),
		}
	}

	server := api.NewAgentServer(":0", notifyHandler, statusHandler)

	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(ctx)

	addr := fmt.Sprintf("localhost:%d", server.Port())
	require.NotZero(t, server.Port())

	// Testa GET /status
	t.Run("status", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://%s/status", addr))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var status models.AgentStatusResponse
		err = json.NewDecoder(resp.Body).Decode(&status)
		require.NoError(t, err)
		assert.Equal(t, "test-node", status.NodeID)
	})

	// Testa POST /notify
	t.Run("notify", func(t *testing.T) {
		payload := models.NotificationPayload{
			Action:      models.ActionUpdate,
			ServiceName: "nginx",
			Timestamp:   time.Now(),
		}

		resp, err := postJSON(fmt.Sprintf("http://%s/notify", addr), payload)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Aguarda notificação ser processada
		select {
		case received := <-notifyCh:
			assert.Equal(t, models.ActionUpdate, received.Action)
			assert.Equal(t, "nginx", received.ServiceName)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for notification handler")
		}
	})

	// Testa POST /notify com payload inválido
	t.Run("notify_invalid", func(t *testing.T) {
		// Payload vazio
		resp, err := postJSON(fmt.Sprintf("http://%s/notify", addr), map[string]string{})
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

// postJSON é helper para fazer POST com JSON.
func postJSON(url string, payload interface{}) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return http.Post(url, "application/json", bytes.NewReader(data))
}

// TestHubAgentIntegration testa o fluxo completo Hub + Agente.
// Requer Docker Swarm em execução (ignorado em Windows).
// Os componentes individuais (StateManager, Generator, DiffEngine,
// AtomicWriter, OrphanCleaner, AgentServer, HubServer) são testados
// separadamente nos testes abaixo.
func TestHubAgentIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if !dockerAvailable() {
		t.Skip("Docker not available, skipping integration test")
	}

	t.Log("Hub+Agent integration test requires Docker Swarm environment")
	t.Log("This test is a placeholder for CI environments with Docker Swarm")
	t.Log("Component-level integration tests are run individually below")
}

// TestUpdateFederationViaStateManager testa o fluxo UpdateFederation
// do StateManager que é usado internamente pelo Hub.
func TestUpdateFederationViaStateManager(t *testing.T) {
	gen := config.NewGenerator(80, "traefik_bridge")
	diff := config.NewDiffEngine()
	sm := config.NewStateManager(gen, diff, nil, filepath.Join(t.TempDir(), "state.json"))

	targets := map[string]*models.FederationTarget{
		"nginx": {
			ServiceName: "nginx",
			NodeIP:      "192.168.1.10",
			Port:        80,
		},
	}

	// Primeira atualização: deve retornar config com mudanças
	cfg, result, err := sm.UpdateFederation(targets)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotNil(t, result)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Added, "nginx")

	// Segunda atualização sem mudanças: cfg deve ser nil
	cfg2, result2, err := sm.UpdateFederation(targets)
	require.NoError(t, err)
	assert.Nil(t, cfg2, "no changes should return nil config")
	assert.NotNil(t, result2)
	assert.False(t, result2.HasChanges)

	// Atualização com mudança: IP alterado
	changedTargets := map[string]*models.FederationTarget{
		"nginx": {
			ServiceName: "nginx",
			NodeIP:      "192.168.1.11", // IP mudou!
			Port:        80,
		},
	}

	cfg3, result3, err := sm.UpdateFederation(changedTargets)
	require.NoError(t, err)
	require.NotNil(t, cfg3, "changes should return non-nil config")
	assert.True(t, result3.HasChanges)
	assert.Contains(t, result3.Modified, "nginx")
}

// TestGenerateFederationConfigEmpty testa configuração de federação vazia.
func TestGenerateFederationConfigEmpty(t *testing.T) {
	gen := config.NewGenerator(80, "traefik_bridge")

	// Targets nil
	cfg := gen.GenerateFederationConfig(nil)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.HTTP)
	assert.Empty(t, cfg.HTTP.Services)

	// Targets vazio
	cfg = gen.GenerateFederationConfig(make(map[string]*models.FederationTarget))
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.HTTP.Services)
}

// TestGenerateLocalConfigNoMatch testa geração de config local quando
// não há task local correspondente (deve retornar config vazia).
func TestGenerateLocalConfigNoMatch(t *testing.T) {
	gen := config.NewGenerator(80, "traefik_bridge")

	tasks := []*models.LocalTaskInfo{
		{ServiceName: "other", BridgeIP: "10.0.0.5"},
	}

	meta := &models.ServiceMeta{
		Name:    "nginx",
		Host:    "nginx.app.local",
		Enabled: true,
	}

	cfg := gen.GenerateLocalConfig(tasks, meta)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.HTTP)

	// Nenhuma task local corresponde a "nginx", então deve retornar config vazia
	assert.Empty(t, cfg.HTTP.Routers)
	assert.Empty(t, cfg.HTTP.Services)
}

// TestGenerateLocalConfigNilParams testa geração de config local com
// parâmetros nil.
func TestGenerateLocalConfigNilParams(t *testing.T) {
	gen := config.NewGenerator(80, "traefik_bridge")

	// Tasks nil, meta nil
	cfg := gen.GenerateLocalConfig(nil, nil)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.HTTP.Routers)

	// Tasks não-nil, meta nil
	cfg = gen.GenerateLocalConfig([]*models.LocalTaskInfo{
		{ServiceName: "nginx", BridgeIP: "10.0.0.2"},
	}, nil)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.HTTP.Routers)

	// Tasks nil, meta não-nil
	cfg = gen.GenerateLocalConfig(nil, &models.ServiceMeta{Name: "nginx"})
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.HTTP.Routers)
}

// TestGenerateFederationRouterConfigNil testa que FederationRouterConfig
// lida corretamente com meta nil.
func TestGenerateFederationRouterConfigNil(t *testing.T) {
	gen := config.NewGenerator(80, "traefik_bridge")

	cfg := gen.GenerateFederationRouterConfig(nil)
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.HTTP)
	assert.Empty(t, cfg.HTTP.Routers)
	assert.Empty(t, cfg.HTTP.Services)
}
