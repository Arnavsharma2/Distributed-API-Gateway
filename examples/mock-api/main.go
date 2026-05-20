package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

var flakyCounter uint64

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/products", productsHandler)
	mux.HandleFunc("/products/", productsHandler)
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]string{
			"route":      "slow",
			"request_id": r.Header.Get("X-Request-ID"),
		})
	})
	mux.HandleFunc("/flaky", func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddUint64(&flakyCounter, 1)
		if count%3 == 0 {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error":      "simulated upstream failure",
				"request_id": r.Header.Get("X-Request-ID"),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"route":      "flaky",
			"request_id": r.Header.Get("X-Request-ID"),
			"attempt":    strconv.FormatUint(count, 10),
		})
	})
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":      "simulated permanent failure",
			"request_id": r.Header.Get("X-Request-ID"),
		})
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"method":     r.Method,
			"path":       r.URL.Path,
			"request_id": r.Header.Get("X-Request-ID"),
		})
	})

	port := envOrDefault("PORT", "3000")
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           loggingMiddleware(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("mock api listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("mock api failed", "error", err)
		os.Exit(1)
	}
}

func productsHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"route":      "products",
		"path":       r.URL.Path,
		"request_id": r.Header.Get("X-Request-ID"),
		"items": []map[string]interface{}{
			{"id": "prod_001", "name": "Starter API Plan", "price_cents": 1900},
			{"id": "prod_002", "name": "Scale API Plan", "price_cents": 9900},
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("mock request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", r.Header.Get("X-Request-ID"),
		)
	})
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
