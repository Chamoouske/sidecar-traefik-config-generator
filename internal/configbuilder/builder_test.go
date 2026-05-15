package configbuilder

import (
	"testing"
)

func TestLocalConfig(t *testing.T) {
	cfg := LocalConfig("api", "Host(`api.worker-01.lab`)", "172.18.0.5", "8080", nil)

	data, err := cfg.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML failed: %v", err)
	}

	parsed, err := ParseYAML(data)
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	router, ok := parsed.HTTP.Routers["api"]
	if !ok {
		t.Fatal("Expected router 'api'")
	}
	if router.Rule != "Host(`api.worker-01.lab`)" {
		t.Errorf("Expected rule Host(`api.worker-01.lab`), got %s", router.Rule)
	}
	if router.EntryPoints != nil {
		t.Errorf("Expected nil entrypoints (using Traefik defaults), got %v", router.EntryPoints)
	}
	if router.Service != "api" {
		t.Errorf("Expected service api, got %s", router.Service)
	}

	svc, ok := parsed.HTTP.Services["api"]
	if !ok {
		t.Fatal("Expected service 'api'")
	}
	if svc.LoadBalancer == nil {
		t.Fatal("Expected loadBalancer")
	}
	if len(svc.LoadBalancer.Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(svc.LoadBalancer.Servers))
	}
	if svc.LoadBalancer.Servers[0].URL != "http://172.18.0.5:8080" {
		t.Errorf("Expected http://172.18.0.5:8080, got %s", svc.LoadBalancer.Servers[0].URL)
	}
}

func TestFederationConfig(t *testing.T) {
	cfg := FederationConfig("api", "Host(`api.worker-01.lab`)", "worker-01", 80, nil)

	data, err := cfg.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML failed: %v", err)
	}

	parsed, err := ParseYAML(data)
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	router, ok := parsed.HTTP.Routers["api"]
	if !ok {
		t.Fatalf("Expected router 'api'")
	}
	if router.Rule != "Host(`api.worker-01.lab`)" {
		t.Errorf("Expected rule Host(`api.worker-01.lab`), got %s", router.Rule)
	}
	if router.Service != "api" {
		t.Errorf("Expected service api, got %s", router.Service)
	}

	svc, ok := parsed.HTTP.Services["api"]
	if !ok {
		t.Fatalf("Expected service 'api'")
	}
	if svc.LoadBalancer == nil {
		t.Fatal("Expected loadBalancer")
	}
	if len(svc.LoadBalancer.Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(svc.LoadBalancer.Servers))
	}
	if svc.LoadBalancer.Servers[0].URL != "http://worker-01:80" {
		t.Errorf("Expected http://worker-01:80, got %s", svc.LoadBalancer.Servers[0].URL)
	}
}

func TestLocalConfig_WithCustomLabels(t *testing.T) {
	labels := map[string]string{
		"traefik.http.routers.api.entrypoints": "web,websecure",
		"traefik.http.routers.api.tls":         "true",
	}

	cfg := LocalConfig("api", "Host(`api.custom.lab`)", "10.0.0.1", "3000", labels)

	data, err := cfg.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML failed: %v", err)
	}

	parsed, err := ParseYAML(data)
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	router := parsed.HTTP.Routers["api"]
	if router == nil {
		t.Fatal("Expected router 'api'")
	}

	if len(router.EntryPoints) != 2 || router.EntryPoints[0] != "web" || router.EntryPoints[1] != "websecure" {
		t.Errorf("Expected entrypoints [web, websecure], got %v", router.EntryPoints)
	}

	if router.TLS == nil {
		t.Error("Expected TLS to be enabled")
	}
}

func TestMiddlewareConfig(t *testing.T) {
	mwConfig := map[string]interface{}{
		"forwardAuth": map[string]interface{}{
			"address": "http://auth-service.lab/verify",
		},
	}

	cfg := MiddlewareConfig("auth", mwConfig)

	data, err := cfg.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML failed: %v", err)
	}

	parsed, err := ParseYAML(data)
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	if parsed.HTTP.Middlewares == nil {
		t.Fatal("Expected middlewares")
	}
	mw, ok := parsed.HTTP.Middlewares["auth"]
	if !ok {
		t.Fatal("Expected middleware 'auth'")
	}
	_ = mw
}

func TestParseYAML_Invalid(t *testing.T) {
	_, err := ParseYAML([]byte("invalid: yaml: ["))
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestLocalConfig_NilEntrypoints(t *testing.T) {
	cfg := LocalConfig("api", "Host(`api.test.lab`)", "10.0.0.1", "8080", nil)

	data, err := cfg.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML failed: %v", err)
	}

	parsed, err := ParseYAML(data)
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	router := parsed.HTTP.Routers["api"]
	if router == nil {
		t.Fatal("Expected router 'api'")
	}

	if router.EntryPoints != nil {
		t.Logf("EntryPoints is %v (nil is also acceptable if Traefik uses defaults)", router.EntryPoints)
	}
}

func TestLocalConfig_EntrypointsFromLabel(t *testing.T) {
	labels := map[string]string{
		"traefik.http.routers.api.entrypoints": "web",
	}

	cfg := LocalConfig("api", "Host(`api.test.lab`)", "10.0.0.1", "8080", labels)

	data, err := cfg.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML failed: %v", err)
	}

	parsed, err := ParseYAML(data)
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	router := parsed.HTTP.Routers["api"]
	if router == nil {
		t.Fatal("Expected router 'api'")
	}

	if len(router.EntryPoints) != 1 || router.EntryPoints[0] != "web" {
		t.Errorf("Expected [web], got %v", router.EntryPoints)
	}
}

func TestLocalConfig_ServiceURL(t *testing.T) {
	tests := []struct {
		ip   string
		port string
		want string
	}{
		{"10.0.0.1", "3000", "http://10.0.0.1:3000"},
		{"172.18.0.5", "8080", "http://172.18.0.5:8080"},
		{"192.168.1.100", "9090", "http://192.168.1.100:9090"},
	}

	for _, tt := range tests {
		cfg := LocalConfig("svc", "Host(`svc.test.lab`)", tt.ip, tt.port, nil)
		if cfg.HTTP.Services["svc"].LoadBalancer.Servers[0].URL != tt.want {
			t.Errorf("Expected URL %s, got %s", tt.want, cfg.HTTP.Services["svc"].LoadBalancer.Servers[0].URL)
		}
	}
}

func TestFederationConfig_TLS(t *testing.T) {
	labels := map[string]string{
		"traefik.http.routers.api.tls":              "true",
		"traefik.http.routers.api.tls.certResolver": "letsencrypt",
	}

	cfg := FederationConfig("api", "Host(`api.worker-01.lab`)", "worker-01", 80, labels)

	router := cfg.HTTP.Routers["api"]
	if router == nil {
		t.Fatal("Expected router 'api'")
	}
	if router.TLS == nil {
		t.Fatal("Expected TLS to be enabled")
	}
	if router.TLS.CertResolver != "letsencrypt" {
		t.Errorf("Expected certResolver letsencrypt, got %s", router.TLS.CertResolver)
	}
}
