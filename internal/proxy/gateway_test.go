package proxy

import (
	"net/url"
	"testing"
	"time"

	"github.com/aps/gatekeeper/internal/config"
)

func TestMatchRouteChoosesLongestPrefix(t *testing.T) {
	routes := []config.RouteConfig{
		{Name: "api", PathPrefix: "/api", UpstreamURL: "http://upstream/api"},
		{Name: "products", PathPrefix: "/api/products", UpstreamURL: "http://upstream/products"},
	}

	route := MatchRoute(routes, "/api/products/123")
	if route == nil || route.Name != "products" {
		t.Fatalf("expected products route, got %#v", route)
	}
}

func TestMatchRouteDoesNotMatchPartialSegment(t *testing.T) {
	routes := []config.RouteConfig{
		{Name: "products", PathPrefix: "/api/products", UpstreamURL: "http://upstream/products"},
	}

	if route := MatchRoute(routes, "/api/products-old"); route != nil {
		t.Fatalf("expected no match, got %#v", route)
	}
}

func TestRewriteURLPreservesSuffixAndQuery(t *testing.T) {
	route := config.RouteConfig{
		PathPrefix:  "/api/products",
		UpstreamURL: "http://mock-api:3000/products",
	}
	original, err := url.Parse("/api/products/123?color=blue")
	if err != nil {
		t.Fatal(err)
	}

	rewritten, err := rewriteURL(route, original)
	if err != nil {
		t.Fatal(err)
	}
	if rewritten.String() != "http://mock-api:3000/products/123?color=blue" {
		t.Fatalf("unexpected rewritten URL %s", rewritten.String())
	}
}

func TestRetryDelayExponential(t *testing.T) {
	if got := retryDelay(50, 3); got != 200*time.Millisecond {
		t.Fatalf("expected 200ms, got %s", got)
	}
}
