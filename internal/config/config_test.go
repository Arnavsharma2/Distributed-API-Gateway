package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gateway.yaml")
	body := []byte(`
redis:
  url: redis://localhost:6379
routes:
  - name: products
    path_prefix: /api/products
    upstream_url: http://localhost:3000/products
    rate_limit:
      enabled: true
    cache:
      enabled: true
    retry:
      enabled: true
    circuit_breaker:
      enabled: true
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg.Server.Port)
	}
	route := cfg.Routes[0]
	if route.RateLimit.Key != "ip" {
		t.Fatalf("expected ip rate limit key, got %q", route.RateLimit.Key)
	}
	if route.Cache.TTLSeconds != 30 {
		t.Fatalf("expected cache ttl 30, got %d", route.Cache.TTLSeconds)
	}
	if route.Retry.Attempts != 2 {
		t.Fatalf("expected retry attempts 2, got %d", route.Retry.Attempts)
	}
	if route.CircuitBreaker.FailureThreshold != 5 {
		t.Fatalf("expected failure threshold 5, got %d", route.CircuitBreaker.FailureThreshold)
	}
}

func TestValidateRejectsDuplicateRouteNames(t *testing.T) {
	cfg := Config{
		Redis: RedisConfig{URL: "redis://localhost:6379"},
		Routes: []RouteConfig{
			{Name: "products", PathPrefix: "/a", UpstreamURL: "http://a"},
			{Name: "products", PathPrefix: "/b", UpstreamURL: "http://b"},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected duplicate route validation error")
	}
}
