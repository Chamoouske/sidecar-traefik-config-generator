package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// =============================================================================
// DiffEngine Tests
// =============================================================================

func TestDiffEngine_Diff_NoChanges(t *testing.T) {
	e := NewDiffEngine()
	prev := map[string]*models.ServiceMeta{"svc1": {Name: "svc1", Port: 80}}
	curr := map[string]*models.ServiceMeta{"svc1": {Name: "svc1", Port: 80}}

	result, err := e.Diff(prev, curr)
	assert.NoError(t, err)
	assert.False(t, result.HasChanges)
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Modified)
}

func TestDiffEngine_Diff_Added(t *testing.T) {
	e := NewDiffEngine()
	prev := map[string]*models.ServiceMeta{}
	curr := map[string]*models.ServiceMeta{"svc1": {Name: "svc1"}}

	result, err := e.Diff(prev, curr)
	assert.NoError(t, err)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Added, "svc1")
	assert.Empty(t, result.Removed)
}

func TestDiffEngine_Diff_Removed(t *testing.T) {
	e := NewDiffEngine()
	prev := map[string]*models.ServiceMeta{"svc1": {Name: "svc1"}}
	curr := map[string]*models.ServiceMeta{}

	result, err := e.Diff(prev, curr)
	assert.NoError(t, err)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Removed, "svc1")
	assert.Empty(t, result.Added)
}

func TestDiffEngine_Diff_Modified(t *testing.T) {
	e := NewDiffEngine()
	prev := map[string]*models.ServiceMeta{"svc1": {Name: "svc1", Port: 80}}
	curr := map[string]*models.ServiceMeta{"svc1": {Name: "svc1", Port: 8080}}

	result, err := e.Diff(prev, curr)
	assert.NoError(t, err)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Modified, "svc1")
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
}

func TestDiffEngine_Diff_AddedAndRemoved(t *testing.T) {
	e := NewDiffEngine()
	prev := map[string]*models.ServiceMeta{
		"svc1": {Name: "svc1"},
		"svc2": {Name: "svc2"},
	}
	curr := map[string]*models.ServiceMeta{
		"svc2": {Name: "svc2"},
		"svc3": {Name: "svc3"},
	}

	result, err := e.Diff(prev, curr)
	assert.NoError(t, err)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Added, "svc3")
	assert.Contains(t, result.Removed, "svc1")
	assert.NotContains(t, result.Added, "svc2")
	assert.NotContains(t, result.Removed, "svc2")
}

func TestDiffEngine_Diff_FederationTargets(t *testing.T) {
	e := NewDiffEngine()
	prev := map[string]*models.FederationTarget{}
	curr := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "10.0.0.1", Port: 80},
	}

	result, err := e.Diff(prev, curr)
	assert.NoError(t, err)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Added, "nginx")
}

func TestDiffEngine_Diff_BothNil(t *testing.T) {
	e := NewDiffEngine()
	result, err := e.Diff(nil, nil)
	assert.NoError(t, err)
	assert.False(t, result.HasChanges)
}

func TestDiffEngine_Diff_UnsupportedType(t *testing.T) {
	e := NewDiffEngine()
	result, err := e.Diff("string1", "string2")
	assert.NoError(t, err)
	assert.True(t, result.HasChanges)
}

func TestDiffEngine_HasChanged_True(t *testing.T) {
	e := NewDiffEngine()
	assert.True(t, e.HasChanged(
		&models.ServiceMeta{Name: "svc1", Port: 80},
		&models.ServiceMeta{Name: "svc1", Port: 8080},
	))
}

func TestDiffEngine_HasChanged_False(t *testing.T) {
	e := NewDiffEngine()
	assert.False(t, e.HasChanged(
		&models.ServiceMeta{Name: "svc1", Port: 80},
		&models.ServiceMeta{Name: "svc1", Port: 80},
	))
}

func TestDiffEngine_HasChanged_Nil(t *testing.T) {
	e := NewDiffEngine()
	assert.False(t, e.HasChanged(nil, nil))
	assert.True(t, e.HasChanged(nil, &models.ServiceMeta{}))
	assert.True(t, e.HasChanged(&models.ServiceMeta{}, nil))
}

