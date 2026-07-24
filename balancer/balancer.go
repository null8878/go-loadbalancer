package balancer

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
)

// Backend represents a single upstream server with its state.
type Backend struct {
	URL            *url.URL
	Weight         int
	HealthPath     string
	Healthy        bool
	ActiveConns    int64
	FailureCount   int
	ReverseProxy   *httputil.ReverseProxy
}

// String returns the host:port of the backend.
func (b *Backend) String() string {
	return b.URL.Host
}

// Balancer is the interface that all load balancing algorithms must implement.
type Balancer interface {
	// NextBackend returns the next available backend. Returns nil if no healthy backend exists.
	NextBackend() *Backend
	// AddBackend registers a new backend.
	AddBackend(b *Backend)
	// Backends returns all registered backends.
	Backends() []*Backend
	// MarkHealthy sets the healthy state for the backend at the given index.
	MarkHealthy(index int, healthy bool)
	// IncrementConns atomically adjusts the connection count for a backend.
	IncrementConns(index int, delta int64)
	// IncrementFailures increments the consecutive failure count for a backend.
	IncrementFailures(index int)
	// ResetFailures resets the failure count for a backend.
	ResetFailures(index int)
}

// baseBalancer holds common state shared by all algorithms.
type baseBalancer struct {
	mu       sync.RWMutex
	backends []*Backend
}

func (b *baseBalancer) AddBackend(backend *Backend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.backends = append(b.backends, backend)
}

func (b *baseBalancer) Backends() []*Backend {
	b.mu.RLock()
	defer b.mu.RUnlock()
	dst := make([]*Backend, len(b.backends))
	copy(dst, b.backends)
	return dst
}

func (b *baseBalancer) MarkHealthy(index int, healthy bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if index >= 0 && index < len(b.backends) {
		b.backends[index].Healthy = healthy
	}
}

func (b *baseBalancer) IncrementConns(index int, delta int64) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if index >= 0 && index < len(b.backends) {
		atomic.AddInt64(&b.backends[index].ActiveConns, delta)
	}
}

func (b *baseBalancer) IncrementFailures(index int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if index >= 0 && index < len(b.backends) {
		b.backends[index].FailureCount++
	}
}

func (b *baseBalancer) ResetFailures(index int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if index >= 0 && index < len(b.backends) {
		b.backends[index].FailureCount = 0
	}
}

// healthy returns only healthy backends.
func (b *baseBalancer) healthy() []*Backend {
	var result []*Backend
	for _, be := range b.backends {
		if be.Healthy {
			result = append(result, be)
		}
	}
	return result
}

// --- Round Robin ---

type roundRobinBalancer struct {
	baseBalancer
	current atomic.Uint64
}

func NewRoundRobinBalancer() Balancer {
	return &roundRobinBalancer{}
}

func (r *roundRobinBalancer) NextBackend() *Backend {
	healthy := r.healthy()
	if len(healthy) == 0 {
		return nil
	}
	idx := r.current.Add(1) - 1
	return healthy[idx%uint64(len(healthy))]
}

// --- Weighted Round Robin ---

type weightedRoundRobinBalancer struct {
	baseBalancer
	currentWeight []int
	mu            sync.Mutex
}

func NewWeightedRoundRobinBalancer() Balancer {
	return &weightedRoundRobinBalancer{}
}

func (w *weightedRoundRobinBalancer) NextBackend() *Backend {
	w.mu.Lock()
	defer w.mu.Unlock()

	healthy := w.healthy()
	if len(healthy) == 0 {
		return nil
	}

	// Initialize weights on first call or when list changes.
	if len(w.currentWeight) != len(w.backends) {
		w.currentWeight = make([]int, len(w.backends))
		for i, b := range w.backends {
			w.currentWeight[i] = b.Weight
		}
	}

	totalWeight := 0
	bestIdx := -1
	bestWeight := -1

	for i, b := range w.backends {
		if !b.Healthy {
			continue
		}
		w.currentWeight[i] += b.Weight
		totalWeight += b.Weight
		if w.currentWeight[i] > bestWeight {
			bestWeight = w.currentWeight[i]
			bestIdx = i
		}
	}

	if bestIdx == -1 {
		return nil
	}

	w.currentWeight[bestIdx] -= totalWeight
	return w.backends[bestIdx]
}

// --- Least Connections ---

type leastConnectionsBalancer struct {
	baseBalancer
}

func NewLeastConnectionsBalancer() Balancer {
	return &leastConnectionsBalancer{}
}

func (lc *leastConnectionsBalancer) NextBackend() *Backend {
	healthy := lc.healthy()
	if len(healthy) == 0 {
		return nil
	}

	var best *Backend
	var bestConns int64 = -1

	for _, b := range healthy {
		conns := atomic.LoadInt64(&b.ActiveConns)
		if best == nil || conns < bestConns {
			best = b
			bestConns = conns
		}
	}

	return best
}

// --- Random ---

type randomBalancer struct {
	baseBalancer
}

func NewRandomBalancer() Balancer {
	return &randomBalancer{}
}

func (rb *randomBalancer) NextBackend() *Backend {
	healthy := rb.healthy()
	if len(healthy) == 0 {
		return nil
	}
	return healthy[rand.Intn(len(healthy))]
}

// NewBalancer creates a Balancer for the given algorithm name.
func NewBalancer(algorithm string) (Balancer, error) {
	switch algorithm {
	case "round_robin":
		return NewRoundRobinBalancer(), nil
	case "weighted_round_robin":
		return NewWeightedRoundRobinBalancer(), nil
	case "least_connections":
		return NewLeastConnectionsBalancer(), nil
	case "random":
		return NewRandomBalancer(), nil
	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
}

// BuildBackend creates a Backend from a URL and sets up its reverse proxy.
func BuildBackend(rawURL string, weight int) (*Backend, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing backend URL %q: %w", rawURL, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.Transport = &backendTransport{target: u.Host}

	return &Backend{
		URL:          u,
		Weight:       weight,
		Healthy:      true,
		ReverseProxy: proxy,
	}, nil
}

// backendTransport wraps http.Transport to track per-backend connections.
type backendTransport struct {
	target string
}

func (t *backendTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return http.DefaultTransport.RoundTrip(req)
}
