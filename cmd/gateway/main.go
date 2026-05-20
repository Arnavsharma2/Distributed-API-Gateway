package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aps/gatekeeper/internal/config"
	"github.com/aps/gatekeeper/internal/observability"
	"github.com/aps/gatekeeper/internal/proxy"
	"github.com/redis/go-redis/v9"
)

func main() {
	defaultConfig := envOrDefault("GATEKEEPER_CONFIG", "/etc/gatekeeper/gateway.yaml")
	configPath := flag.String("config", defaultConfig, "path to gateway YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	redisClient, err := newRedisClient(cfg.Redis.URL)
	if err != nil {
		logger.Error("failed to create redis client", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 2*time.Second)
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		logger.Warn("redis is unavailable; rate limiting will fail open and cache will be bypassed", "error", err)
	}
	cancelPing()

	metrics := observability.NewMetrics()
	gateway := proxy.NewGateway(cfg, redisClient, metrics, logger)

	appMux := http.NewServeMux()
	appMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	appMux.Handle("/", gateway)

	appServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           appMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	metricsServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.MetricsPort),
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		logger.Info("gateway server listening", "addr", appServer.Addr)
		errCh <- appServer.ListenAndServe()
	}()
	go func() {
		logger.Info("metrics server listening", "addr", metricsServer.Addr)
		errCh <- metricsServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := appServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("gateway shutdown failed", "error", err)
	}
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics shutdown failed", "error", err)
	}
}

func newRedisClient(rawURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opts), nil
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