func TestDiffEngine_CompareServices_Exported(t *testing.T) {
	e := NewDiffEngine()
	prev := map[string]*models.ServiceMeta{
		"svc1": {Name: "svc1", Host: "a.com"},
		"svc2": {Name: "svc2", Host: "b.com"},
	}
	curr := map[string]*models.ServiceMeta{
		"svc1": {Name: "svc1", Host: "a.com"},
		"svc2": {Name: "svc2", Host: "changed.com"},
	}

	result := e.compareServices(prev, curr)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Modified, "svc2")
	assert.NotContains(t, result.Modified, "svc1")
}

func TestDiffEngine_CompareFederations_Exported(t *testing.T) {
	e := NewDiffEngine()
	prev := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "10.0.0.1"},
	}
	curr := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "10.0.0.2"},
	}

	result := e.compareFederations(prev, curr)
	assert.True(t, result.HasChanges)
	assert.Contains(t, result.Modified, "nginx")
}

// =============================================================================
// Generator Tests
// =============================================================================

func TestGenerator_NewGenerator(t *testing.T) {
	g := NewGenerator(0, "traefik_bridge")
	assert.NotNil(t, g)
	assert.Equal(t, 80, g.traefikPort)
	assert.Equal(t, "traefik_bridge", g.bridgeName)
	assert.True(t, g.passHostHeader)

	g2 := NewGenerator(9090, "custom_bridge")
	assert.Equal(t, 9090, g2.traefikPort)
}

func TestGenerator_GenerateFederationConfig(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	targets := map[string]*models.FederationTarget{
		"nginx": {
			ServiceName: "nginx",
			NodeIP:      "192.168.1.10",
			NodeID:      "node1",
			Port:        80,
			TLS:         false,
		},
	}

	config := g.GenerateFederationConfig(targets)
	assert.NotNil(t, config)
	assert.NotNil(t, config.HTTP)

	svcName := models.FederationServiceName("nginx")
	svc, ok := config.HTTP.Services[svcName]
	assert.True(t, ok)
	assert.NotNil(t, svc.LoadBalancer)
	assert.Len(t, svc.LoadBalancer.Servers, 1)
	assert.Equal(t, "http://192.168.1.10:80", svc.LoadBalancer.Servers[0].URL)
	assert.True(t, *svc.LoadBalancer.PassHostHeader)
}

func TestGenerator_GenerateFederationConfig_TLS(t *testing.T) {
	g := NewGenerator(443, "traefik_bridge")

	targets := map[string]*models.FederationTarget{
		"nginx": {
			ServiceName: "nginx",
			NodeIP:      "10.0.0.1",
			Port:        443,
			TLS:         true,
		},
	}

	config := g.GenerateFederationConfig(targets)
	svcName := models.FederationServiceName("nginx")
	svc := config.HTTP.Services[svcName]
	assert.Equal(t, "https://10.0.0.1:443", svc.LoadBalancer.Servers[0].URL)
}

func TestGenerator_GenerateFederationConfig_NilTargets(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	config := g.GenerateFederationConfig(nil)
	assert.NotNil(t, config)
	assert.Empty(t, config.HTTP.Services)
}

func TestGenerator_GenerateFederationConfig_EmptyTargets(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	config := g.GenerateFederationConfig(map[string]*models.FederationTarget{})
	assert.NotNil(t, config)
	assert.Empty(t, config.HTTP.Services)
}

func TestGenerator_GenerateFederationConfig_NilTargetValue(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	targets := map[string]*models.FederationTarget{
		"nginx": nil,
	}
	config := g.GenerateFederationConfig(targets)
	assert.Empty(t, config.HTTP.Services)
}

func TestGenerator_GenerateLocalConfig(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	tasks := []*models.LocalTaskInfo{
		{
			ServiceName: "nginx",
			BridgeIP:    "10.0.0.2",
			ContainerID: "abc123",
		},
	}

	meta := &models.ServiceMeta{
		Name:        "nginx",
		Host:        "nginx.app.local",
		Port:        8080,
		Entrypoints: []string{"web"},
	}

	config := g.GenerateLocalConfig(tasks, meta)
	assert.NotNil(t, config)
	assert.NotNil(t, config.HTTP)

	// Verifica service local
	svcName := models.LocalServiceName("nginx")
	svc, ok := config.HTTP.Services[svcName]
	assert.True(t, ok)
	assert.Equal(t, "http://10.0.0.2:8080", svc.LoadBalancer.Servers[0].URL)

	// Verifica router local
	routerName := models.LocalRouterName("nginx")
	router, ok := config.HTTP.Routers[routerName]
	assert.True(t, ok)
	assert.Contains(t, router.Rule, "nginx.app.local")
	assert.Equal(t, svcName, router.Service)
	assert.Equal(t, []string{"web"}, router.EntryPoints)
}

