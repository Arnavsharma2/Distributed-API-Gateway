package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig  `yaml:"server"`
	Redis  RedisConfig   `yaml:"redis"`
	Routes []RouteConfig `yaml:"routes"`
}

type ServerConfig struct {
	Port        int `yaml:"port"`
	MetricsPort int `yaml:"metrics_port"`
}

type RedisConfig struct {
	URL string `yaml:"url"`
}

type RouteConfig struct {
	Name           string               `yaml:"name"`
	PathPrefix     string               `yaml:"path_prefix"`
	UpstreamURL    string               `yaml:"upstream_url"`
	RateLimit      RateLimitConfig      `yaml:"rate_limit"`
	Cache          CacheConfig          `yaml:"cache"`
	Retry          RetryConfig          `yaml:"retry"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`
}

type RateLimitConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Key           string `yaml:"key"`
	Header        string `yaml:"header"`
	Limit         int    `yaml:"limit"`
	WindowSeconds int    `yaml:"window_seconds"`
}

type CacheConfig struct {
	Enabled    bool `yaml:"enabled"`
	TTLSeconds int  `yaml:"ttl_seconds"`
}

type RetryConfig struct {
	Enabled     bool `yaml:"enabled"`
	Attempts    int  `yaml:"attempts"`
	BaseDelayMS int  `yaml:"base_delay_ms"`
}

type CircuitBreakerConfig struct {
	Enabled          bool `yaml:"enabled"`
	FailureThreshold int  `yaml:"failure_threshold"`
	CooldownSeconds  int  `yaml:"cooldown_seconds"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.MetricsPort == 0 {
		c.Server.MetricsPort = 9090
	}
	if c.Redis.URL == "" {
		return errors.New("redis.url is required")
	}
	if len(c.Routes) == 0 {
		return errors.New("at least one route is required")
	}

	seenRoutes := map[string]bool{}
	for i := range c.Routes {
		route := &c.Routes[i]
		if route.Name == "" {
			return fmt.Errorf("routes[%d].name is required", i)
		}
		if seenRoutes[route.Name] {
			return fmt.Errorf("duplicate route name %q", route.Name)
		}
		seenRoutes[route.Name] = true
		if route.PathPrefix == "" || route.PathPrefix[0] != '/' {
			return fmt.Errorf("route %q path_prefix must start with /", route.Name)
		}
		if route.UpstreamURL == "" {
			return fmt.Errorf("route %q upstream_url is required", route.Name)
		}
		applyRouteDefaults(route)
	}
	return nil
}

func applyRouteDefaults(route *RouteConfig) {
	if route.RateLimit.Enabled {
		if route.RateLimit.Key == "" {
			route.RateLimit.Key = "ip"
		}
		if route.RateLimit.WindowSeconds == 0 {
			route.RateLimit.WindowSeconds = 60
		}
	}
	if route.Cache.Enabled && route.Cache.TTLSeconds == 0 {
		route.Cache.TTLSeconds = 30
	}
	if route.Retry.Enabled {
		if route.Retry.Attempts == 0 {
			route.Retry.Attempts = 2
		}
		if route.Retry.BaseDelayMS == 0 {
			route.Retry.BaseDelayMS = 50
		}
	}
	if route.CircuitBreaker.Enabled {
		if route.CircuitBreaker.FailureThreshold == 0 {
			route.CircuitBreaker.FailureThreshold = 5
		}
		if route.CircuitBreaker.CooldownSeconds == 0 {
			route.CircuitBreaker.CooldownSeconds = 20
		}
	}
}
