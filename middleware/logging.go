package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"time"
)

// statusRecorder captures the HTTP status written by downstream handlers.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

// Logging returns middleware that logs every request with method, path,
// status code, latency, and client IP. If color is true the output uses
// ANSI escape codes for the terminal.
func Logging(logger *slog.Logger, color bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			elapsed := time.Since(start)
			clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				clientIP = r.RemoteAddr
			}

			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			} else if rec.status >= 400 {
				level = slog.LevelWarn
			}

			statusStr := colorCode(rec.status, color)

			logger.LogAttrs(r.Context(), level, "request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("status", statusStr),
				slog.Duration("latency", elapsed),
				slog.String("client", clientIP),
			)
		})
	}
}

func colorCode(status int, color bool) string {
	if !color {
		return http.StatusText(status)
	}
	switch {
	case status >= 500:
		return "\033[31m" + http.StatusText(status) + "\033[0m"
	case status >= 400:
		return "\033[33m" + http.StatusText(status) + "\033[0m"
	case status >= 300:
		return "\033[36m" + http.StatusText(status) + "\033[0m"
	default:
		return "\033[32m" + http.StatusText(status) + "\033[0m"
	}
}
