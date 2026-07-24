package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// BackendConfig defines a single upstream server.
type BackendConfig struct {
	URL        string `yaml:"url"`
	Weight     int    `yaml:"weight"`
	HealthPath string `yaml:"health_path"`
}

// HealthCheckConfig configures the health checking subsystem.
type HealthCheckConfig struct {
	Interval        time.Duration `yaml:"interval"`
	Timeout         time.Duration `yaml:"timeout"`
	FailureThreshold int          `yaml:"failure_threshold"`
}

// RateLimitConfig configures per-IP rate limiting.
type RateLimitConfig struct {
	Rate  float64 `yaml:"rate"`
	Burst int     `yaml:"burst"`
}

// Config is the top-level configuration for GoBalancer.
type Config struct {
	ListenAddr  string            `yaml:"listen_addr"`
	Algorithm   string            `yaml:"algorithm"`
	LogLevel    string            `yaml:"log_level"`
	Backends    []BackendConfig   `yaml:"backends"`
	HealthCheck HealthCheckConfig `yaml:"health_check"`
	RateLimit   RateLimitConfig   `yaml:"rate_limit"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		ListenAddr: ":8080",
		Algorithm:  "round_robin",
		LogLevel:   "info",
		HealthCheck: HealthCheckConfig{
			Interval:         5 * time.Second,
			Timeout:          2 * time.Second,
			FailureThreshold: 3,
		},
		RateLimit: RateLimitConfig{
			Rate:  100,
			Burst: 200,
		},
	}
}

// Load reads and parses a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks the configuration for logical errors.
func (c *Config) Validate() error {
	if len(c.Backends) == 0 {
		return fmt.Errorf("at least one backend is required")
	}

	for i, b := range c.Backends {
		if b.URL == "" {
			return fmt.Errorf("backend %d: URL is required", i)
		}
		if b.Weight < 1 {
			return fmt.Errorf("backend %d: weight must be >= 1", i)
		}
	}

	validAlgorithms := map[string]bool{
		"round_robin":       true,
		"weighted_round_robin": true,
		"least_connections": true,
		"random":            true,
	}
	if !validAlgorithms[c.Algorithm] {
		return fmt.Errorf("unknown algorithm %q: supported algorithms are round_robin, weighted_round_robin, least_connections, random", c.Algorithm)
	}

	return nil
}
