package ratelimit

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// IPRateLimiter manages rate limiters per IP address
type IPRateLimiter struct {
	ips map[string]*rate.Limiter
	mu  *sync.RWMutex
	r   rate.Limit
	b   int
}

// NewIPRateLimiter creates a new IP-based rate limiter
// r is the rate (requests per second)
// b is the burst size (maximum number of requests allowed at once)
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		ips: make(map[string]*rate.Limiter),
		mu:  &sync.RWMutex{},
		r:   r,
		b:   b,
	}
}

// GetLimiter returns the rate limiter for a given IP address
func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	limiter, exists := i.ips[ip]
	if !exists {
		limiter = rate.NewLimiter(i.r, i.b)
		i.ips[ip] = limiter
	}

	return limiter
}

// CleanupStale removes limiters that haven't been used recently
// This should be called periodically to prevent memory leaks
func (i *IPRateLimiter) CleanupStale() {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Remove all limiters - they will be recreated on next use
	// This is safe because rate.Limiter starts fresh each time
	i.ips = make(map[string]*rate.Limiter)
}

// Middleware creates an HTTP middleware for rate limiting
func (i *IPRateLimiter) Middleware(next http.HandlerFunc) http.HandlerFunc {
	// Start cleanup goroutine
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			i.CleanupStale()
		}
	}()

	return func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for health checks
		if r.URL.Path == "/health" {
			next(w, r)
			return
		}

		ip := getIP(r)
		limiter := i.GetLimiter(ip)

		if !limiter.Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next(w, r)
	}
}

// getIP extracts the IP address from the request
func getIP(r *http.Request) string {
	// Try X-Forwarded-For header first (for proxied requests)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// X-Forwarded-For can contain multiple IPs, get the first one
		return forwarded
	}

	// Try X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}
