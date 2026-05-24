package middleware

import (
	"net/http"
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
	tokens     int
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

// Handler returns the rate limiting middleware
func (rl *RateLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getIP(r)

		if !rl.allow(ip) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// allow checks if a request from the given IP is allowed
func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	now := time.Now()

	if !exists {
		rl.visitors[ip] = &visitor{
			tokens:     rl.rate - 1,
			lastUpdate: now,
		}
		return true
	}

	// Refill tokens based on time elapsed
	elapsed := now.Sub(v.lastUpdate)
	tokensToAdd := int(elapsed / rl.interval * time.Duration(rl.rate))

	v.tokens += tokensToAdd
	if v.tokens > rl.rate {
		v.tokens = rl.rate
	}
	v.lastUpdate = now

	if v.tokens > 0 {
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
	// Check X-Forwarded-For header first
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		return forwarded
	}

	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}
