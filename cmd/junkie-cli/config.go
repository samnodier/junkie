package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultBaseURL is where `junkie login` points when nothing else says
// otherwise. Overridable per-login with --url, or per-command with
// JUNKIE_URL, which is what running against a local server needs.
const defaultBaseURL = "https://junkie-blin.onrender.com"

// config is everything `junkie login` leaves behind: which server, and the
// session that proves who you are.
//
// Token is a live session cookie — byte for byte the value a browser holds —
// so anyone who reads this file is signed in as you until it expires (30
// days) or `junkie logout` deletes it server-side. It is written 0600 inside
// a 0700 directory for that reason, and never logged.
type config struct {
	BaseURL  string `json:"baseUrl"`
	Token    string `json:"token"`
	Username string `json:"username"`
}

var errNotLoggedIn = errors.New("not signed in — run `junkie login`")

// configPath follows the XDG basedir spec, which is also where a Linux user
// expects to find (and delete) a tool's credentials.
func configPath() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("JUNKIE_CONFIG")); dir != "" {
		return dir, nil
	}
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "junkie", "config.json"), nil
}

// loadConfig reads the stored session. A missing file is not an error worth
// distinguishing from a logged-out one: both mean "run junkie login".
func loadConfig() (config, error) {
	path, err := configPath()
	if err != nil {
		return config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config{BaseURL: baseURLFromEnv(defaultBaseURL)}, errNotLoggedIn
	}
	if err != nil {
		return config{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	// The environment wins over the stored value so one login can be pointed
	// at a local server for a single command without rewriting the file.
	cfg.BaseURL = baseURLFromEnv(cfg.BaseURL)
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.Token == "" {
		return cfg, errNotLoggedIn
	}
	return cfg, nil
}

func baseURLFromEnv(fallback string) string {
	if v := strings.TrimSpace(os.Getenv("JUNKIE_URL")); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return strings.TrimSuffix(fallback, "/")
}

// saveConfig writes the session atomically: a rename over the real path, so
// an interrupted write can never leave a half-written token behind that
// would read as a corrupt config on the next command.
func saveConfig(cfg config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("save %s: %w", path, err)
	}
	return nil
}

// clearConfig removes the stored session. Deleting the file is the local
// half of logging out; the server-side half is POST /logout, which is what
// actually revokes the token.
func clearConfig() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
