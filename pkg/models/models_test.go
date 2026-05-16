package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseServiceMeta_Enabled(t *testing.T) {
	labels := map[string]string{
		"traefik.federation.enabled":      "true",
		"traefik.federation.host":         "app.example.com",
		"traefik.federation.port":         "8080",
		"traefik.federation.tls":          "true",
		"traefik.federation.entrypoints":  "web,websecure",
		"traefik.federation.middlewares":  "cors,ratelimit",
	}

	meta := ParseServiceMeta(labels)
	assert.True(t, meta.Enabled)
	assert.Equal(t, "app.example.com", meta.Host)
	assert.Equal(t, 8080, meta.Port)
	assert.True(t, meta.TLS)
	assert.Equal(t, []string{"web", "websecure"}, meta.Entrypoints)
	assert.Equal(t, []string{"cors", "ratelimit"}, meta.Middlewares)
}

func TestParseServiceMeta_Disabled(t *testing.T) {
	labels := map[string]string{}
	meta := ParseServiceMeta(labels)
	assert.False(t, meta.Enabled)
	assert.Empty(t, meta.Host)
	assert.Equal(t, 0, meta.Port)
	assert.False(t, meta.TLS)
}

func TestParseServiceMeta_InvalidPort(t *testing.T) {
	labels := map[string]string{
		"traefik.federation.enabled": "true",
		"traefik.federation.port":    "invalid",
	}
	meta := ParseServiceMeta(labels)
	assert.True(t, meta.Enabled)
	assert.Equal(t, 0, meta.Port) // invalid port defaults to 0
}

func TestParseServiceMeta_NameOverride(t *testing.T) {
	labels := map[string]string{
		"traefik.federation.enabled": "true",
		"traefik.federation.name":    "custom-name",
	}
	meta := ParseServiceMeta(labels)
	assert.True(t, meta.Enabled)
	assert.Equal(t, "custom-name", meta.Name)
}

func TestParseServiceMeta_LabelTrueVariants(t *testing.T) {
	// Test different truthy values
	assert.True(t, isLabelTrue(map[string]string{"k": "true"}, "k"))
	assert.True(t, isLabelTrue(map[string]string{"k": "1"}, "k"))
	assert.True(t, isLabelTrue(map[string]string{"k": "yes"}, "k"))
	assert.True(t, isLabelTrue(map[string]string{"k": "TRUE"}, "k"))
	assert.True(t, isLabelTrue(map[string]string{"k": "YES"}, "k"))

	// Test falsy values
	assert.False(t, isLabelTrue(map[string]string{"k": "false"}, "k"))
	assert.False(t, isLabelTrue(map[string]string{"k": "0"}, "k"))
	assert.False(t, isLabelTrue(map[string]string{"k": "no"}, "k"))
	assert.False(t, isLabelTrue(map[string]string{}, "k"))
	assert.False(t, isLabelTrue(nil, "k"))
}

func TestSplitAndTrim(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, splitAndTrim("a,b,c"))
	assert.Equal(t, []string{"a", "b"}, splitAndTrim(" a , b "))
	assert.Equal(t, []string{"single"}, splitAndTrim("single"))
	assert.Equal(t, []string{}, splitAndTrim(""))
	assert.Equal(t, []string{}, splitAndTrim(","))
}

func TestNamingHelpers(t *testing.T) {
	assert.Equal(t, "service-nginx-federation", FederationServiceName("nginx"))
	assert.Equal(t, "nginx-local-router", LocalRouterName("nginx"))
	assert.Equal(t, "nginx-local-service", LocalServiceName("nginx"))
	assert.Equal(t, "nginx-federation-router", FederationRouterName("nginx"))
}

func TestDiffResult_IsEmpty(t *testing.T) {
	d := &DiffResult{HasChanges: false}
	assert.True(t, d.IsEmpty())

	d.HasChanges = true
	assert.False(t, d.IsEmpty())

	// Empty DiffResult (zero value)
	d2 := &DiffResult{}
	assert.True(t, d2.IsEmpty())
}

func TestDiffResult_WithChanges(t *testing.T) {
	d := &DiffResult{
		HasChanges: true,
		Added:      []string{"svc1"},
		Removed:    []string{"svc2"},
		Modified:   []string{"svc3"},
	}
	assert.False(t, d.IsEmpty())
	assert.Equal(t, []string{"svc1"}, d.Added)
	assert.Equal(t, []string{"svc2"}, d.Removed)
	assert.Equal(t, []string{"svc3"}, d.Modified)
}

