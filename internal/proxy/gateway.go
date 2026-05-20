package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	gatecache "github.com/aps/gatekeeper/internal/cache"
	"github.com/aps/gatekeeper/internal/config"
	"github.com/aps/gatekeeper/internal/observability"
	"github.com/aps/gatekeeper/internal/ratelimit"
	"github.com/aps/gatekeeper/internal/resilience"
	"github.com/redis/go-redis/v9"
)

type Gateway struct {
	cfg      *config.Config
	cache    *gatecache.RedisCache
	limiter  *ratelimit.RedisLimiter
	breakers *resilience.Registry
	metrics  *observability.Metrics
	logger   *slog.Logger
	client   *http.Client
}

type upstreamResponse struct {
	status int
	header http.Header
	body   []byte
}

func NewGateway(cfg *config.Config, redisClient *redis.Client, metrics *observability.Metrics, logger *slog.Logger) *Gateway {
	return &Gateway{
		cfg:      cfg,
		cache:    gatecache.NewRedisCache(redisClient),
		limiter:  ratelimit.NewRedisLimiter(redisClient),
		breakers: resilience.NewRegistry(cfg.Routes),
		metrics:  metrics,
		logger:   logger,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 5 * time.Second,
			},
		},
	}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	requestID := requestID(req)
	route := MatchRoute(g.cfg.Routes, req.URL.Path)
	if route == nil {
		http.Error(w, "no matching route", http.StatusNotFound)
		g.metrics.RecordRequest("unmatched", req.Method, http.StatusNotFound, time.Since(start))
		return
	}

	status := http.StatusOK
	cacheStatus := "skip"
	rateLimitStatus := "disabled"
	upstreamStatus := 0

	defer func() {
		g.metrics.RecordRequest(route.Name, req.Method, status, time.Since(start))
		g.logger.Info("request completed",
			"request_id", requestID,
			"route", route.Name,
			"method", req.Method,
			"path", req.URL.Path,
			"status", status,
			"upstream_status", upstreamStatus,
			"duration_ms", time.Since(start).Milliseconds(),
			"cache", cacheStatus,
			"rate_limit", rateLimitStatus,
		)
	}()

	rateHeaders := http.Header{}
	if route.RateLimit.Enabled {
		result, err := g.checkRateLimit(req.Context(), req, *route)
		if err != nil {
			rateLimitStatus = "unavailable"
			g.logger.Warn("rate limiter unavailable; request allowed", "request_id", requestID, "route", route.Name, "error", err)
		} else {
			rateLimitStatus = "allowed"
			rateHeaders.Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
			rateHeaders.Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			if !result.Allowed {
				rateLimitStatus = "limited"
				status = http.StatusTooManyRequests
				rateHeaders.Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())))
				copyHeaders(w.Header(), rateHeaders)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
				g.metrics.RecordRateLimited(route.Name)
				return
			}
		}
	}

	if route.Cache.Enabled && req.Method == http.MethodGet {
		cached, hit, err := g.cache.Get(req.Context(), gatecache.Key(route.Name, req))
		if err != nil {
			cacheStatus = "unavailable"
			g.metrics.RecordCache(route.Name, "error")
			g.logger.Warn("cache unavailable; request will be proxied", "request_id", requestID, "route", route.Name, "error", err)
		} else if hit {
			cacheStatus = "hit"
			status = cached.StatusCode
			copyHeaders(w.Header(), http.Header(cached.Header))
			copyHeaders(w.Header(), rateHeaders)
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(status)
			_, _ = w.Write(cached.Body)
			g.metrics.RecordCache(route.Name, "hit")
			return
		} else {
			cacheStatus = "miss"
			g.metrics.RecordCache(route.Name, "miss")
		}
	}

	if breaker, ok := g.breakers.Get(route.Name); ok {
		if !breaker.Allow() {
			status = http.StatusServiceUnavailable
			g.metrics.SetCircuitState(route.Name, breaker.State())
			copyHeaders(w.Header(), rateHeaders)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"upstream circuit open"}`))
			return
		}
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		status = http.StatusBadRequest
		http.Error(w, "failed to read request body", status)
		return
	}
	defer req.Body.Close()

	resp, err := g.forwardWithRetries(req.Context(), req, *route, body, requestID)
	if err != nil {
		status = http.StatusBadGateway
		g.recordBreakerFailure(route.Name)
		g.metrics.RecordUpstreamError(route.Name)
		copyHeaders(w.Header(), rateHeaders)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":%q}`, err.Error())))
		return
	}

	status = resp.status
	upstreamStatus = resp.status
	if resp.status >= http.StatusInternalServerError {
		g.recordBreakerFailure(route.Name)
		g.metrics.RecordUpstreamError(route.Name)
	} else {
		g.recordBreakerSuccess(route.Name)
	}

	if route.Cache.Enabled && req.Method == http.MethodGet && resp.status >= 200 && resp.status < 300 {
		cacheStatus = "stored"
		ttl := time.Duration(route.Cache.TTLSeconds) * time.Second
		payload := gatecache.Response{
			StatusCode: resp.status,
			Header:     gatecache.CacheableHeaders(resp.header),
			Body:       resp.body,
		}
		if err := g.cache.Set(req.Context(), gatecache.Key(route.Name, req), payload, ttl); err != nil {
			cacheStatus = "store_error"
			g.metrics.RecordCache(route.Name, "store_error")
			g.logger.Warn("failed to store response in cache", "request_id", requestID, "route", route.Name, "error", err)
		}
	}

	copyHeaders(w.Header(), resp.header)
	copyHeaders(w.Header(), rateHeaders)
	if cacheStatus == "hit" {
		w.Header().Set("X-Cache", "HIT")
	} else if route.Cache.Enabled && req.Method == http.MethodGet {
		w.Header().Set("X-Cache", cacheResponseHeader(cacheStatus))
	}
	w.WriteHeader(resp.status)
	_, _ = w.Write(resp.body)
}

