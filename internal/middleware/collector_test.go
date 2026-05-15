package middleware

import (
	"testing"
)

func TestExtractMiddlewareNames(t *testing.T) {
	labels := map[string]string{
		"traefik.federation.middlewares": "auth,ratelimit,headers",
	}

	names := ExtractMiddlewareNames(labels)
	if len(names) != 3 {
		t.Fatalf("Expected 3 names, got %d: %v", len(names), names)
	}
	if names[0] != "auth" || names[1] != "ratelimit" || names[2] != "headers" {
		t.Errorf("Unexpected names: %v", names)
	}
}

func TestExtractMiddlewareNames_Empty(t *testing.T) {
	labels := map[string]string{}
	names := ExtractMiddlewareNames(labels)
	if len(names) != 0 {
		t.Errorf("Expected empty, got %v", names)
	}
}

func TestExtractMiddlewareNames_Nil(t *testing.T) {
	names := ExtractMiddlewareNames(nil)
	if names != nil {
		t.Errorf("Expected nil, got %v", names)
	}
}

func TestExtractMiddlewareNames_Whitespace(t *testing.T) {
	labels := map[string]string{
		"traefik.federation.middlewares": " auth , ratelimit ",
	}

	names := ExtractMiddlewareNames(labels)
	if len(names) != 2 {
		t.Fatalf("Expected 2 names, got %d: %v", len(names), names)
	}
	if names[0] != "auth" || names[1] != "ratelimit" {
		t.Errorf("Unexpected names: %v", names)
	}
}

func TestCollector_ExtractFromLabels(t *testing.T) {
	c := NewCollector()

	labels := map[string]string{
		"traefik.federation.middleware.auth.forwardAuth.address":             "http://auth:8080/verify",
		"traefik.federation.middleware.auth.forwardAuth.authResponseHeaders": "X-Auth-User",
	}

	c.ExtractFromLabels("api", labels)

	mws := c.GetAll()
	if len(mws) != 1 {
		t.Fatalf("Expected 1 middleware, got %d", len(mws))
	}

	mw, ok := mws["auth"]
	if !ok {
		t.Fatal("Expected middleware 'auth'")
	}

	fwd, ok := mw.Config["forwardAuth"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected forwardAuth config")
	}
	if fwd["address"] != "http://auth:8080/verify" {
		t.Errorf("Expected address http://auth:8080/verify, got %v", fwd["address"])
	}
}

func TestCollector_Deduplication(t *testing.T) {
	c := NewCollector()

	labels := map[string]string{
		"traefik.federation.middleware.ratelimit.rateLimit.average": "100",
		"traefik.federation.middleware.ratelimit.rateLimit.burst":   "50",
	}

	c.ExtractFromLabels("api", labels)
	c.ExtractFromLabels("web", labels)

	mws := c.GetAll()
	if len(mws) != 1 {
		t.Fatalf("Expected 1 middleware after dedup, got %d", len(mws))
	}
}

func TestCollector_NilLabels(t *testing.T) {
	c := NewCollector()
	c.ExtractFromLabels("api", nil)

	mws := c.GetAll()
	if len(mws) != 0 {
		t.Errorf("Expected empty, got %d middleware(s)", len(mws))
	}
}

func TestCollector_MultipleMiddlewares(t *testing.T) {
	c := NewCollector()

	labels := map[string]string{
		"traefik.federation.middleware.auth.forwardAuth.address":                 "http://auth:8080/verify",
		"traefik.federation.middleware.ratelimit.rateLimit.average":              "100",
		"traefik.federation.middleware.ratelimit.rateLimit.burst":                "50",
		"traefik.federation.middleware.headers.customRequestHeaders.X-My-Header": "value",
	}

	c.ExtractFromLabels("api", labels)

	mws := c.GetAll()
	if len(mws) != 3 {
		t.Fatalf("Expected 3 middlewares, got %d", len(mws))
	}

	for _, name := range []string{"auth", "ratelimit", "headers"} {
		if _, ok := mws[name]; !ok {
			t.Errorf("Expected middleware '%s'", name)
		}
	}
}

func TestCollector_InvalidPrefixIgnored(t *testing.T) {
	c := NewCollector()

	labels := map[string]string{
		"traefik.http.routers.api.rule": "Host(`test.lab`)",
	}

	c.ExtractFromLabels("api", labels)

	mws := c.GetAll()
	if len(mws) != 0 {
		t.Errorf("Expected 0 middlewares from non-federation labels, got %d", len(mws))
	}
}