func TestGenerator_GenerateLocalConfig_DefaultPort(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	tasks := []*models.LocalTaskInfo{
		{
			ServiceName: "nginx",
			BridgeIP:    "10.0.0.2",
		},
	}

	meta := &models.ServiceMeta{
		Name: "nginx",
		Host: "nginx.local",
		Port: 0, // zero port - should use traefikPort
	}

	config := g.GenerateLocalConfig(tasks, meta)
	svcName := models.LocalServiceName("nginx")
	svc := config.HTTP.Services[svcName]
	assert.Equal(t, "http://10.0.0.2:80", svc.LoadBalancer.Servers[0].URL)
}

func TestGenerator_GenerateLocalConfig_DefaultEntrypoints(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	tasks := []*models.LocalTaskInfo{
		{
			ServiceName: "nginx",
			BridgeIP:    "10.0.0.2",
		},
	}

	meta := &models.ServiceMeta{
		Name: "nginx",
		Host: "nginx.local",
	}

	config := g.GenerateLocalConfig(tasks, meta)
	routerName := models.LocalRouterName("nginx")
	router := config.HTTP.Routers[routerName]
	assert.Equal(t, []string{"web"}, router.EntryPoints)
}

func TestGenerator_GenerateLocalConfig_WithMiddlewares(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	tasks := []*models.LocalTaskInfo{
		{
			ServiceName: "nginx",
			BridgeIP:    "10.0.0.2",
		},
	}

	meta := &models.ServiceMeta{
		Name:        "nginx",
		Host:        "nginx.local",
		Middlewares: []string{"cors", "ratelimit"},
	}

	config := g.GenerateLocalConfig(tasks, meta)
	routerName := models.LocalRouterName("nginx")
	router := config.HTTP.Routers[routerName]
	assert.Equal(t, []string{"cors", "ratelimit"}, router.Middlewares)
}

func TestGenerator_GenerateLocalConfig_WithTLS(t *testing.T) {
	g := NewGenerator(443, "traefik_bridge")

	tasks := []*models.LocalTaskInfo{
		{
			ServiceName: "nginx",
			BridgeIP:    "10.0.0.2",
		},
	}

	meta := &models.ServiceMeta{
		Name: "nginx",
		Host: "nginx.local",
		TLS:  true,
	}

	config := g.GenerateLocalConfig(tasks, meta)
	routerName := models.LocalRouterName("nginx")
	router := config.HTTP.Routers[routerName]
	assert.NotNil(t, router.TLS)
}

func TestGenerator_GenerateLocalConfig_NilMeta(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	config := g.GenerateLocalConfig([]*models.LocalTaskInfo{}, nil)
	assert.Empty(t, config.HTTP.Services)
	assert.Empty(t, config.HTTP.Routers)
}

func TestGenerator_GenerateLocalConfig_NilTasks(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	config := g.GenerateLocalConfig(nil, &models.ServiceMeta{Name: "nginx"})
	assert.Empty(t, config.HTTP.Services)
	assert.Empty(t, config.HTTP.Routers)
}

func TestGenerator_GenerateLocalConfig_NoMatchingTask(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	tasks := []*models.LocalTaskInfo{
		{ServiceName: "other", BridgeIP: "10.0.0.2"},
	}
	meta := &models.ServiceMeta{Name: "nginx", Host: "nginx.local"}

	config := g.GenerateLocalConfig(tasks, meta)
	assert.Empty(t, config.HTTP.Services)
	assert.Empty(t, config.HTTP.Routers)
}

func TestGenerator_GenerateFederationRouterConfig(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	meta := &models.ServiceMeta{
		Name:        "nginx",
		Host:        "nginx.app.local",
		Entrypoints: []string{"web"},
	}

	config := g.GenerateFederationRouterConfig(meta)
	assert.NotNil(t, config)

	routerName := models.FederationRouterName("nginx")
	router, ok := config.HTTP.Routers[routerName]
	assert.True(t, ok)
	assert.Contains(t, router.Rule, "nginx.app.local")
	assert.Equal(t, models.FederationServiceName("nginx"), router.Service)
	assert.Equal(t, []string{"web"}, router.EntryPoints)
}

