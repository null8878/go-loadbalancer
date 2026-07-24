package health

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/null8878/go-loadbalancer/balancer"
)

// Config holds health checker settings.
type Config struct {
	Interval         time.Duration
	Timeout          time.Duration
	FailureThreshold int
}

// Checker periodically probes backends and updates their healthy state.
type Checker struct {
	balancer balancer.Balancer
	config   Config
	logger   *slog.Logger
	client   *http.Client

	mu              sync.Mutex
	failureCounts   map[string]int
	lastKnownHealth map[string]bool
}

// NewChecker creates a health checker with the given config.
func NewChecker(b balancer.Balancer, cfg Config, logger *slog.Logger) *Checker {
	return &Checker{
		balancer:        b,
		config:          cfg,
		logger:          logger,
		failureCounts:   make(map[string]int),
		lastKnownHealth: make(map[string]bool),
		client: &http.Client{
			Timeout: cfg.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Start begins the periodic health check loop. It blocks until the context is cancelled.
func (c *Checker) Start(ctx context.Context) {
	c.logger.Info("health checker started",
		"interval", c.config.Interval,
		"timeout", c.config.Timeout,
		"failure_threshold", c.config.FailureThreshold,
	)

	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()

	c.checkAll()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("health checker stopped")
			return
		case <-ticker.C:
			c.checkAll()
		}
	}
}

func (c *Checker) checkAll() {
	backends := c.balancer.Backends()
	for i, b := range backends {
		go c.checkBackend(i, b)
	}
}

func (c *Checker) checkBackend(index int, b *balancer.Backend) {
	healthURL := fmt.Sprintf("%s%s", b.URL.String(), b.HealthPath)
	if b.HealthPath == "" || b.HealthPath == "/" {
		healthURL = b.URL.String()
	}

	resp, err := c.client.Get(healthURL)

	isFailure := err != nil || resp.StatusCode >= 500
	if resp != nil {
		resp.Body.Close()
	}

	backendKey := b.String()

	c.mu.Lock()
	defer c.mu.Unlock()

	// On first encounter, backends are assumed healthy (their default state).
	if _, seen := c.lastKnownHealth[backendKey]; !seen {
		c.lastKnownHealth[backendKey] = true
	}
	wasHealthy := c.lastKnownHealth[backendKey]

	if isFailure {
		c.failureCounts[backendKey]++
		failures := c.failureCounts[backendKey]
		if failures >= c.config.FailureThreshold && wasHealthy {
			c.lastKnownHealth[backendKey] = false
			c.balancer.MarkHealthy(index, false)
			c.logger.Warn("backend marked unhealthy",
				"backend", backendKey,
				"consecutive_failures", failures,
			)
		} else {
			c.logger.Debug("health check failed",
				"backend", backendKey,
				"consecutive_failures", failures,
			)
		}
	} else {
		c.failureCounts[backendKey] = 0
		if !wasHealthy {
			c.lastKnownHealth[backendKey] = true
			c.balancer.MarkHealthy(index, true)
			c.logger.Info("backend recovered",
				"backend", backendKey,
			)
		}
	}
}
