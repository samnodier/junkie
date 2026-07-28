package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"strings"
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

// Expired buckets must be collected on a schedule. The old sweep only ran
// above 10k keys and deleted nothing while buckets were still live, so it
// rescanned the whole map on every single call once it crossed that line.
func TestRateLimiterSweepsExpiredBuckets(t *testing.T) {
	l := newRateLimiter()
	for i := range 50 {
		l.buckets[fmt.Sprintf("stale-%d", i)] = rateBucket{count: 1, resetAt: time.Now().Add(-time.Hour)}
	}
	l.buckets["live"] = rateBucket{count: 1, resetAt: time.Now().Add(time.Hour)}

	// No sweep is due yet, so the stale keys are still resident.
	l.allow("k", 5, time.Minute)
	if len(l.buckets) < 51 {
		t.Fatalf("swept before the interval elapsed: %d buckets left", len(l.buckets))
	}

	// Backdate the last sweep so the next call is due one.
	l.lastSweep = time.Now().Add(-2 * rateSweepInterval)
	l.allow("k", 5, time.Minute)
	if _, ok := l.buckets["live"]; !ok {
		t.Error("sweep dropped a bucket whose window is still open")
	}
	for i := range 50 {
		if _, ok := l.buckets[fmt.Sprintf("stale-%d", i)]; ok {
			t.Fatalf("expired bucket stale-%d survived the sweep", i)
		}
	}
}

// The CSP hashes every inline script the page ships so script-src no longer
// needs 'unsafe-inline'. A miss here is silent in the worst way — the browser
// refuses the script and the theme flash or the service worker quietly stops
// working — so pin the extraction rules.
func TestInlineScriptHashes(t *testing.T) {
	html := []byte(`<!doctype html>
<head>
  <script>
    var theme = 'dark';
  </script>
  <script type="module" crossorigin src="/app/assets/index-ABC.js"></script>
  <script SRC="/other.js"></script>
  <script></script>
  <script>register()</script>
</head>`)

	got := inlineScriptHashes(html)
	if len(got) != 2 {
		t.Fatalf("expected 2 inline scripts hashed (src and empty ones skipped), got %d: %v", len(got), got)
	}
	for _, h := range got {
		if !strings.HasPrefix(h, "'sha256-") || !strings.HasSuffix(h, "'") {
			t.Errorf("malformed CSP source expression: %s", h)
		}
	}

	// The hash must cover the element body exactly as written: a browser
	// hashes the raw text, so any trimming or re-indentation here would
	// produce a policy that rejects the very script it was built from.
	sum := sha256.Sum256([]byte("register()"))
	want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if got[1] != want {
		t.Errorf("body hash = %s, want %s (hash must be over the exact body)", got[1], want)
	}

	// Distinct bodies must not collide, or one edit would silently authorize
	// the other script too.
	if got[0] == got[1] {
		t.Error("different script bodies produced the same hash")
	}

	if h := inlineScriptHashes([]byte("<p>no scripts here</p>")); len(h) != 0 {
		t.Errorf("expected no hashes for script-free html, got %v", h)
	}
}

// buildScriptSrc must never emit 'unsafe-inline' — that was the finding this
// replaced — and must always allow same-origin bundles.
func TestScriptSrcDirective(t *testing.T) {
	if strings.Contains(scriptSrc, "unsafe-inline") {
		t.Errorf("script-src still allows unsafe-inline: %s", scriptSrc)
	}
	if !strings.HasPrefix(scriptSrc, "'self'") {
		t.Errorf("script-src must allow same-origin bundles, got: %s", scriptSrc)
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
