package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSafeNext(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "/"},
		{"/", "/"},
		{"/dashboard", "/dashboard"},
		{"/r/AB-CD?error=x", "/r/AB-CD?error=x"},
		{"https://evil.example", "/"},
		{"//evil.example", "/"},
		{`/\evil.example`, "/"},
		{"evil.example", "/"},
	}
	for _, c := range cases {
		if got := safeNext(c.in); got != c.want {
			t.Errorf("safeNext(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// clientIP must key rate limits on the rightmost X-Forwarded-For entry — the
// one appended by our own proxy — never a leftmost value the client typed
// itself, or every request could mint a fresh rate-limit bucket.
func TestClientIPUsesRightmostForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:4321"
	if got := clientIP(r); got != "10.0.0.1" {
		t.Errorf("no XFF: got %q, want RemoteAddr host", got)
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := clientIP(r); got != "203.0.113.7" {
		t.Errorf("single XFF: got %q", got)
	}
	// The client sent a spoofed leading entry; the proxy appended the real IP.
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8, 203.0.113.7")
	if got := clientIP(r); got != "203.0.113.7" {
		t.Errorf("spoofed XFF chain: got %q, want the proxy-appended value", got)
	}
}

func TestValidUsername(t *testing.T) {
	valid := []string{"sam", "ab", "grand-pa_99", "a.b.c", "0x1"}
	for _, u := range valid {
		if !validUsername(u) {
			t.Errorf("validUsername(%q) = false, want true", u)
		}
	}
	invalid := []string{"", "a", "-leadingdash", ".leadingdot", "UPPER", "has space", "tag<script>", "waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaytoolong"}
	for _, u := range invalid {
		if validUsername(u) {
			t.Errorf("validUsername(%q) = true, want false", u)
		}
	}
}

func TestLimitRunes(t *testing.T) {
	if got := limitRunes("hello", 10); got != "hello" {
		t.Errorf("limitRunes short = %q", got)
	}
	if got := limitRunes("héllo wörld", 5); got != "héllo" {
		t.Errorf("limitRunes multibyte = %q", got)
	}
}

func TestHashToken(t *testing.T) {
	a, b := hashToken("abc"), hashToken("abc")
	if a != b {
		t.Error("hashToken is not deterministic")
	}
	if a == "abc" || len(a) != 64 {
		t.Errorf("hashToken(%q) = %q, want 64-char digest", "abc", a)
	}
	if hashToken("abd") == a {
		t.Error("different tokens must not collide trivially")
	}
}

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter()
	for i := 0; i < 3; i++ {
		if !l.allow("k", 3, time.Minute) {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if l.allow("k", 3, time.Minute) {
		t.Error("attempt over limit should be denied")
	}
	if !l.allow("other", 3, time.Minute) {
		t.Error("separate key should not be affected")
	}
	// Expired window resets the bucket.
	l.buckets["k"] = rateBucket{count: 99, resetAt: time.Now().Add(-time.Second)}
	if !l.allow("k", 3, time.Minute) {
		t.Error("expired window should reset the counter")
	}
}

func TestRateLimiterRefund(t *testing.T) {
	l := newRateLimiter()
	// Use the full quota, then refund one: exactly one more attempt fits.
	for i := 0; i < 2; i++ {
		if !l.allow("k", 2, time.Minute) {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if l.allow("k", 2, time.Minute) {
		t.Fatal("attempt over limit should be denied")
	}
	// The denied attempt above also counted; refund it plus one delivery
	// failure, mirroring the reset-DM path (allow, then the DM fails).
	l.refund("k")
	l.refund("k")
	if !l.allow("k", 2, time.Minute) {
		t.Error("refunded attempt should be allowed again")
	}
	if l.allow("k", 2, time.Minute) {
		t.Error("refund must give back only what was refunded")
	}
	// Refunding unknown or empty buckets must not underflow or panic.
	l.refund("missing")
	l.buckets["z"] = rateBucket{count: 0, resetAt: time.Now().Add(time.Minute)}
	l.refund("z")
	if l.buckets["z"].count != 0 {
		t.Error("refund on empty bucket must not underflow")
	}
}
