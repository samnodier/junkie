package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func tempConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "junkie", "config.json")
	t.Setenv("JUNKIE_CONFIG", path)
	t.Setenv("JUNKIE_URL", "")
	return path
}

func TestConfigRoundTrip(t *testing.T) {
	path := tempConfig(t)
	want := config{BaseURL: "https://example.test", Token: "sekrit", Username: "sam"}
	if err := saveConfig(want); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got != want {
		t.Errorf("loaded %+v, saved %+v", got, want)
	}

	// The token is a live session cookie: anyone who can read the file is
	// signed in as this user until it expires.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600", perm)
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("config directory mode = %o, want 700", perm)
	}
}

func TestLoadConfigMissingIsNotLoggedIn(t *testing.T) {
	tempConfig(t)
	cfg, err := loadConfig()
	if !errors.Is(err, errNotLoggedIn) {
		t.Fatalf("error = %v, want errNotLoggedIn", err)
	}
	// Even with nothing stored, the default server is filled in so
	// `junkie login` has somewhere to go.
	if cfg.BaseURL != defaultBaseURL {
		t.Errorf("base URL = %q, want the default", cfg.BaseURL)
	}
}

// A file with no token is the same situation as no file: the stored session
// is unusable either way.
func TestLoadConfigWithoutTokenIsNotLoggedIn(t *testing.T) {
	path := tempConfig(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"baseUrl":"https://example.test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(); !errors.Is(err, errNotLoggedIn) {
		t.Errorf("error = %v, want errNotLoggedIn", err)
	}
}

// JUNKIE_URL is what points one command at a local server without
// rewriting — or losing — the stored login.
func TestEnvURLOverridesStored(t *testing.T) {
	tempConfig(t)
	if err := saveConfig(config{BaseURL: "https://production.test", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JUNKIE_URL", "http://localhost:8080/")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	// Trailing slash trimmed: every path the client builds starts with one,
	// and "//api/desk" is a different route.
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("base URL = %q", cfg.BaseURL)
	}
	if cfg.Token != "t" {
		t.Errorf("the stored token should survive a URL override, got %q", cfg.Token)
	}
}

func TestClearConfig(t *testing.T) {
	path := tempConfig(t)
	if err := saveConfig(config{BaseURL: "https://example.test", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := clearConfig(); err != nil {
		t.Fatalf("clearConfig: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("config still present after clear: %v", err)
	}
	// Logging out twice is not an error.
	if err := clearConfig(); err != nil {
		t.Errorf("clearing an absent config: %v", err)
	}
}

// An interrupted write must not leave a half-written token that reads as a
// corrupt config on the next command, so the save renames into place.
func TestSaveConfigLeavesNoTempFile(t *testing.T) {
	path := tempConfig(t)
	if err := saveConfig(config{BaseURL: "https://example.test", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file left behind: %v", err)
	}
}
