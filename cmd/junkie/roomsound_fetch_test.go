package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A link is a way to get bytes, not somewhere we'll keep pointing -- and the
// fetch that gets them must not become a way to reach the private network.
func TestCheckPublicURLRefusesTheInternalNetwork(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool // true = allowed
	}{
		{"https://example.com/chime.mp3", true},
		{"http://example.com/chime.mp3", false}, // https only
		{"ftp://example.com/chime.mp3", false},
		{"https://127.0.0.1/chime.mp3", false}, // loopback
		{"https://localhost/x", true},          // name resolution is caught at dial time
		{"https://10.0.0.5/x", false},          // private
		{"https://192.168.1.1/x", false},
		{"https://172.16.0.1/x", false},
		{"https://169.254.169.254/latest/meta-data", false}, // cloud metadata
		{"https://[::1]/x", false},
		{"https://0.0.0.0/x", false},
		{"https:///x", false},
	} {
		u, err := parseURLForTest(tc.url)
		if err != nil {
			if tc.want {
				t.Errorf("%s: unexpected parse error %v", tc.url, err)
			}
			continue
		}
		got := checkPublicURL(u) == nil
		if got != tc.want {
			t.Errorf("checkPublicURL(%s) allowed=%v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8":         true,
		"1.1.1.1":         true,
		"127.0.0.1":       false,
		"10.1.2.3":        false,
		"192.168.0.1":     false,
		"172.20.0.1":      false,
		"169.254.169.254": false,
		"0.0.0.0":         false,
		"::1":             false,
		"224.0.0.1":       false,
		"2606:4700::1111": true,
	} {
		if got := isPublicIP(net.ParseIP(ip)); got != want {
			t.Errorf("isPublicIP(%s) = %v, want %v", ip, got, want)
		}
	}
}

// Even with a public hostname, a host that resolves to a private address must
// be refused -- the check is at dial time, not just on the literal string.
func TestFetchRefusesAHostThatResolvesInternally(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("should never be reached"))
	}))
	defer srv.Close()
	// httptest listens on 127.0.0.1, so this is exactly the case.
	url := strings.Replace(srv.URL, "http://", "https://", 1)
	if _, _, err := fetchSoundFromURL(context.Background(), url); err == nil {
		t.Fatal("a fetch to a loopback address should have been refused")
	}
}

func TestFetchRefusesPlainHTTP(t *testing.T) {
	if _, _, err := fetchSoundFromURL(context.Background(), "http://example.com/x.mp3"); err == nil {
		t.Fatal("plain http should be refused")
	} else if !strings.Contains(err.Error(), "https") {
		t.Errorf("error should say why: %v", err)
	}
}

// parseURLForTest keeps the table above readable.
func parseURLForTest(raw string) (*url.URL, error) {
	return url.Parse(raw)
}

// reachableForTest points the fetcher at a local test server: its address
// check at one, and its TLS trust at the server's self-signed certificate.
func reachableForTest(t *testing.T, srv *httptest.Server) {
	t.Helper()
	previousIP, previousTLS := soundFetchAllowIP, soundFetchTLS
	soundFetchAllowIP = func(net.IP) bool { return true }
	soundFetchTLS = srv.Client().Transport.(*http.Transport).TLSClientConfig
	t.Cleanup(func() {
		soundFetchAllowIP, soundFetchTLS = previousIP, previousTLS
	})
}

func TestFetchStoresTheBytesAndNamesTheFile(t *testing.T) {
	body := wavBytes(2000)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()
	reachableForTest(t, srv)

	data, name, err := fetchSoundFromURL(context.Background(), srv.URL+"/chime%20one.mp3")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !bytes.Equal(data, body) {
		t.Errorf("got %d bytes, want %d", len(data), len(body))
	}
	if name != "chime one.mp3" {
		t.Errorf("name = %q, want the URL's filename, unescaped", name)
	}
	// And what came back is what validation accepts, end to end.
	if _, err := validateRoomSound(data); err != nil {
		t.Errorf("fetched audio failed validation: %v", err)
	}
}

// An enormous file is stopped by the read limit rather than pulled into
// memory whole, and then refused by the same size rule an upload meets.
func TestFetchStopsReadingPastTheSizeCap(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := make([]byte, 64<<10)
		for range 100 { // ~6.4 MB if it were all read
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	reachableForTest(t, srv)

	data, _, err := fetchSoundFromURL(context.Background(), srv.URL+"/huge.wav")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(data) > maxRoomSoundBytes+1 {
		t.Fatalf("read %d bytes, want no more than %d", len(data), maxRoomSoundBytes+1)
	}
	if _, err := validateRoomSound(data); err == nil {
		t.Error("an oversize download must still be refused by validation")
	}
}

// A link that redirects onto the private network is caught on the hop, not
// just on the URL somebody typed.
func TestFetchRefusesARedirectIntoThePrivateNetwork(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer srv.Close()
	// Only TLS trust is relaxed here: the address check stays real, which is
	// what the redirect has to run into.
	previous := soundFetchTLS
	soundFetchTLS = srv.Client().Transport.(*http.Transport).TLSClientConfig
	t.Cleanup(func() { soundFetchTLS = previous })

	if _, _, err := fetchSoundFromURL(context.Background(), srv.URL+"/start.mp3"); err == nil {
		t.Fatal("a redirect into the private network should have been refused")
	}
}
