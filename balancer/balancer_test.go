package balancer

import (
	"fmt"
	"net/url"
	"sync"
	"testing"
)

func makeBackends(count int) []*Backend {
	var backends []*Backend
	for i := 0; i < count; i++ {
		backends = append(backends, &Backend{
			URL:     mustParse(fmt.Sprintf("http://localhost:900%d", i+1)),
			Weight:  1,
			Healthy: true,
		})
	}
	return backends
}

func makeWeightedBackends() []*Backend {
	return []*Backend{
		{URL: mustParse("http://localhost:9001"), Weight: 1, Healthy: true},
		{URL: mustParse("http://localhost:9002"), Weight: 3, Healthy: true},
		{URL: mustParse("http://localhost:9003"), Weight: 1, Healthy: true},
	}
}

func mustParse(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

func TestRoundRobinBalancer(t *testing.T) {
	rr := NewRoundRobinBalancer()
	for _, b := range makeBackends(3) {
		rr.AddBackend(b)
	}

	results := make(map[string]int)
	for i := 0; i < 6; i++ {
		b := rr.NextBackend()
		if b == nil {
			t.Fatal("NextBackend returned nil")
		}
		results[b.String()]++
	}

	if results["localhost:9001"] != 2 || results["localhost:9002"] != 2 || results["localhost:9003"] != 2 {
		t.Errorf("expected even distribution, got %v", results)
	}
}

func TestRoundRobinSkipsUnhealthy(t *testing.T) {
	rr := NewRoundRobinBalancer()
	backends := makeBackends(3)
	for _, b := range backends {
		rr.AddBackend(b)
	}

	rr.MarkHealthy(1, false)

	results := make(map[string]int)
	for i := 0; i < 6; i++ {
		b := rr.NextBackend()
		if b == nil {
			t.Fatal("NextBackend returned nil")
		}
		results[b.String()]++
	}

	if _, ok := results["localhost:9002"]; ok {
		t.Error("unhealthy backend should not be selected")
	}
	if results["localhost:9001"] != 3 || results["localhost:9003"] != 3 {
		t.Errorf("expected even distribution between healthy backends, got %v", results)
	}
}

func TestRoundRobinAllUnhealthy(t *testing.T) {
	rr := NewRoundRobinBalancer()
	for _, b := range makeBackends(3) {
		rr.AddBackend(b)
	}
	rr.MarkHealthy(0, false)
	rr.MarkHealthy(1, false)
	rr.MarkHealthy(2, false)

	if b := rr.NextBackend(); b != nil {
		t.Errorf("expected nil when all backends unhealthy, got %v", b)
	}
}

func TestWeightedRoundRobinBalancer(t *testing.T) {
	wrr := NewWeightedRoundRobinBalancer()
	for _, b := range makeWeightedBackends() {
		wrr.AddBackend(b)
	}

	results := make(map[string]int)
	for i := 0; i < 50; i++ {
		b := wrr.NextBackend()
		if b == nil {
			t.Fatal("NextBackend returned nil")
		}
		results[b.String()]++
	}

	if results["localhost:9002"] <= results["localhost:9001"] {
		t.Errorf("weighted backend should get more requests: %v", results)
	}
}

func TestWeightedRoundRobinSkipsUnhealthy(t *testing.T) {
	wrr := NewWeightedRoundRobinBalancer()
	backends := makeWeightedBackends()
	for _, b := range backends {
		wrr.AddBackend(b)
	}

	wrr.MarkHealthy(2, false)

	results := make(map[string]int)
	for i := 0; i < 40; i++ {
		b := wrr.NextBackend()
		if b == nil {
			t.Fatal("NextBackend returned nil")
		}
		results[b.String()]++
	}

	if _, ok := results["localhost:9003"]; ok {
		t.Error("unhealthy backend should not be selected")
	}
}

func TestLeastConnectionsBalancer(t *testing.T) {
	lc := NewLeastConnectionsBalancer()
	backends := makeBackends(3)
	for _, b := range backends {
		lc.AddBackend(b)
	}

	lc.IncrementConns(0, 5)
	lc.IncrementConns(1, 2)

	b := lc.NextBackend()
	if b == nil {
		t.Fatal("NextBackend returned nil")
	}

	if b.String() != "localhost:9003" {
		t.Errorf("expected least connections backend localhost:9003, got %s", b.String())
	}
}

func TestLeastConnectionsSkipsUnhealthy(t *testing.T) {
	lc := NewLeastConnectionsBalancer()
	backends := makeBackends(3)
	for _, b := range backends {
		lc.AddBackend(b)
	}

	lc.MarkHealthy(0, false)
	lc.IncrementConns(1, 10)

	b := lc.NextBackend()
	if b == nil {
		t.Fatal("NextBackend returned nil")
	}

	if b.String() != "localhost:9003" {
		t.Errorf("expected localhost:9003, got %s", b.String())
	}
}

func TestRandomBalancer(t *testing.T) {
	rb := NewRandomBalancer()
	for _, b := range makeBackends(3) {
		rb.AddBackend(b)
	}

	results := make(map[string]int)
	for i := 0; i < 1000; i++ {
		b := rb.NextBackend()
		if b == nil {
			t.Fatal("NextBackend returned nil")
		}
		results[b.String()]++
	}

	if len(results) != 3 {
		t.Errorf("expected all 3 backends to appear, got %v", results)
	}
}

func TestRandomSkipsUnhealthy(t *testing.T) {
	rb := NewRandomBalancer()
	backends := makeBackends(3)
	for _, b := range backends {
		rb.AddBackend(b)
	}
	rb.MarkHealthy(1, false)

	for i := 0; i < 100; i++ {
		b := rb.NextBackend()
		if b == nil {
			t.Fatal("NextBackend returned nil")
		}
		if b.String() == "localhost:9002" {
			t.Error("unhealthy backend should not be selected")
		}
	}
}

func TestConcurrentNextBackend(t *testing.T) {
	rr := NewRoundRobinBalancer()
	for _, b := range makeBackends(3) {
		rr.AddBackend(b)
	}

	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b := rr.NextBackend(); b == nil {
				t.Error("NextBackend returned nil")
			}
		}()
	}
	wg.Wait()
}

func TestNewBalancerInvalidAlgorithm(t *testing.T) {
	_, err := NewBalancer("invalid")
	if err == nil {
		t.Error("expected error for invalid algorithm")
	}
}

func TestNewBalancerValidAlgorithms(t *testing.T) {
	for _, algo := range []string{"round_robin", "weighted_round_robin", "least_connections", "random"} {
		bal, err := NewBalancer(algo)
		if err != nil {
			t.Errorf("unexpected error for %s: %v", algo, err)
		}
		if bal == nil {
			t.Errorf("expected non-nil balancer for %s", algo)
		}
	}
}
