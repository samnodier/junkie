package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const discordMaxAttempts = 3

// discordRetryTransport owns HTTP 429 retries. discordgo's recursive retries
// must be disabled: a missing/zero retry_after otherwise creates a tight loop.
// A conservative session-wide cooldown also covers malformed/proxy 429s whose
// rate-limit scope is unknown. It may delay unrelated routes, but prevents
// concurrent commands and live-message refreshes from amplifying a limit.
type discordRetryTransport struct {
	base     http.RoundTripper
	mu       sync.Mutex
	until    time.Time
	failures int
	now      func() time.Time
	wait     func(context.Context, time.Duration) error
	jitter   func() time.Duration
}

func newDiscordRetryTransport(base http.RoundTripper) *discordRetryTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &discordRetryTransport{
		base: base, now: time.Now, wait: waitDiscordRetry,
		jitter: func() time.Duration { return time.Duration(rand.Int64N(int64(250 * time.Millisecond))) },
	}
}

func waitDiscordRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (t *discordRetryTransport) waitCooldown(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		t.mu.Lock()
		delay := t.until.Sub(t.now())
		t.mu.Unlock()
		if delay <= 0 {
			return nil
		}
		if deadline, ok := ctx.Deadline(); ok && !t.now().Add(delay).Before(deadline) {
			return &discordCooldownError{delay: delay}
		}
		if err := t.wait(ctx, delay); err != nil {
			return err
		}
		// Another request may have extended the shared cooldown while we waited.
	}
}

type discordCooldownError struct{ delay time.Duration }

func (e *discordCooldownError) Error() string {
	return fmt.Sprintf("Discord rate limited; retry stopped, cooldown remaining %s", e.delay.Round(time.Millisecond))
}

func (t *discordRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 1; attempt <= discordMaxAttempts; attempt++ {
		if err := t.waitCooldown(req.Context()); err != nil {
			if attempt == 1 && req.Body != nil {
				req.Body.Close()
			}
			return nil, err
		}
		current := req.Clone(req.Context())
		if attempt > 1 && req.Body != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			current.Body = body
		}
		resp, err := t.base.RoundTrip(current)
		// Retry only explicit rejection (429), never ambiguous network/5xx errors
		// which could follow a successful message creation.
		if err != nil {
			return resp, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				t.mu.Lock()
				if !t.now().Before(t.until) {
					t.failures = 0
				}
				t.mu.Unlock()
			}
			return resp, nil
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		delay := discordRetryAfter(resp.Header, body, t.now())
		t.mu.Lock()
		if t.failures < 6 {
			t.failures++
		}
		fallback := time.Second << (t.failures - 1)
		if fallback > 30*time.Second {
			fallback = 30 * time.Second
		}
		// A positive server delay is a minimum. Missing/zero/invalid delays use
		// increasing backoff instead of being interpreted as permission to spin.
		if delay <= 0 {
			delay = fallback
		}
		delay += t.jitter()
		next := t.now().Add(delay)
		if next.After(t.until) {
			t.until = next
		}
		delay = t.until.Sub(t.now())
		t.mu.Unlock()
		log.Printf("discord: REST rate limited route=%s attempt=%d/%d cooldown=%s", discordRequestRoute(req), attempt, discordMaxAttempts, delay.Round(time.Millisecond))
		if attempt == discordMaxAttempts || (req.Body != nil && req.GetBody == nil) {
			return nil, &discordCooldownError{delay: delay}
		}
	}
	panic("unreachable")
}

// Prefer the longest valid delay so conflicting body/header values never
// cause an early retry. Discord uses seconds, including fractional seconds.
func discordRetryAfter(h http.Header, body []byte, now time.Time) time.Duration {
	var result time.Duration
	useSeconds := func(seconds float64) {
		if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
			return
		}
		// Bound conversion to prevent duration overflow, never wrap to a negative
		// delay and retry early on unexpectedly large server values.
		max := time.Duration(1<<63 - 1)
		delay := max
		if seconds < float64(max)/float64(time.Second) {
			delay = time.Duration(seconds * float64(time.Second))
		}
		if delay > result {
			result = delay
		}
	}
	if seconds, err := strconv.ParseFloat(h.Get("Retry-After"), 64); err == nil {
		useSeconds(seconds)
	} else if date, err := http.ParseTime(h.Get("Retry-After")); err == nil {
		useSeconds(date.Sub(now).Seconds())
	}
	if seconds, err := strconv.ParseFloat(h.Get("X-RateLimit-Reset-After"), 64); err == nil {
		useSeconds(seconds)
	}
	var data struct {
		RetryAfter float64 `json:"retry_after"`
	}
	if json.Unmarshal(body, &data) == nil {
		useSeconds(data.RetryAfter)
	}
	// Leave headroom for jitter and time arithmetic for absurd server values.
	if result > time.Duration(1<<62) {
		return time.Duration(1 << 62)
	}
	return result
}

// Never include IDs or tokens from webhook/interaction URLs in logs.
func discordRequestRoute(req *http.Request) string {
	path := req.URL.Path
	switch {
	case strings.Contains(path, "/interactions/"):
		return "interaction-response"
	case strings.Contains(path, "/webhooks/"):
		return "webhook"
	case strings.Contains(path, "/commands"):
		return "command-registration"
	case strings.Contains(path, "/channels/"):
		return "channel"
	case strings.Contains(path, "/gateway"):
		return "gateway"
	default:
		return "other"
	}
}

// net/http wraps transport failures with the full URL, which may contain an
// interaction token. The error cause is sufficient for operational logging.
func discordError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return discordError(urlErr.Err)
	}
	var limit *discordgo.RateLimitError
	if errors.As(err, &limit) {
		return &discordCooldownError{delay: limit.RetryAfter}
	}
	return err
}