func TestGenerator_GenerateFederationRouterConfig_DefaultEntrypoints(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	meta := &models.ServiceMeta{
		Name: "nginx",
		Host: "nginx.app.local",
	}

	config := g.GenerateFederationRouterConfig(meta)
	routerName := models.FederationRouterName("nginx")
	router := config.HTTP.Routers[routerName]
	assert.Equal(t, []string{"web"}, router.EntryPoints)
}

func TestGenerator_GenerateFederationRouterConfig_WithTLS(t *testing.T) {
	g := NewGenerator(443, "traefik_bridge")

	meta := &models.ServiceMeta{
		Name: "nginx",
		Host: "nginx.app.local",
		TLS:  true,
	}

	config := g.GenerateFederationRouterConfig(meta)
	routerName := models.FederationRouterName("nginx")
	router := config.HTTP.Routers[routerName]
	assert.NotNil(t, router.TLS)
}

func TestGenerator_GenerateFederationRouterConfig_WithMiddlewares(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	meta := &models.ServiceMeta{
		Name:        "nginx",
		Host:        "nginx.app.local",
		Middlewares: []string{"auth"},
	}

	config := g.GenerateFederationRouterConfig(meta)
	routerName := models.FederationRouterName("nginx")
	router := config.HTTP.Routers[routerName]
	assert.Equal(t, []string{"auth"}, router.Middlewares)
}

func TestGenerator_GenerateFederationRouterConfig_NilMeta(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	config := g.GenerateFederationRouterConfig(nil)
	assert.Empty(t, config.HTTP.Routers)
	assert.Empty(t, config.HTTP.Services)
}

func TestGenerator_MergeConfigs(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	c1 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"r1": {Rule: "Host(`a.com`)"},
			},
		},
	}
	c2 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"r2": {Rule: "Host(`b.com`)"},
			},
		},
	}

	merged := g.MergeConfigs(c1, c2)
	assert.Len(t, merged.HTTP.Routers, 2)
	assert.Contains(t, merged.HTTP.Routers, "r1")
	assert.Contains(t, merged.HTTP.Routers, "r2")
}

func TestGenerator_MergeConfigs_WithNil(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	c1 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"r1": {Rule: "Host(`a.com`)"},
			},
		},
	}

	merged := g.MergeConfigs(c1, nil)
	assert.Len(t, merged.HTTP.Routers, 1)
	assert.Contains(t, merged.HTTP.Routers, "r1")
}

func TestGenerator_MergeConfigs_ServicesAndMiddlewares(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	c1 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Services: map[string]*models.ServiceConfig{
				"svc1": {LoadBalancer: &models.LoadBalancerConfig{}},
			},
		},
	}
	c2 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Middlewares: map[string]*models.MiddlewareConfig{
				"mw1": {Headers: &models.HeadersConfig{}},
			},
		},
	}

	merged := g.MergeConfigs(c1, c2)
	assert.Len(t, merged.HTTP.Services, 1)
	assert.Len(t, merged.HTTP.Middlewares, 1)
}

func TestGenerator_MergeConfigs_TCP(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	c1 := &models.TraefikConfig{
		TCP: &models.TCPConfig{
			Routers: map[string]*models.TCPRouterConfig{
				"tcp1": {Rule: "HostSNI(`*`)"},
			},
		},
	}

	merged := g.MergeConfigs(c1)
	assert.Len(t, merged.TCP.Routers, 1)
	assert.Contains(t, merged.TCP.Routers, "tcp1")
}

func TestGenerator_MergeConfigs_Empty(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	merged := g.MergeConfigs()
	assert.NotNil(t, merged.HTTP)
	assert.NotNil(t, merged.TCP)
	assert.Empty(t, merged.HTTP.Routers)
}

