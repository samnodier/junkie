package main

import (
	"errors"
	"net/url"
)

// store is what the desk reads and writes. Two implementations sit behind
// the same screen: the cloud (signed in; the server owns the clock) and
// local files (guest; this machine owns the clock). The views do not care
// which, so a sign-in mid-session is a store swap rather than a different
// program.
type store interface {
	Load() (deskResponse, error)
	Do(path string, form url.Values) error
	Profile() (profileResponse, error)
	Events() <-chan signal
	Close()
}

// identity is who this desk is running as. Guest is the unsigned local
// mode: no user id, no rooms, nothing that would need the server.
type identity struct {
	Guest    bool
	User     string
	UserID   string
	Username string
	BaseURL  string
}

func guestIdentity(baseURL string) identity {
	return identity{Guest: true, User: "guest", BaseURL: baseURL}
}

// openDeskSession is how the TUI and the guest-capable one-shots start:
// a stored login talks to the server; otherwise this machine's own files.
func openDeskSession() (store, identity, error) {
	cfg, err := loadConfig()
	if errors.Is(err, errNotLoggedIn) {
		s, err := openLocalStore()
		if err != nil {
			return nil, identity{}, err
		}
		return s, guestIdentity(cfg.BaseURL), nil
	}
	if err != nil {
		return nil, identity{}, err
	}
	c := newClient(cfg)
	me, err := c.me()
	if errors.Is(err, errSessionExpired) {
		return nil, identity{}, err
	}
	id := identity{
		User:     cfg.Username,
		Username: cfg.Username,
		BaseURL:  cfg.BaseURL,
	}
	if err == nil && me.User != nil {
		id.User = firstNonEmpty(me.User.DisplayName, cfg.Username)
		id.UserID = me.User.ID
	}
	return newCloudStore(c), id, nil
}

// errNeedsAccount is what a guest store returns for anything that only
// exists on an account — rooms, mainly. The TUI turns it into a sign-in
// hint rather than a crash.
var errNeedsAccount = errors.New("sign in to use rooms")
