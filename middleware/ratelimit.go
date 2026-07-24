package middleware

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// tokenBucket implements a simple token bucket rate limiter.
type tokenBucket struct {
	rate     float64   // tokens per second
	burst    int       // maximum tokens
	tokens   float64   // current tokens
	lastTime time.Time // last refill time
}

func (tb *tokenBucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.tokens += elapsed * tb.rate
	if tb.tokens > float64(tb.burst) {
		tb.tokens = float64(tb.burst)
	}
	tb.lastTime = now

	if tb.tokens < 1 {
		return false
	}
	tb.tokens--
	return true
}

// rateLimiter tracks per-IP rate limiters.
type rateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*tokenBucket
	rate     float64
	burst    int
}

func newRateLimiter(rate float64, burst int) *rateLimiter {
	return &rateLimiter{
		limiters: make(map[string]*tokenBucket),
		rate:     rate,
		burst:    burst,
	}
}

func (rl *rateLimiter) getLimiter(ip string) *tokenBucket {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	tb, exists := rl.limiters[ip]
	if !exists {
		tb = &tokenBucket{
			rate:     rl.rate,
			burst:    rl.burst,
			tokens:   float64(rl.burst),
			lastTime: time.Now(),
		}
		rl.limiters[ip] = tb
	}
	return tb
}

// RateLimit returns middleware that enforces per-IP token bucket rate limiting.
// Clients that exceed the rate receive 429 Too Many Requests with a Retry-After header.
func RateLimit(rate float64, burst int) func(http.Handler) http.Handler {
	limiter := newRateLimiter(rate, burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			tb := limiter.getLimiter(ip)
			if !tb.allow() {
				retryAfter := int(1.0/rate + 0.5)
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(burst))
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded","retry_after":` + strconv.Itoa(retryAfter) + `}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
