package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Importing a sound from a link.
//
// The link is a way to get the bytes, never a reference we keep: we fetch
// once, validate, and store what came back. Pointing playback at somebody
// else's URL would mean the file could be swapped for something else after
// it was approved, it would vanish when their host did, and the page's
// default-src 'self' would refuse to play it anyway.
const (
	soundFetchTimeout   = 10 * time.Second
	soundFetchRedirects = 3
)

// safeFetchClient refuses to connect to anything on a private, loopback or
// link-local address, checked at dial time so a hostname that resolves to one
// is caught as surely as a literal address -- and checked again on every
// redirect hop, since only the first URL is the one anybody looked at.
func safeFetchClient() *http.Client {
	dialer := &net.Dialer{Timeout: soundFetchTimeout}
	return &http.Client{
		Timeout: soundFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= soundFetchRedirects {
				return errors.New("too many redirects")
			}
			return checkPublicURL(req.URL)
		},
		Transport: &http.Transport{
			TLSClientConfig: soundFetchTLS,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				for _, ip := range ips {
					if !soundFetchAllowIP(ip.IP) {
						return nil, fmt.Errorf("refusing to connect to %s", ip.IP)
					}
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}
}

// soundFetchAllowIP decides which addresses the fetcher may connect to, and
// soundFetchTLS is the TLS config it dials with. Both are variables only so
// tests can point the fetcher at a local server with a self-signed
// certificate; production code never reassigns either.
var (
	soundFetchAllowIP = isPublicIP
	soundFetchTLS     *tls.Config
)

// isPublicIP reports whether an address is one we're willing to fetch from.
func isPublicIP(ip net.IP) bool {
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() &&
		!ip.IsInterfaceLocalMulticast() && !ip.IsMulticast()
}

// checkPublicURL rejects a URL before any connection is attempted.
func checkPublicURL(u *url.URL) error {
	if u == nil {
		return errors.New("that isn't a link I can fetch")
	}
	// HTTPS only: a plain-HTTP fetch could be redirected or rewritten in
	// transit into something else entirely.
	if !strings.EqualFold(u.Scheme, "https") {
		return errors.New("the link has to be https")
	}
	if u.Hostname() == "" {
		return errors.New("that isn't a link I can fetch")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !soundFetchAllowIP(ip) {
		return errors.New("that address isn't reachable from here")
	}
	return nil
}

// fetchSoundFromURL downloads at most maxRoomSoundBytes+1 from a public URL,
// so an enormous file is refused by the same size rule as an upload rather
// than by being streamed into memory first.
func fetchSoundFromURL(ctx context.Context, rawURL string) ([]byte, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, "", errors.New("that isn't a link I can fetch")
	}
	if err := checkPublicURL(parsed); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", errors.New("that isn't a link I can fetch")
	}
	res, err := safeFetchClient().Do(req)
	if err != nil {
		return nil, "", errors.New("couldn't download that link")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("that link answered %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxRoomSoundBytes+1))
	if err != nil {
		return nil, "", errors.New("couldn't download that link")
	}
	// The name is only ever a label; the bytes are what get validated.
	name := "sound"
	if p := strings.Trim(parsed.EscapedPath(), "/"); p != "" {
		if i := strings.LastIndex(p, "/"); i >= 0 {
			p = p[i+1:]
		}
		if unescaped, err := url.PathUnescape(p); err == nil && unescaped != "" {
			name = unescaped
		}
	}
	return data, name, nil
}
