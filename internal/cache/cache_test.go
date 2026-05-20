package cache

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKeyVariesByUserID(t *testing.T) {
	first := httptest.NewRequest(http.MethodGet, "/api/products?id=1", nil)
	first.Header.Set("X-User-ID", "a")
	second := httptest.NewRequest(http.MethodGet, "/api/products?id=1", nil)
	second.Header.Set("X-User-ID", "b")

	if Key("products", first) == Key("products", second) {
		t.Fatal("expected cache key to vary by X-User-ID")
	}
}

func TestCacheableHeadersDropsHopByHopAndCookies(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("Connection", "keep-alive")
	headers.Set("Set-Cookie", "session=abc")

	cacheable := http.Header(CacheableHeaders(headers))
	if cacheable.Get("Content-Type") != "application/json" {
		t.Fatal("expected content type to be retained")
	}
	if cacheable.Get("Connection") != "" {
		t.Fatal("expected connection header to be dropped")
	}
	if cacheable.Get("Set-Cookie") != "" {
		t.Fatal("expected set-cookie header to be dropped")
	}
}
