package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// Applies to new passwords only (signup, change, reset) — existing
	// shorter passwords keep working at login. Deliberately light-touch
	// (Sam's call): 6+ characters and not your username, nothing more.
	minPasswordLength = 6
	// bcrypt only reads the first 72 bytes of a password.
	maxPasswordBytes = 72
	maxTodoTextLen   = 500
	maxRoomNameLen   = 80
	maxRequestBody   = 64 << 10
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)

// reservedUsernames are names that would shadow application routes or
// well-known files now that profiles are served at /<username>.
var reservedUsernames = map[string]bool{
	"login": true, "signup": true, "logout": true, "profile": true,
	"todos": true, "todo": true, "todos-fragment": true, "rooms": true,
	"join": true, "admin": true, "healthz": true, "assets": true,
	"avatar": true, "ws": true, "r": true, "dashboard": true, "solo": true,
	"connect": true, "connections": true, "settings": true, "api": true,
	"favicon.ico": true, "robots.txt": true, "sitemap.xml": true,
	"terms": true, "privacy": true, "discord": true, "reset-password": true,
}

func validUsername(username string) bool {
	return usernamePattern.MatchString(username) && !reservedUsernames[username]
}

func limitRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max])
	}
	return s
}

// hashToken derives the value stored in the sessions table. Only the hash
// lives in the database, so a leaked database dump cannot be replayed as
// live session cookies.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Rightmost value: the one appended by our own proxy (Render, or
		// Caddy when self-hosted), which the client cannot choose. Leftmost
		// entries are attacker-supplied, and keying rate limits on them
		// would let a client mint a fresh bucket per request.
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

type rateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]rateBucket
	lastSweep time.Time
}

type rateBucket struct {
	count   int
	resetAt time.Time
}

// rateSweepInterval bounds how often expired buckets are collected.
const rateSweepInterval = time.Minute

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: map[string]rateBucket{}, lastSweep: time.Now()}
}

// allow records one attempt for key and reports whether it stays within
// limit attempts per window (fixed window).
func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	// Sweep on a schedule, not on map size. The size trigger only fired above
	// 10k keys and then rescanned the whole map on *every* subsequent call —
	// and since it deletes nothing while the buckets are still live, a flood
	// of distinct keys turned each rate-limit check into an O(n) scan holding
	// this global lock, which is exactly when the limiter must stay cheap.
	if now.Sub(l.lastSweep) >= rateSweepInterval {
		for k, b := range l.buckets {
			if now.After(b.resetAt) {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[key]
	if !ok || now.After(b.resetAt) {
		l.buckets[key] = rateBucket{count: 1, resetAt: now.Add(window)}
		return true
	}
	b.count++
	l.buckets[key] = b
	return b.count <= limit
}

// refund gives back one attempt previously recorded by allow, for callers
// whose gated action failed after the check (e.g. a reset link that could
// not be DM'd) — the user shouldn't lose quota for a delivery they never got.
func (l *rateLimiter) refund(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok || b.count == 0 {
		return
	}
	b.count--
	l.buckets[key] = b
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src https://fonts.gstatic.com; "+
				"img-src 'self' data:; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"form-action 'self'; "+
				"base-uri 'self'")
		if isSecureRequest(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		next.ServeHTTP(w, r)
	})
}