func TestGenerator_MergeConfigs_DuplicateKeys(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	c1 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"r1": {Rule: "Host(`a.com`)"},
			},
		},
	}
	c2 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"r1": {Rule: "Host(`b.com`)"},
			},
		},
	}

	merged := g.MergeConfigs(c1, c2)
	assert.Len(t, merged.HTTP.Routers, 1)
	// Last one wins
	assert.Equal(t, "Host(`b.com`)", merged.HTTP.Routers["r1"].Rule)
}

func TestGenerator_GenerateMiddlewareConfig(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	services := map[string]*models.ServiceMeta{
		"nginx": {
			Name:        "nginx",
			Middlewares: []string{"cors"},
			Labels: map[string]string{
				"traefik.http.middlewares.cors.headers.accesscontrolallowmethods": "GET,POST",
			},
		},
	}

	config := g.GenerateMiddlewareConfig(services)
	assert.NotNil(t, config)
	assert.Len(t, config.HTTP.Middlewares, 1)
	assert.Contains(t, config.HTTP.Middlewares, "cors")
}

func TestGenerator_GenerateMiddlewareConfig_NilServices(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	config := g.GenerateMiddlewareConfig(nil)
	assert.Empty(t, config.HTTP.Middlewares)
}

func TestGenerator_GenerateMiddlewareConfig_EmptyMiddlewares(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	services := map[string]*models.ServiceMeta{
		"nginx": {Name: "nginx"},
	}

	config := g.GenerateMiddlewareConfig(services)
	assert.Empty(t, config.HTTP.Middlewares)
}

func TestGenerator_GenerateMiddlewareConfig_Duplicate(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	services := map[string]*models.ServiceMeta{
		"nginx": {
			Name:        "nginx",
			Middlewares: []string{"cors"},
			Labels: map[string]string{
				"traefik.http.middlewares.cors.headers.accesscontrolallowmethods": "GET,POST",
			},
		},
		"api": {
			Name:        "api",
			Middlewares: []string{"cors"},
			Labels: map[string]string{
				"traefik.http.middlewares.cors.headers.accesscontrolallowmethods": "GET,POST",
			},
		},
	}

	config := g.GenerateMiddlewareConfig(services)
	assert.Len(t, config.HTTP.Middlewares, 1) // cors só deve aparecer uma vez
}

func TestGenerator_GenerateMiddlewareConfig_RateLimit(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	services := map[string]*models.ServiceMeta{
		"nginx": {
			Name:        "nginx",
			Middlewares: []string{"ratelimit"},
			Labels: map[string]string{
				"traefik.http.middlewares.ratelimit.ratelimit.average": "100",
			},
		},
	}

	config := g.GenerateMiddlewareConfig(services)
	assert.Contains(t, config.HTTP.Middlewares, "ratelimit")
	assert.NotNil(t, config.HTTP.Middlewares["ratelimit"].RateLimit)
	assert.Equal(t, 100, config.HTTP.Middlewares["ratelimit"].RateLimit.Average)
}

func TestGenerator_GenerateMiddlewareConfig_Retry(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	services := map[string]*models.ServiceMeta{
		"nginx": {
			Name:        "nginx",
			Middlewares: []string{"retry"},
			Labels: map[string]string{
				"traefik.http.middlewares.retry.retry.attempts": "5",
			},
		},
	}

	config := g.GenerateMiddlewareConfig(services)
	assert.Contains(t, config.HTTP.Middlewares, "retry")
	assert.NotNil(t, config.HTTP.Middlewares["retry"].Retry)
	assert.Equal(t, 3, config.HTTP.Middlewares["retry"].Retry.Attempts)
}

func TestSplitComma(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, splitComma("a,b,c"))
	assert.Equal(t, []string{"single"}, splitComma("single"))
	assert.Nil(t, splitComma(""))
	assert.Equal(t, []string{"a"}, splitComma("a,"))
}

// =============================================================================
// Golden File Tests
// =============================================================================

func TestGenerator_FederationConfigGoldenFile(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	targets := map[string]*models.FederationTarget{
		"nginx": {
			ServiceName: "nginx",
			NodeIP:      "192.168.1.10",
			Port:        80,
		},
	}

	config := g.GenerateFederationConfig(targets)
	yamlBytes, err := yaml.Marshal(config)
	require.NoError(t, err)

	// Read golden file
	goldenPath := filepath.Join("..", "..", "test", "fixtures", "federation_expected.yaml")
	expected, err := os.ReadFile(goldenPath)
	require.NoError(t, err)

	assert.Equal(t, string(expected), string(yamlBytes))
}

