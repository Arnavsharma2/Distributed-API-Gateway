package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Response struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       []byte              `json:"body"`
}

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
}

func (c *RedisCache) Get(ctx context.Context, key string) (Response, bool, error) {
	raw, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return Response{}, false, nil
	}
	if err != nil {
		return Response{}, false, err
	}

	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Response{}, false, err
	}
	return resp, true, nil
}

func (c *RedisCache) Set(ctx context.Context, key string, resp Response, ttl time.Duration) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, raw, ttl).Err()
}

func Key(routeName string, req *http.Request) string {
	hasher := sha256.New()
	hasher.Write([]byte(routeName))
	hasher.Write([]byte{0})
	hasher.Write([]byte(req.Method))
	hasher.Write([]byte{0})
	hasher.Write([]byte(req.URL.RequestURI()))
	hasher.Write([]byte{0})

	if userID := req.Header.Get("X-User-ID"); userID != "" {
		hasher.Write([]byte("user:"))
		hasher.Write([]byte(userID))
	}
	if authorization := req.Header.Get("Authorization"); authorization != "" {
		hasher.Write([]byte("auth:"))
		hasher.Write([]byte(authorization))
	}

	return "gk:cache:" + hex.EncodeToString(hasher.Sum(nil))
}

func CacheableHeaders(header http.Header) map[string][]string {
	out := map[string][]string{}
	for key, values := range header {
		if skipHeader(key) {
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	return out
}

func skipHeader(key string) bool {
	lower := strings.ToLower(key)
	if lower == "set-cookie" {
		return true
	}
	_, isHopByHop := hopByHopHeaders[lower]
	return isHopByHop
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
