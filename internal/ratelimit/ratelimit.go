package ratelimit

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aps/gatekeeper/internal/config"
	"github.com/redis/go-redis/v9"
)

type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

type RedisLimiter struct {
	client  *redis.Client
	counter uint64
}

func NewRedisLimiter(client *redis.Client) *RedisLimiter {
	return &RedisLimiter{client: client}
}

var slidingWindowScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]

redis.call("ZREMRANGEBYSCORE", key, 0, now - window)
local count = redis.call("ZCARD", key)

if count >= limit then
  local oldest = redis.call("ZRANGE", key, 0, 0, "WITHSCORES")
  local retry_after = 1
  if oldest[2] then
    retry_after = math.ceil((tonumber(oldest[2]) + window - now) / 1000)
    if retry_after < 1 then retry_after = 1 end
  end
  return {0, 0, retry_after}
end

redis.call("ZADD", key, now, member)
redis.call("PEXPIRE", key, window)
return {1, limit - count - 1, 0}
`)

func (l *RedisLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (Result, error) {
	if limit <= 0 {
		return Result{Allowed: true, Limit: limit, Remaining: 0}, nil
	}

	now := time.Now().UnixMilli()
	member := fmt.Sprintf("%d-%d", now, atomic.AddUint64(&l.counter, 1))
	values, err := slidingWindowScript.Run(ctx, l.client, []string{key}, now, window.Milliseconds(), limit, member).Result()
	if err != nil {
		return Result{}, err
	}

	parts, ok := values.([]interface{})
	if !ok || len(parts) != 3 {
		return Result{}, fmt.Errorf("unexpected redis rate limit response: %#v", values)
	}

	allowed, err := asInt64(parts[0])
	if err != nil {
		return Result{}, err
	}
	remaining, err := asInt64(parts[1])
	if err != nil {
		return Result{}, err
	}
	retryAfterSeconds, err := asInt64(parts[2])
	if err != nil {
		return Result{}, err
	}

	if remaining < 0 {
		remaining = 0
	}

	return Result{
		Allowed:    allowed == 1,
		Limit:      limit,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(retryAfterSeconds) * time.Second,
	}, nil
}

func KeyFromRequest(req *http.Request, route config.RouteConfig) string {
	identity := "unknown"
	keyMode := strings.ToLower(route.RateLimit.Key)

	switch {
	case keyMode == "ip":
		identity = clientIP(req)
	case keyMode == "header":
		identity = req.Header.Get(route.RateLimit.Header)
	case strings.HasPrefix(keyMode, "header:"):
		identity = req.Header.Get(strings.TrimPrefix(route.RateLimit.Key, "header:"))
	default:
		identity = req.Header.Get(route.RateLimit.Key)
	}

	if identity == "" {
		identity = "missing"
	}
	return fmt.Sprintf("gk:rl:%s:%s", route.Name, identity)
}

func clientIP(req *http.Request) string {
	if forwardedFor := req.Header.Get("X-Forwarded-For"); forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		return host
	}
	if req.RemoteAddr != "" {
		return req.RemoteAddr
	}
	return "unknown"
}

func asInt64(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected integer type %T", value)
	}
}