func TestGenerator_LocalConfigGoldenFile(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")

	tasks := []*models.LocalTaskInfo{
		{
			ServiceName: "nginx",
			BridgeIP:    "10.0.0.2",
			ContainerID: "abc123",
		},
	}

	meta := &models.ServiceMeta{
		Name:        "nginx",
		Host:        "nginx.app.local",
		Port:        8080,
		Entrypoints: []string{"web"},
	}

	config := g.GenerateLocalConfig(tasks, meta)
	yamlBytes, err := yaml.Marshal(config)
	require.NoError(t, err)

	// Read golden file
	goldenPath := filepath.Join("..", "..", "test", "fixtures", "local_config_expected.yaml")
	expected, err := os.ReadFile(goldenPath)
	require.NoError(t, err)

	assert.Equal(t, string(expected), string(yamlBytes))
}

// =============================================================================
// StateManager Tests
// =============================================================================

func TestStateManager_New(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")
	assert.NotNil(t, sm)
	assert.NotNil(t, sm.lastFederation)
	assert.NotNil(t, sm.lastServices)
	assert.NotNil(t, sm.lastNodes)
}

func TestStateManager_GetService(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	sm.SetService(&models.ServiceMeta{Name: "nginx", Host: "nginx.local"})
	svc, ok := sm.GetService("nginx")
	assert.True(t, ok)
	assert.Equal(t, "nginx.local", svc.Host)

	_, ok = sm.GetService("nonexistent")
	assert.False(t, ok)
}

func TestStateManager_SetAndDeleteService(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	sm.SetService(&models.ServiceMeta{Name: "nginx"})
	assert.Len(t, sm.ListServices(), 1)

	sm.SetService(nil) // should not panic
	assert.Len(t, sm.ListServices(), 1)

	sm.DeleteService("nginx")
	assert.Len(t, sm.ListServices(), 0)

	sm.DeleteService("nonexistent") // should not panic
}

func TestStateManager_ListServices(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	sm.SetService(&models.ServiceMeta{Name: "svc1"})
	sm.SetService(&models.ServiceMeta{Name: "svc2"})
	sm.SetService(&models.ServiceMeta{Name: "svc3"})

	services := sm.ListServices()
	assert.Len(t, services, 3)
}

func TestStateManager_FederationTargets(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	sm.SetFederationTarget(&models.FederationTarget{
		ServiceName: "nginx",
		NodeIP:      "10.0.0.1",
	})
	assert.Len(t, sm.GetLastFederation(), 1)

	target, ok := sm.GetFederationTarget("nginx")
	assert.True(t, ok)
	assert.Equal(t, "10.0.0.1", target.NodeIP)

	sm.SetFederationTarget(nil) // should not panic
	assert.Len(t, sm.GetLastFederation(), 1)

	sm.DeleteFederationTarget("nginx")
	assert.Empty(t, sm.GetLastFederation())

	sm.DeleteFederationTarget("nonexistent") // should not panic
}

func TestStateManager_Nodes(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	node := &models.NodeInfo{
		ID:       "node1",
		Hostname: "worker-1",
		Addr:     "192.168.1.10",
	}
	sm.SetNode(node)
	assert.Len(t, sm.ListNodes(), 1)

	sm.SetNode(nil) // should not panic
	assert.Len(t, sm.ListNodes(), 1)

	n, ok := sm.GetNode("node1")
	assert.True(t, ok)
	assert.Equal(t, "worker-1", n.Hostname)

	_, ok = sm.GetNode("nonexistent")
	assert.False(t, ok)

	sm.DeleteNode("node1")
	assert.Empty(t, sm.ListNodes())

	sm.DeleteNode("nonexistent") // should not panic

	nodes := sm.GetLastNodes()
	assert.Empty(t, nodes)
}

func TestStateManager_SaveState_NoStore(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, stateFile)

	sm.SetService(&models.ServiceMeta{Name: "nginx", Host: "nginx.local"})

	err := sm.SaveState()
	require.NoError(t, err)
	assert.FileExists(t, stateFile)
}

func TestStateManager_LoadState_NoFile(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "/nonexistent/state.json")

	err := sm.LoadState()
	assert.NoError(t, err)
	assert.Empty(t, sm.GetLastServices())
	assert.Empty(t, sm.GetLastFederation())
}

