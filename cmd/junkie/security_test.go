package main

import (
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
