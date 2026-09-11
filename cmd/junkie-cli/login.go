package main

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// loginForm is the in-TUI sign-in. A guest can press L without dropping back
// to the shell; a successful sign-in swaps the store under the same program
// and the desk keeps going.
//
// It asks for nothing. The desk shows a code, the person approves it in a
// browser, and the session arrives here — so a password is never typed into a
// full-screen program that redraws over whatever you type.
type loginForm struct {
	baseURL string
	// start is zero until the server has issued a code.
	start  linkStart
	opened bool // the browser was launched for them
	err    string
	// busy covers both waits: opening the request, and waiting for someone
	// to approve it. Either way the panel takes no input but esc.
	busy bool
	// cancel stops the polling goroutine when the panel is dismissed, so
	// escaping before approval cannot sign someone in a minute later.
	cancel context.CancelFunc
}

func newLoginForm(baseURL, _ string) *loginForm {
	return &loginForm{baseURL: baseURL, busy: true}
}

type loginResultMsg struct {
	store store
	id    identity
	err   error
}

// pairStartedMsg carries the issued code back to the desk.
type pairStartedMsg struct {
	start  linkStart
	opened bool
	err    error
}

// begin opens the pairing request. The desk fires this as soon as the panel
// appears: there is nothing to fill in first, so there is nothing to wait for.
func (f *loginForm) begin() tea.Cmd {
	base := f.baseURL
	return func() tea.Msg {
		c := newClient(config{BaseURL: base})
		start, err := c.startPairing()
		if err != nil {
			return pairStartedMsg{err: err}
		}
		opened := openBrowser(firstNonEmpty(start.VerifyURLFull, start.VerifyURL))
		return pairStartedMsg{start: start, opened: opened}
	}
}

// wait polls until the code is approved, and signs in when it is.
func (f *loginForm) wait() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	base, start := f.baseURL, f.start
	return func() tea.Msg {
		c := newClient(config{BaseURL: base})
		out, err := c.awaitApproval(ctx, start, nil)
		if err != nil {
			return loginResultMsg{err: err}
		}
		return finishSignIn(base, out.Token, out.Username)
	}
}

// dismiss closes the panel and stops any wait behind it.
func (f *loginForm) dismiss() {
	if f.cancel != nil {
		f.cancel()
	}
}

// finishSignIn stores the session and loads the identity behind it. Shared by
// the desk panel and, in spirit, by `junkie login` — both end up here with a
// token and a name.
func finishSignIn(baseURL, token, username string) loginResultMsg {
	cfg := config{BaseURL: baseURL, Token: token, Username: username}
	if err := saveConfig(cfg); err != nil {
		return loginResultMsg{err: err}
	}
	c := newClient(cfg)
	_ = c.setTimezone(time.Local.String())
	me, err := c.me()
	if err != nil {
		return loginResultMsg{err: err}
	}
	if me.User == nil {
		return loginResultMsg{err: errSessionExpired}
	}
	return loginResultMsg{
		store: newCloudStore(c),
		id: identity{
			User:     firstNonEmpty(me.User.DisplayName, username),
			UserID:   me.User.ID,
			Username: username,
			BaseURL:  baseURL,
		},
	}
}

func (f *loginForm) View(width, height int) string {
	if width < 1 {
		width = 40
	}
	if height < 1 {
		height = 12
	}
	body := []string{styleAccent.Render("sign in"), "", styleFaint.Render(clip(f.baseURL, width-4)), ""}

	switch {
	case f.err != "":
		body = append(body, styleDanger.Render(clip(f.err, width-4)), "",
			styleFaint.Render("esc close"))
	case f.start.UserCode == "":
		body = append(body, styleFaint.Render("getting a code…"), "",
			styleFaint.Render("esc stay a guest"))
	default:
		// The code is the thing to read, so it gets the accent and a line of
		// its own; the URL under it is what to do with it.
		body = append(body,
			styleLabel.Render("code"),
			styleAccent.Render(f.start.UserCode),
			"",
			styleInk.Render(clip(f.start.VerifyURL, width-4)),
		)
		if f.opened {
			body = append(body, styleFaint.Render("opened in your browser"))
		} else {
			body = append(body, styleFaint.Render(clip("open on any device — not just this one", width-4)))
		}
		body = append(body, "", styleFaint.Render("waiting for approval… esc stay a guest"))
	}

	card := lipgloss.JoinVertical(lipgloss.Left, body...)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card)
}
