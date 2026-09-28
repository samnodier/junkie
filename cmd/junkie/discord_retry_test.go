package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func retryHarness(base http.RoundTripper) (*discordRetryTransport, *[]time.Duration) {
	transport := newDiscordRetryTransport(base)
	now := time.Now()
	var waits []time.Duration
	transport.now = func() time.Time { return now }
	transport.jitter = func() time.Duration { return 0 }
	transport.wait = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		waits = append(waits, d)
		now = now.Add(d)
		return nil
	}
	return transport, &waits
}

func TestDiscordRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name, header, reset, body string
		want                      time.Duration
	}{
		{name: "body seconds", body: `{"retry_after":1.25}`, want: 1250 * time.Millisecond},
		{name: "header seconds", header: "2.5", want: 2500 * time.Millisecond},
		{name: "longest wins", header: "3", reset: "4", body: `{"retry_after":2}`, want: 4 * time.Second},
		{name: "date", header: now.Add(7 * time.Second).Format(http.TimeFormat), want: 7 * time.Second},
		{name: "zero", body: `{"retry_after":0}`},
		{name: "missing", body: `{"message":"blocked"}`},
		{name: "malformed", header: "invalid", body: `<html>blocked</html>`},
		{name: "negative", header: "-1", body: `{"retry_after":-2}`},
		{name: "non finite", header: "NaN", reset: "+Inf"},
		{name: "huge", header: "1e100", want: time.Duration(1 << 62)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{"Retry-After": {tt.header}, "X-Ratelimit-Reset-After": {tt.reset}}
			if got := discordRetryAfter(h, []byte(tt.body), now); got != tt.want {
				t.Fatalf("delay = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDiscordZeroDelayRetriesAreBounded(t *testing.T) {
	calls := 0
	transport, waits := retryHarness(statusTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if string(body) != "payload" {
				t.Fatalf("retry lost body: %q", body)
			}
		}
		return statusHTTPResponse(429, `{"retry_after":0}`), nil
	}))
	req, _ := http.NewRequest(http.MethodPost, "https://discord.test/api/channels/secret/messages", strings.NewReader("payload"))
	_, err := transport.RoundTrip(req)
	var limit *discordCooldownError
	if !errors.As(err, &limit) || calls != 3 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	if want := []time.Duration{time.Second, 2 * time.Second}; !reflect.DeepEqual(*waits, want) {
		t.Fatalf("waits=%v want=%v", *waits, want)
	}
	if limit.delay != 4*time.Second {
		t.Fatalf("remaining cooldown=%s", limit.delay)
	}
	// A separate request shares the cooldown and does not start over at zero.
	req, _ = http.NewRequest(http.MethodGet, "https://discord.test/api/gateway", nil)
	_, _ = transport.RoundTrip(req)
	if (*waits)[2] != 4*time.Second {
		t.Fatalf("shared cooldown not preserved: %v", *waits)
	}
}

func TestDiscordRetryRecoversAndPreservesResponse(t *testing.T) {
	calls := 0
	transport, waits := retryHarness(statusTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			resp := statusHTTPResponse(429, `{"retry_after":0}`)
			resp.Header.Set("Retry-After", "5")
			return resp, nil
		}
		return statusHTTPResponse(200, `{"id":"created-once"}`), nil
	}))
	req, _ := http.NewRequest(http.MethodGet, "https://discord.test/api/gateway", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if calls != 2 || string(body) != `{"id":"created-once"}` || !reflect.DeepEqual(*waits, []time.Duration{5 * time.Second}) {
		t.Fatalf("calls=%d body=%s waits=%v", calls, body, *waits)
	}
	if transport.failures != 0 {
		t.Fatal("backoff did not reset after recovery")
	}
}