func MatchRoute(routes []config.RouteConfig, requestPath string) *config.RouteConfig {
	bestIndex := -1
	bestLength := -1
	for i := range routes {
		prefix := strings.TrimRight(routes[i].PathPrefix, "/")
		if prefix == "" {
			prefix = "/"
		}
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			if len(prefix) > bestLength {
				bestIndex = i
				bestLength = len(prefix)
			}
		}
	}
	if bestIndex == -1 {
		return nil
	}
	return &routes[bestIndex]
}

func (g *Gateway) checkRateLimit(ctx context.Context, req *http.Request, route config.RouteConfig) (ratelimit.Result, error) {
	window := time.Duration(route.RateLimit.WindowSeconds) * time.Second
	return g.limiter.Allow(ctx, ratelimit.KeyFromRequest(req, route), route.RateLimit.Limit, window)
}

func (g *Gateway) forwardWithRetries(ctx context.Context, original *http.Request, route config.RouteConfig, body []byte, requestID string) (upstreamResponse, error) {
	attempts := 1
	if route.Retry.Enabled && route.Retry.Attempts > 0 {
		attempts = route.Retry.Attempts
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err := g.forwardOnce(ctx, original, route, body, requestID)
		if err == nil && resp.status < http.StatusInternalServerError {
			return resp, nil
		}

		if err == nil && attempt == attempts {
			return resp, nil
		}
		if err != nil {
			if attempt == attempts {
				return upstreamResponse{}, err
			}
			lastErr = err
		}

		g.metrics.RecordRetry(route.Name)
		if err == nil {
			g.metrics.RecordUpstreamError(route.Name)
		}

		delay := retryDelay(route.Retry.BaseDelayMS, attempt)
		select {
		case <-ctx.Done():
			return upstreamResponse{}, ctx.Err()
		case <-time.After(delay):
		}
	}

	if lastErr != nil {
		return upstreamResponse{}, lastErr
	}
	return upstreamResponse{}, fmt.Errorf("upstream failed after %d attempts", attempts)
}

func (g *Gateway) forwardOnce(ctx context.Context, original *http.Request, route config.RouteConfig, body []byte, requestID string) (upstreamResponse, error) {
	upstreamURL, err := rewriteURL(route, original.URL)
	if err != nil {
		return upstreamResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, original.Method, upstreamURL.String(), bytes.NewReader(body))
	if err != nil {
		return upstreamResponse{}, err
	}
	req.Header = cloneHeader(original.Header)
	removeHopByHopHeaders(req.Header)
	req.Header.Set("X-Request-ID", requestID)
	appendForwardedFor(req, original)

	resp, err := g.client.Do(req)
	if err != nil {
		return upstreamResponse{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return upstreamResponse{}, err
	}
	return upstreamResponse{
		status: resp.StatusCode,
		header: cloneHeader(resp.Header),
		body:   respBody,
	}, nil
}

func rewriteURL(route config.RouteConfig, original *url.URL) (*url.URL, error) {
	upstream, err := url.Parse(route.UpstreamURL)
	if err != nil {
		return nil, err
	}
	suffix := strings.TrimPrefix(original.Path, strings.TrimRight(route.PathPrefix, "/"))
	upstream.Path = joinURLPath(upstream.Path, suffix)
	upstream.RawQuery = original.RawQuery
	return upstream, nil
}

func joinURLPath(base string, suffix string) string {
	if suffix == "" || suffix == "/" {
		if base == "" {
			return "/"
		}
		return base
	}
	if base == "" || base == "/" {
		return path.Clean("/" + suffix)
	}
	return path.Clean(strings.TrimRight(base, "/") + "/" + strings.TrimLeft(suffix, "/"))
}

func retryDelay(baseDelayMS int, attempt int) time.Duration {
	if baseDelayMS <= 0 {
		baseDelayMS = 50
	}
	multiplier := math.Pow(2, float64(attempt-1))
	return time.Duration(float64(baseDelayMS)*multiplier) * time.Millisecond
}

func cacheResponseHeader(status string) string {
	if status == "stored" {
		return "MISS"
	}
	return strings.ToUpper(status)
}

func (g *Gateway) recordBreakerFailure(routeName string) {
	if breaker, ok := g.breakers.Get(routeName); ok {
		breaker.OnFailure()
		g.metrics.SetCircuitState(routeName, breaker.State())
	}
}

func (g *Gateway) recordBreakerSuccess(routeName string) {
	if breaker, ok := g.breakers.Get(routeName); ok {
		breaker.OnSuccess()
		g.metrics.SetCircuitState(routeName, breaker.State())
	}
}

func requestID(req *http.Request) string {
	if id := req.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(bytes[:])
}

func copyHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func cloneHeader(src http.Header) http.Header {
	dst := http.Header{}
	copyHeaders(dst, src)
	return dst
}

func removeHopByHopHeaders(header http.Header) {
	for key := range header {
		if isHopByHopHeader(key) {
			header.Del(key)
		}
	}
}

func isHopByHopHeader(key string) bool {
	_, ok := hopByHopHeaders[strings.ToLower(key)]
	return ok
}

var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
}

func appendForwardedFor(req *http.Request, original *http.Request) {
	prior := original.Header.Get("X-Forwarded-For")
	host, _, err := net.SplitHostPort(original.RemoteAddr)
	if err != nil {
		host = original.RemoteAddr
	}
	if host == "" {
		return
	}
	if prior == "" {
		req.Header.Set("X-Forwarded-For", host)
		return
	}
	req.Header.Set("X-Forwarded-For", prior+", "+host)
}
