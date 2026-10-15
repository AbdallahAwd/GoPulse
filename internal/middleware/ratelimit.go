package middleware

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements token bucket rate limiting per IP
type RateLimiter struct {
	visitors map[string]*visitor
	mu       sync.RWMutex
	rate     int           // requests per interval
	interval time.Duration // time window
}

type visitor struct {
	tokens     float64
	lastUpdate time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(rate int, interval time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		interval: interval,
	}

	// Cleanup old visitors periodically
	go rl.cleanup()

	return rl
}

// Handler limits a token bucket without trusting proxy-supplied IP headers.
func (rl *RateLimiter) Handler(next http.Handler) http.Handler {
	return rl.handler(getIP)(next)
}

// APIOnlyHandler bounds API traffic before authentication while leaving the
// application shell and recovery assets available, including after a 429.
func (rl *RateLimiter) APIOnlyHandler(next http.Handler) http.Handler {
	limited := rl.Handler(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			limited.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// APIHandler gives signed-in users independent budgets, including users behind
// the same reverse proxy. Authentication must run before this middleware.
func (rl *RateLimiter) APIHandler(next http.Handler) http.Handler {
	return rl.handler(func(r *http.Request) string {
		if user, _ := r.Context().Value(UserContextKey).(string); user != "" {
			return "user:" + user
		}
		return "ip:" + getIP(r)
	})(next)
}

func (rl *RateLimiter) handler(key func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity := key(r)
			if !rl.allow(identity) {
				rl.mu.RLock()
				tokens := rl.visitors[identity].tokens
				rl.mu.RUnlock()
				retry := int(math.Ceil((1 - tokens) * rl.interval.Seconds() / float64(rl.rate)))
				if retry < 1 {
					retry = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Too many requests. Please wait before trying again.", "retry_after": retry})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// allow checks if a request from the given IP is allowed
func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	now := time.Now()

	if !exists {
		rl.visitors[ip] = &visitor{
			tokens:     float64(rl.rate - 1),
			lastUpdate: now,
		}
		return true
	}

	// Refill tokens based on time elapsed
	elapsed := now.Sub(v.lastUpdate)
	tokensToAdd := elapsed.Seconds() / rl.interval.Seconds() * float64(rl.rate)

	v.tokens += tokensToAdd
	if v.tokens > float64(rl.rate) {
		v.tokens = float64(rl.rate)
	}
	v.lastUpdate = now

	if v.tokens >= 1 {
		v.tokens--
		return true
	}

	return false
}

// cleanup removes old visitors periodically
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastUpdate) > time.Hour {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// getIP extracts the IP address from the request
func getIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
