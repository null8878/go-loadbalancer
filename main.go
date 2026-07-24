package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/null8878/go-loadbalancer/balancer"
	"github.com/null8878/go-loadbalancer/config"
	"github.com/null8878/go-loadbalancer/health"
	"github.com/null8878/go-loadbalancer/middleware"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "path to YAML config file")
	listenAddr := flag.String("addr", "", "listen address override (e.g. :8080)")
	logLevel := flag.String("log", "", "log level: debug, info, warn, error")
	algorithm := flag.String("algorithm", "", "algorithm override: round_robin, weighted_round_robin, least_connections, random")
	flag.Parse()

	// Load configuration.
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Apply CLI overrides.
	if *listenAddr != "" {
		cfg.ListenAddr = *listenAddr
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}
	if *algorithm != "" {
		cfg.Algorithm = *algorithm
	}

	// Set up structured logger.
	logger := setupLogger(cfg.LogLevel)
	logger.Info("starting GoBalancer", "listen", cfg.ListenAddr, "algorithm", cfg.Algorithm, "backends", len(cfg.Backends))

	// Build backends and load balancer.
	bal, err := balancer.NewBalancer(cfg.Algorithm)
	if err != nil {
		logger.Error("failed to create balancer", "error", err)
		os.Exit(1)
	}

	for _, bc := range cfg.Backends {
		b, err := balancer.BuildBackend(bc.URL, bc.Weight)
		if err != nil {
			logger.Error("failed to build backend", "url", bc.URL, "error", err)
			os.Exit(1)
		}
		if bc.HealthPath != "" {
			b.HealthPath = bc.HealthPath
		} else {
			b.HealthPath = "/health"
		}
		bal.AddBackend(b)
		logger.Info("registered backend", "url", bc.URL, "weight", bc.Weight)
	}

	// Start health checker.
	healthChecker := health.NewChecker(bal, health.Config{
		Interval:         cfg.HealthCheck.Interval,
		Timeout:          cfg.HealthCheck.Timeout,
		FailureThreshold: cfg.HealthCheck.FailureThreshold,
	}, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go healthChecker.Start(ctx)

	// Build HTTP handler chain.
	proxy := &balancer.Proxy{
		Balancer: bal,
		Logger:   logger,
	}

	mux := http.NewServeMux()

	// Apply middleware chain: recovery → logging → rate limit → proxy.
	var handler http.Handler = proxy
	handler = middleware.RateLimit(cfg.RateLimit.Rate, cfg.RateLimit.Burst)(handler)
	handler = middleware.Logging(logger, isTerminal())(handler)
	handler = middleware.Recovery(logger)(handler)

	mux.Handle("/", handler)

	// Prometheus metrics endpoint.
	mux.Handle("/metrics", promhttp.Handler())

	// Health endpoint for the balancer itself.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Graceful shutdown.
	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr)
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig)
	case err := <-errCh:
		if err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
		}
	}

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped gracefully")
}

func setupLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
	})
	return slog.New(handler)
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
