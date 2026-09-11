package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Browser pairing: the terminal shows a short code, you approve it in a
// browser that is already signed in, and the terminal collects a session.
//
// It replaces typing a password into the terminal, which meant having the
// password to hand and handing a credential to a program that never needed to
// see it. The approving browser does not have to be on this machine, so this
// also works on a box reached over SSH -- read the code, approve it on your
// laptop or your phone.

// linkStart is what the server hands back when a pairing request opens.
type linkStart struct {
	DeviceCode    string `json:"deviceCode"`
	UserCode      string `json:"userCode"`
	VerifyURL     string `json:"verifyUrl"`
	VerifyURLFull string `json:"verifyUrlFull"`
	ExpiresIn     int    `json:"expiresIn"`
	Interval      int    `json:"interval"`
}

type linkPoll struct {
	Status   string `json:"status"`
	Token    string `json:"token"`
	Username string `json:"username"`
}

// errPairingUnsupported means the server predates this flow. It is worth
// telling apart so `junkie login` can fall back to asking for a password
// against an older or self-hosted server rather than simply failing.
var errPairingUnsupported = errors.New("this server does not support browser sign-in")

// errPairingExpired is the code running out before anyone approved it.
var errPairingExpired = errors.New("the sign-in code expired before it was approved")

// postJSON sends a form and decodes a JSON reply. The pairing endpoints
// answer JSON rather than the redirects the rest of the API uses, because the
// caller here has no session for a redirect to be about.
func (c *client) postJSON(path string, form url.Values, out any) (int, error) {
	req, err := c.newRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, reachError(c.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return resp.StatusCode, errPairingUnsupported
	}
	if out != nil {
		// A body that will not decode is worth reporting as the status it
		// came with, not as a JSON error nobody can act on.
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && resp.StatusCode < 400 {
			return resp.StatusCode, fmt.Errorf("unexpected reply from %s", c.baseURL)
		}
	}
	return resp.StatusCode, nil
}

// startPairing opens a pairing request.
func (c *client) startPairing() (linkStart, error) {
	// The success body and the error body arrive on the same endpoint, so
	// both shapes are decoded at once and whichever is populated is the
	// answer. It saves reading the body twice to find out which one it was.
	var out struct {
		linkStart
		Error string `json:"error"`
	}
	status, err := c.postJSON("/api/cli/link/start", nil, &out)
	if err != nil {
		return linkStart{}, err
	}
	if status != http.StatusOK || out.DeviceCode == "" || out.UserCode == "" {
		if out.Error != "" {
			return linkStart{}, errors.New(out.Error)
		}
		return linkStart{}, fmt.Errorf("could not start sign-in (server said %d)", status)
	}
	return out.linkStart, nil
}

// pollPairing asks once whether the request has been approved.
func (c *client) pollPairing(deviceCode string) (linkPoll, error) {
	var out linkPoll
	status, err := c.postJSON("/api/cli/link/poll", url.Values{"device_code": {deviceCode}}, &out)
	if err != nil {
		return linkPoll{}, err
	}
	if status == http.StatusTooManyRequests {
		// Not fatal: back off and let the caller try again.
		return linkPoll{Status: "pending"}, nil
	}
	if status != http.StatusOK {
		return linkPoll{}, fmt.Errorf("could not check sign-in (server said %d)", status)
	}
	return out, nil
}

// awaitApproval polls until the code is approved, expires, or ctx is done.
// progress is called once per attempt so a caller can show something moving;
// it may be nil.
func (c *client) awaitApproval(ctx context.Context, start linkStart, progress func()) (linkPoll, error) {
	interval := time.Duration(start.Interval) * time.Second
	if interval < time.Second {
		interval = 2 * time.Second
	}
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	if start.ExpiresIn <= 0 {
		deadline = time.Now().Add(10 * time.Minute)
	}
	for {
		select {
		case <-ctx.Done():
			return linkPoll{}, ctx.Err()
		case <-time.After(interval):
		}
		if progress != nil {
			progress()
		}
		out, err := c.pollPairing(start.DeviceCode)
		if err != nil {
			return linkPoll{}, err
		}
		switch out.Status {
		case "approved":
			if out.Token == "" {
				return linkPoll{}, errors.New("server approved the sign-in but sent no session")
			}
			return out, nil
		case "expired":
			return linkPoll{}, errPairingExpired
		}
		if time.Now().After(deadline) {
			return linkPoll{}, errPairingExpired
		}
	}
}

// openBrowser tries to put the approval page in front of the person. It is a
// convenience, never a requirement: the URL is always printed, because this
// may be a machine with no browser on it at all -- which is precisely when
// approving from another device matters.
func openBrowser(target string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return false
		}
		cmd = exec.Command("xdg-open", target)
	}
	// Detached and ignored: a browser that writes to stderr must not scribble
	// over the code the person is reading.
	if err := cmd.Start(); err != nil {
		return false
	}
	go func() { _ = cmd.Wait() }()
	return true
}
