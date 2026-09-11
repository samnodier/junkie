package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// fastStart is a start payload with a poll interval short enough that a test
// is not waiting on the real two seconds.
func fastStart(userCode string) linkStart {
	return linkStart{
		DeviceCode: "device-token",
		UserCode:   userCode,
		VerifyURL:  "https://example.test/cli",
		ExpiresIn:  60,
		Interval:   1,
	}
}

func TestStartPairingReadsTheCode(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/cli/link/start" {
			t.Errorf("start went to %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deviceCode": "dev-123", "userCode": "WXYZ-2345",
			"verifyUrl": "https://example.test/cli", "expiresIn": 600, "interval": 2,
		})
	})
	got, err := c.startPairing()
	if err != nil {
		t.Fatalf("startPairing: %v", err)
	}
	if got.DeviceCode != "dev-123" || got.UserCode != "WXYZ-2345" {
		t.Errorf("startPairing = %+v", got)
	}
}

// An older server has no pairing endpoints, and `junkie login` falls back to
// asking for a password on exactly this error rather than giving up.
func TestStartPairingOnAnOldServerIsUnsupportedNotAnError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	if _, err := c.startPairing(); !errors.Is(err, errPairingUnsupported) {
		t.Errorf("startPairing against an old server = %v, want errPairingUnsupported", err)
	}
}

func TestStartPairingSurfacesTheServersMessage(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "Too many sign-in attempts."})
	})
	_, err := c.startPairing()
	if err == nil || err.Error() != "Too many sign-in attempts." {
		t.Errorf("startPairing = %v, want the server's own message", err)
	}
}

// The normal path: a few pending polls, then approval.
func TestAwaitApprovalWaitsThenSignsIn(t *testing.T) {
	var polls int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&polls, 1)
		if n < 3 {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "approved", "token": "session-token", "username": "sam",
		})
	})
	out, err := c.awaitApproval(context.Background(), fastStart("WXYZ-2345"), nil)
	if err != nil {
		t.Fatalf("awaitApproval: %v", err)
	}
	if out.Token != "session-token" || out.Username != "sam" {
		t.Errorf("awaitApproval = %+v", out)
	}
	if atomic.LoadInt32(&polls) < 3 {
		t.Errorf("gave up after %d polls", polls)
	}
}

// An expired code stops the wait rather than spinning until the CLI's own
// deadline — the person needs to be told to start again.
func TestAwaitApprovalStopsOnExpiry(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "expired"})
	})
	if _, err := c.awaitApproval(context.Background(), fastStart("WXYZ-2345"), nil); !errors.Is(err, errPairingExpired) {
		t.Errorf("awaitApproval = %v, want errPairingExpired", err)
	}
}

// Ctrl-C, or closing the desk panel, has to actually stop the polling.
func TestAwaitApprovalStopsWhenCancelled(t *testing.T) {
	var polls int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&polls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending"})
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := c.awaitApproval(ctx, fastStart("WXYZ-2345"), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("awaitApproval = %v, want context.Canceled", err)
	}
	before := atomic.LoadInt32(&polls)
	time.Sleep(120 * time.Millisecond)
	if after := atomic.LoadInt32(&polls); after != before {
		t.Errorf("kept polling after cancellation (%d then %d)", before, after)
	}
}

// Being told to slow down is not a failure: back off and keep waiting.
func TestAwaitApprovalSurvivesRateLimiting(t *testing.T) {
	var polls int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&polls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "Polling too fast."})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "approved", "token": "session-token", "username": "sam",
		})
	})
	out, err := c.awaitApproval(context.Background(), fastStart("WXYZ-2345"), nil)
	if err != nil {
		t.Fatalf("a 429 ended the wait: %v", err)
	}
	if out.Token != "session-token" {
		t.Errorf("awaitApproval = %+v", out)
	}
}

// An "approved" with no token is a server bug, and must not be stored as a
// session that cannot work.
func TestAwaitApprovalRejectsApprovalWithoutAToken(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "approved"})
	})
	if _, err := c.awaitApproval(context.Background(), fastStart("WXYZ-2345"), nil); err == nil {
		t.Error("an approval with no token was accepted")
	}
}

// The device code is what proves this terminal opened the request, so every
// poll has to carry it.
func TestPollSendsTheDeviceCode(t *testing.T) {
	var got string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm.Get("device_code")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending"})
	})
	if _, err := c.pollPairing("dev-abc"); err != nil {
		t.Fatal(err)
	}
	if got != "dev-abc" {
		t.Errorf("poll sent device_code %q, want dev-abc", got)
	}
}
