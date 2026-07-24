package balancer

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/null8878/go-loadbalancer/metrics"
)

// Proxy is an http.Handler that forwards requests to a backend selected by
// the configured load balancing algorithm.
type Proxy struct {
	Balancer Balancer
	Logger   *slog.Logger
}

// ServeHTTP selects a backend and proxies the request. If no healthy backend
// is available it returns 503. It supports WebSocket upgrades and logs every
// request with its chosen backend.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	backend := p.Balancer.NextBackend()
	if backend == nil {
		metrics.RequestCount.WithLabelValues(r.Method, "503", "none").Inc()
		http.Error(w, "service unavailable: no healthy backends", http.StatusServiceUnavailable)
		return
	}

	idx := -1
	for i, b := range p.Balancer.Backends() {
		if b == backend {
			idx = i
			break
		}
	}
	if idx >= 0 {
		p.Balancer.IncrementConns(idx, 1)
		defer p.Balancer.IncrementConns(idx, -1)
	}

	isWebSocket := isUpgradeRequest(r)
	if isWebSocket {
		p.handleWebSocket(w, r, backend)
	} else {
		p.proxyHTTP(w, r, backend, idx)
	}

	elapsed := time.Since(start).Seconds()
	metrics.RequestDuration.WithLabelValues(r.Method, backend.String()).Observe(elapsed)
}

func (p *Proxy) proxyHTTP(w http.ResponseWriter, r *http.Request, backend *Backend, idx int) {
	// Clone the request so we don't mutate the original.
	clone := r.Clone(r.Context())
	clone.URL.Scheme = backend.URL.Scheme
	clone.URL.Host = backend.URL.Host
	clone.Host = backend.URL.Host

	// Remove hop-by-hop headers.
	clone.Header.Del("X-Forwarded-For")
	clone.Header.Del("X-Forwarded-Host")
	clone.Header.Del("X-Real-IP")
	if clientIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		clone.Header.Set("X-Forwarded-For", clientIP)
		clone.Header.Set("X-Real-IP", clientIP)
	}
	clone.Header.Set("X-Forwarded-Host", r.Host)

	backend.ReverseProxy.ServeHTTP(w, clone)

	status := extractStatus(w)
	metrics.RequestCount.WithLabelValues(r.Method, status, backend.String()).Inc()

	if idx >= 0 {
		p.Balancer.ResetFailures(idx)
	}

	p.Logger.Info("proxied request",
		"method", r.Method,
		"path", r.URL.Path,
		"backend", backend.String(),
		"status", status,
	)
}

func (p *Proxy) handleWebSocket(w http.ResponseWriter, r *http.Request, backend *Backend) {
	// Hijack the connection for WebSocket upgrade.
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket not supported", http.StatusInternalServerError)
		return
	}

	// Dial the backend directly.
	targetConn, err := net.DialTimeout("tcp", backend.URL.Host, 5*time.Second)
	if err != nil {
		p.Logger.Error("websocket dial failed", "backend", backend.String(), "error", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	// Forward the original HTTP upgrade request.
	if err := r.Write(targetConn); err != nil {
		targetConn.Close()
		p.Logger.Error("websocket write failed", "error", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		targetConn.Close()
		p.Logger.Error("websocket hijack failed", "error", err)
		return
	}

	p.Logger.Info("websocket upgrade", "backend", backend.String(), "path", r.URL.Path)
	go bidirectionalCopy(clientConn, targetConn)
}

// bidirectionalCopy relays bytes between two connections until one closes.
func bidirectionalCopy(a, b net.Conn) {
	defer a.Close()
	defer b.Close()

	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		buf := make([]byte, 32*1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				if _, wErr := dst.Write(buf[:n]); wErr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}

	go cp(a, b)
	go cp(b, a)
	<-done
}

// isUpgradeRequest checks whether the request is a WebSocket upgrade.
func isUpgradeRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Connection"), "upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func extractStatus(w http.ResponseWriter) string {
	if sr, ok := w.(*statusRecorder); ok && sr.status != 0 {
		return http.StatusText(sr.status)
	}
	return "200"
}