func TestStateManager_LoadState_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	err := os.WriteFile(stateFile, []byte("{invalid json}"), 0644)
	require.NoError(t, err)

	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, stateFile)

	err = sm.LoadState()
	assert.NoError(t, err) // invalid JSON is not fatal, starts with empty state
}

func TestStateManager_UpdateFederation_NoChanges(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	targets := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "10.0.0.1"},
	}

	config, diff, err := sm.UpdateFederation(targets)
	assert.NoError(t, err)
	assert.NotNil(t, diff)
	assert.True(t, diff.HasChanges) // First update always has changes
	assert.NotNil(t, config)

	// Second update with same targets - no changes
	config, diff, err = sm.UpdateFederation(targets)
	assert.NoError(t, err)
	assert.NotNil(t, diff)
	assert.False(t, diff.HasChanges)
	assert.Nil(t, config)
}

func TestStateManager_UpdateFederation_WithChanges(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	initial := map[string]*models.FederationTarget{
		"nginx": {ServiceName: "nginx", NodeIP: "10.0.0.1"},
	}

	// First update
	_, _, err := sm.UpdateFederation(initial)
	require.NoError(t, err)

	// Changed targets
	updated := map[string]*models.FederationTarget{
		"nginx":  {ServiceName: "nginx", NodeIP: "10.0.0.2"},
		"newsvc": {ServiceName: "newsvc", NodeIP: "10.0.0.3"},
	}

	config, diff, err := sm.UpdateFederation(updated)
	assert.NoError(t, err)
	assert.True(t, diff.HasChanges)
	assert.Contains(t, diff.Modified, "nginx")
	assert.Contains(t, diff.Added, "newsvc")
	assert.NotNil(t, config)
}

func TestStateManager_UpdateLocal_NoChanges(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	tasks := []*models.LocalTaskInfo{
		{ServiceName: "nginx", BridgeIP: "10.0.0.2"},
	}
	meta := &models.ServiceMeta{
		Name:    "nginx",
		Host:    "nginx.local",
		Enabled: true,
		Port:    80,
	}

	// First update
	config, diff, err := sm.UpdateLocal(tasks, meta)
	assert.NoError(t, err)
	assert.NotNil(t, diff)
	assert.NotNil(t, config)

	// Second update with same data
	config, diff, err = sm.UpdateLocal(tasks, meta)
	assert.NoError(t, err)
	assert.NotNil(t, diff)
	assert.False(t, diff.HasChanges)
	assert.Nil(t, config)
}

func TestStateManager_UpdateLocal_DisabledService(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	tasks := []*models.LocalTaskInfo{
		{ServiceName: "nginx", BridgeIP: "10.0.0.2"},
	}
	meta := &models.ServiceMeta{
		Name:    "nginx",
		Host:    "nginx.local",
		Enabled: false,
	}

	config, diff, err := sm.UpdateLocal(tasks, meta)
	assert.NoError(t, err)
	assert.False(t, diff.HasChanges)
	assert.Nil(t, config)
}

func TestStateManager_GetLastServices(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	sm.SetService(&models.ServiceMeta{Name: "svc1"})
	sm.SetService(&models.ServiceMeta{Name: "svc2"})

	last := sm.GetLastServices()
	assert.Len(t, last, 2)
	assert.Contains(t, last, "svc1")
	assert.Contains(t, last, "svc2")
}

func TestStateManager_GetLastFederation(t *testing.T) {
	g := NewGenerator(80, "traefik_bridge")
	e := NewDiffEngine()
	sm := NewStateManager(g, e, nil, "")

	sm.SetFederationTarget(&models.FederationTarget{ServiceName: "nginx"})

	last := sm.GetLastFederation()
	assert.Len(t, last, 1)
	assert.Contains(t, last, "nginx")
}

func TestExtractNames(t *testing.T) {
	m := map[string]*models.ServiceMeta{
		"svc1": {Name: "svc1"},
		"svc2": {Name: "svc2"},
	}
	names := extractNames(m)
	assert.Len(t, names, 2)
	assert.Contains(t, names, "svc1")
	assert.Contains(t, names, "svc2")

	// Non-map type
	assert.Nil(t, extractNames("string"))
}