func TestDiscordDoesNotRetryAmbiguousOrPermanentFailures(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 500, 502} {
		calls := 0
		transport, waits := retryHarness(statusTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return statusHTTPResponse(code, "failure"), nil
		}))
		req, _ := http.NewRequest(http.MethodPost, "https://discord.test/api/channels/id/messages", nil)
		resp, err := transport.RoundTrip(req)
		if err != nil || calls != 1 || len(*waits) != 0 || resp.StatusCode != code {
			t.Fatalf("code=%d calls=%d err=%v", code, calls, err)
		}
		resp.Body.Close()
	}
	calls := 0
	transport, _ := retryHarness(statusTransport(func(r *http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF }))
	req, _ := http.NewRequest(http.MethodPost, "https://discord.test/api/channels/id/messages", nil)
	_, err := transport.RoundTrip(req)
	if !errors.Is(err, io.ErrUnexpectedEOF) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestDiscordCooldownHonorsContext(t *testing.T) {
	calls := 0
	transport, waits := retryHarness(statusTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return statusHTTPResponse(429, `{"retry_after":60}`), nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://discord.test/api/gateway", nil)
	_, err := transport.RoundTrip(req)
	var limit *discordCooldownError
	if !errors.As(err, &limit) || calls != 1 || len(*waits) != 0 {
		t.Fatalf("calls=%d waits=%v err=%v", calls, *waits, err)
	}
	// Subsequent callers also fail without sending requests during the cooldown.
	_, err = transport.RoundTrip(req)
	if !errors.As(err, &limit) || calls != 1 {
		t.Fatalf("cooldown bypassed: calls=%d err=%v", calls, err)
	}
	cancel()
	if err := waitDiscordRetry(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
}

func TestDiscordRetryDoesNotReplayStreamingBody(t *testing.T) {
	calls := 0
	transport, _ := retryHarness(statusTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		r.Body.Close()
		return statusHTTPResponse(429, `{}`), nil
	}))
	req, _ := http.NewRequest(http.MethodPost, "https://discord.test/api/channels/id/messages", io.NopCloser(strings.NewReader("stream")))
	_, err := transport.RoundTrip(req)
	if err == nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestDiscordRetrySessionAndSafeErrors(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "test-token")
	t.Setenv("DISCORD_APPLICATION_ID", "app")
	bot, err := newDiscordBot(&app{})
	if err != nil {
		t.Fatal(err)
	}
	if bot.session.ShouldRetryOnRateLimit || bot.session.MaxRestRetries != 0 {
		t.Fatal("library retries still enabled")
	}
	if _, ok := bot.session.Client.Transport.(*discordRetryTransport); !ok {
		t.Fatal("retry transport missing")
	}
	secretURL := "https://discord.test/api/interactions/id/secret-token/callback"
	wrapped := &url.Error{Op: "Post", URL: secretURL, Err: &discordCooldownError{delay: time.Second}}
	if strings.Contains(discordError(wrapped).Error(), "secret-token") {
		t.Fatal("URL token leaked")
	}
	req, _ := http.NewRequest(http.MethodPost, secretURL, nil)
	if discordRequestRoute(req) != "interaction-response" {
		t.Fatal("unsafe route label")
	}
	// Exercise the real discordgo call to prove no library loop wraps the budget.
	calls := 0
	transport, _ := retryHarness(statusTransport(func(r *http.Request) (*http.Response, error) { calls++; return statusHTTPResponse(429, `{}`), nil }))
	bot.session.Client.Transport = transport
	err = bot.session.InteractionRespond(statusInteraction().Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource})
	if err == nil || calls != 3 {
		t.Fatalf("real session calls=%d err=%v", calls, discordError(err))
	}
}

func TestDiscordConcurrentCallersRespectCooldown(t *testing.T) {
	transport := newDiscordRetryTransport(statusTransport(func(r *http.Request) (*http.Response, error) {
		t.Error("request sent during cooldown")
		return statusHTTPResponse(200, "{}"), nil
	}))
	transport.until = time.Now().Add(time.Minute)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://discord.test/api/gateway", nil)
			_, err := transport.RoundTrip(req)
			var limit *discordCooldownError
			if !errors.As(err, &limit) {
				t.Errorf("expected cooldown, got %v", err)
			}
		})
	}
	wg.Wait()
}

func TestDiscordInitialResponseStopsBeforeExpiry(t *testing.T) {
	s := statusTestSession(t, func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 2500*time.Millisecond {
			t.Error("initial reply has no short deadline")
		}
		return statusHTTPResponse(429, `{"retry_after":10}`), nil
	})
	s.ShouldRetryOnRateLimit = false
	calls := 0
	base := s.Client.Transport
	transport := newDiscordRetryTransport(statusTransport(func(r *http.Request) (*http.Response, error) { calls++; return base.RoundTrip(r) }))
	s.Client.Transport = transport
	err := (&discordBot{}).respondInteraction(s, statusInteraction(), &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource})
	var limit *discordCooldownError
	if !errors.As(err, &limit) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, discordError(err))
	}
}
