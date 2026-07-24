package health

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/null8878/go-loadbalancer/balancer"
)

func mustParseURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

func newTestBackend(serverURL string, healthy bool) *balancer.Backend {
	return &balancer.Backend{
		URL:     mustParseURL(serverURL),
		Weight:  1,
		Healthy: healthy,
	}
}

func TestCheckBackendHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	b := newTestBackend(server.URL, true)
	mock := &mockBalancer{backends: []*balancer.Backend{b}}

	logger := slog.Default()
	checker := NewChecker(mock, Config{
		Interval:         time.Minute,
		Timeout:          time.Second,
		FailureThreshold: 3,
	}, logger)

	checker.checkAll()
	time.Sleep(200 * time.Millisecond)

	if !mock.isHealthy(0) {
		t.Error("backend should remain healthy after successful check")
	}
}

func TestCheckBackendUnhealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	b := newTestBackend(server.URL, true)
	mock := &mockBalancer{backends: []*balancer.Backend{b}}

	logger := slog.Default()
	checker := NewChecker(mock, Config{
		Interval:         time.Minute,
		Timeout:          time.Second,
		FailureThreshold: 1,
	}, logger)

	checker.checkAll()
	time.Sleep(200 * time.Millisecond)

	if mock.isHealthy(0) {
		t.Error("backend should be marked unhealthy after 500 response")
	}
}

func TestCheckBackendConnectionRefused(t *testing.T) {
	b := newTestBackend("http://localhost:19999", true)
	mock := &mockBalancer{backends: []*balancer.Backend{b}}

	logger := slog.Default()
	checker := NewChecker(mock, Config{
		Interval:         time.Minute,
		Timeout:          time.Second,
		FailureThreshold: 1,
	}, logger)

	checker.checkAll()
	time.Sleep(200 * time.Millisecond)

	if mock.isHealthy(0) {
		t.Error("backend should be marked unhealthy after connection failure")
	}
}

func TestFailureThreshold(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	b := newTestBackend(server.URL, true)
	mock := &mockBalancer{backends: []*balancer.Backend{b}}

	logger := slog.Default()
	checker := NewChecker(mock, Config{
		Interval:         time.Minute,
		Timeout:          time.Second,
		FailureThreshold: 3,
	}, logger)

	checker.checkAll()
	time.Sleep(200 * time.Millisecond)
	if !mock.isHealthy(0) {
		t.Error("backend should remain healthy before threshold")
	}

	checker.checkAll()
	time.Sleep(200 * time.Millisecond)
	if !mock.isHealthy(0) {
		t.Error("backend should remain healthy before threshold")
	}

	checker.checkAll()
	time.Sleep(200 * time.Millisecond)
	if mock.isHealthy(0) {
		t.Error("backend should be unhealthy after reaching threshold")
	}
}

func TestRecovery(t *testing.T) {
	var mu sync.Mutex
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	b := newTestBackend(server.URL, false)
	mock := &mockBalancer{backends: []*balancer.Backend{b}}

	logger := slog.Default()
	checker := NewChecker(mock, Config{
		Interval:         time.Minute,
		Timeout:          time.Second,
		FailureThreshold: 1,
	}, logger)

	checker.checkAll()
	time.Sleep(200 * time.Millisecond)
	if mock.isHealthy(0) {
		t.Error("backend should be unhealthy when server returns 500")
	}

	mu.Lock()
	fail = false
	mu.Unlock()

	checker.checkAll()
	time.Sleep(200 * time.Millisecond)
	if !mock.isHealthy(0) {
		t.Error("backend should recover when server returns 200")
	}
}

// mockBalancer implements balancer.Balancer for testing with thread-safe state access.
type mockBalancer struct {
	mu       sync.RWMutex
	backends []*balancer.Backend
}

func (m *mockBalancer) NextBackend() *balancer.Backend {
	return nil
}

func (m *mockBalancer) AddBackend(b *balancer.Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends = append(m.backends, b)
}

func (m *mockBalancer) Backends() []*balancer.Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dst := make([]*balancer.Backend, len(m.backends))
	copy(dst, m.backends)
	return dst
}

func (m *mockBalancer) MarkHealthy(index int, healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= 0 && index < len(m.backends) {
		m.backends[index].Healthy = healthy
	}
}

func (m *mockBalancer) IncrementConns(index int, delta int64) {
}

func (m *mockBalancer) IncrementFailures(index int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= 0 && index < len(m.backends) {
		m.backends[index].FailureCount++
	}
}

func (m *mockBalancer) ResetFailures(index int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= 0 && index < len(m.backends) {
		m.backends[index].FailureCount = 0
	}
}

// isHealthy reads the health status through the mutex.
func (m *mockBalancer) isHealthy(index int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if index >= 0 && index < len(m.backends) {
		return m.backends[index].Healthy
	}
	return false
}

// Ensure atomic is used where needed (suppress unused import).
var _ = atomic.Bool{}
