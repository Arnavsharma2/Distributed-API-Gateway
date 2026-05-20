package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aps/gatekeeper/internal/config"
	"github.com/redis/go-redis/v9"
)

func TestKeyFromRequestUsesForwardedIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/products", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.1")
	route := config.RouteConfig{
		Name: "products",
		RateLimit: config.RateLimitConfig{
			Key: "ip",
		},
	}

	key := KeyFromRequest(req, route)
	if key != "gk:rl:products:203.0.113.10" {
		t.Fatalf("unexpected key %q", key)
	}
}

func TestKeyFromRequestUsesHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/products", nil)
	req.Header.Set("X-User-ID", "user-123")
	route := config.RouteConfig{
		Name: "products",
		RateLimit: config.RateLimitConfig{
			Key:    "header",
			Header: "X-User-ID",
		},
	}

	key := KeyFromRequest(req, route)
	if key != "gk:rl:products:user-123" {
		t.Fatalf("unexpected key %q", key)
	}
}

func TestRedisLimiterIntegration(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("set REDIS_URL to run Redis integration test")
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	defer client.Close()

	ctx := context.Background()
	key := "gk:test:ratelimit"
	if err := client.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}

	limiter := NewRedisLimiter(client)
	first, err := limiter.Allow(ctx, key, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := limiter.Allow(ctx, key, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	third, err := limiter.Allow(ctx, key, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if !first.Allowed || !second.Allowed || third.Allowed {
		t.Fatalf("expected two allowed requests then rejection, got %#v %#v %#v", first, second, third)
	}
}