func TestClusterState_New(t *testing.T) {
	s := NewClusterState()
	assert.NotNil(t, s)
	assert.NotNil(t, s.Services)
	assert.NotNil(t, s.Nodes)
	assert.NotNil(t, s.Tasks)
	assert.NotNil(t, s.Agents)
	assert.NotNil(t, s.Federations)
	assert.Empty(t, s.Services)
	assert.Empty(t, s.Nodes)
	assert.Empty(t, s.Tasks)
	assert.Empty(t, s.Agents)
	assert.Empty(t, s.Federations)
}

func TestClusterState_LockUnlock(t *testing.T) {
	s := NewClusterState()

	// Thread safety - Lock/Unlock for write
	s.Lock()
	s.Services["test"] = &ServiceMeta{Name: "test", Host: "test.local"}
	assert.Equal(t, "test.local", s.Services["test"].Host)
	s.Unlock()

	// RLock/RUnlock for read
	s.RLock()
	assert.Equal(t, "test", s.Services["test"].Name)
	s.RUnlock()
}

func TestClusterState_MultipleServices(t *testing.T) {
	s := NewClusterState()
	s.Lock()
	s.Services["svc1"] = &ServiceMeta{Name: "svc1", Port: 80}
	s.Services["svc2"] = &ServiceMeta{Name: "svc2", Port: 443, TLS: true}
	s.Unlock()

	s.RLock()
	assert.Len(t, s.Services, 2)
	assert.Equal(t, 80, s.Services["svc1"].Port)
	assert.True(t, s.Services["svc2"].TLS)
	s.RUnlock()
}

func TestConstants(t *testing.T) {
	assert.Equal(t, 80, DefaultTraefikHTTPPort)
	assert.Equal(t, 443, DefaultTraefikHTTPSPort)
}

func TestActionTypeConstants(t *testing.T) {
	assert.Equal(t, ActionType("CREATE"), ActionCreate)
	assert.Equal(t, ActionType("UPDATE"), ActionUpdate)
	assert.Equal(t, ActionType("DELETE"), ActionDelete)
}

func TestEventTypeConstants(t *testing.T) {
	assert.Equal(t, EventType("SERVICE_CREATE"), EventServiceCreate)
	assert.Equal(t, EventType("ServiceUpdate"), EventServiceUpdate)
	assert.Equal(t, EventType("SERVICE_REMOVE"), EventServiceRemove)
	assert.Equal(t, EventType("TASK_DEPLOY"), EventTaskDeploy)
	assert.Equal(t, EventType("TASK_REMOVE"), EventTaskRemove)
	assert.Equal(t, EventType("NODE_UPDATE"), EventNodeUpdate)
}

func TestParseServiceMeta_EntrypointsWithSpaces(t *testing.T) {
	labels := map[string]string{
		"traefik.federation.enabled":     "true",
		"traefik.federation.entrypoints": " web, websecure ",
	}
	meta := ParseServiceMeta(labels)
	assert.Equal(t, []string{"web", "websecure"}, meta.Entrypoints)
}

func TestParseServiceMeta_MiddlewaresEmpty(t *testing.T) {
	labels := map[string]string{
		"traefik.federation.enabled": "true",
	}
	meta := ParseServiceMeta(labels)
	assert.Empty(t, meta.Middlewares)
}

func TestParseServiceMeta_LabelsPreserved(t *testing.T) {
	labels := map[string]string{
		"traefik.federation.enabled": "true",
		"custom.label":               "value",
	}
	meta := ParseServiceMeta(labels)
	assert.NotNil(t, meta.Labels)
	assert.Equal(t, "true", meta.Labels["traefik.federation.enabled"])
	assert.Equal(t, "value", meta.Labels["custom.label"])
}

func TestClusterState_ConcurrentAccess(t *testing.T) {
	s := NewClusterState()

	// Simulate concurrent access pattern
	s.Lock()
	s.Services["svc1"] = &ServiceMeta{Name: "svc1"}
	s.Nodes["node1"] = &NodeInfo{ID: "node1", Hostname: "worker-1"}
	s.Tasks["task1"] = &LocalTaskInfo{TaskID: "task1", ServiceName: "svc1"}
	s.Agents["agent1"] = &AgentState{NodeID: "node1", Online: true}
	s.Federations["fed1"] = &FederationTarget{ServiceName: "svc1", NodeIP: "10.0.0.1"}
	s.Unlock()

	s.RLock()
	assert.Len(t, s.Services, 1)
	assert.Len(t, s.Nodes, 1)
	assert.Len(t, s.Tasks, 1)
	assert.Len(t, s.Agents, 1)
	assert.Len(t, s.Federations, 1)
	assert.Equal(t, "worker-1", s.Nodes["node1"].Hostname)
	assert.True(t, s.Agents["agent1"].Online)
	s.RUnlock()
}
