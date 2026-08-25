package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// loginForm is the in-TUI sign-in. A guest can press L without dropping
// back to the shell; a successful login swaps the store under the same
// program and the desk keeps going.
type loginForm struct {
	baseURL  string
	username []rune
	password []rune
	field    int // 0 username, 1 password
	busy     bool
	err      string
}

func newLoginForm(baseURL, username string) *loginForm {
	f := &loginForm{baseURL: baseURL}
	if username != "" {
		f.username = []rune(username)
		f.field = 1
	}
	return f
}

type loginResultMsg struct {
	store store
	id    identity
	err   error
}

func (f *loginForm) value(field int) string {
	if field == 0 {
		return strings.TrimSpace(string(f.username))
	}
	return string(f.password)
}

func (f *loginForm) insert(runes []rune) {
	if f.field == 0 {
		f.username = append(f.username, runes...)
		return
	}
	f.password = append(f.password, runes...)
}

func (f *loginForm) backspace() {
	if f.field == 0 && len(f.username) > 0 {
		f.username = f.username[:len(f.username)-1]
		return
	}
	if f.field == 1 && len(f.password) > 0 {
		f.password = f.password[:len(f.password)-1]
	}
}

func (f *loginForm) submit() tea.Cmd {
	user := strings.ToLower(strings.TrimSpace(string(f.username)))
	pass := string(f.password)
	base := f.baseURL
	if user == "" {
		f.err = "a username is required"
		return nil
	}
	if pass == "" {
		f.err = "a password is required"
		return nil
	}
	f.busy = true
	f.err = ""
	return func() tea.Msg {
		return signIn(base, user, pass)
	}
}

func signIn(baseURL, username, password string) loginResultMsg {
	c := newClient(config{BaseURL: baseURL})
	token, err := c.login(username, password)
	if err != nil {
		return loginResultMsg{err: err}
	}
	cfg := config{BaseURL: baseURL, Token: token, Username: username}
	if err := saveConfig(cfg); err != nil {
		return loginResultMsg{err: err}
	}
	c = newClient(cfg)
	_ = c.setTimezone(time.Local.String())
	me, err := c.me()
	if err != nil {
		return loginResultMsg{err: err}
	}
	if me.User == nil {
		return loginResultMsg{err: errSessionExpired}
	}
	id := identity{
		User:     firstNonEmpty(me.User.DisplayName, username),
		UserID:   me.User.ID,
		Username: username,
		BaseURL:  baseURL,
	}
	return loginResultMsg{store: newCloudStore(c), id: id}
}

func (f *loginForm) View(width, height int) string {
	if width < 1 {
		width = 40
	}
	if height < 1 {
		height = 12
	}
	title := styleAccent.Render("sign in")
	server := styleFaint.Render(clip(f.baseURL, width-4))
	userLine := fieldLine("username", string(f.username), f.field == 0, false, width)
	passShown := strings.Repeat("•", len(f.password))
	passLine := fieldLine("password", passShown, f.field == 1, true, width)

	var body []string
	body = append(body, title, "", server, "", userLine, passLine)
	if f.busy {
		body = append(body, "", styleFaint.Render("signing in…"))
	} else if f.err != "" {
		body = append(body, "", styleDanger.Render(clip(f.err, width-4)))
	}
	body = append(body, "", styleFaint.Render(clip("enter sign in · esc stay a guest · tab next field", width-2)))
	card := lipgloss.JoinVertical(lipgloss.Left, body...)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card)
}

func fieldLine(label, value string, active, _ bool, width int) string {
	cursor := ""
	if active {
		cursor = styleAccent.Render("▌")
	}
	name := styleLabel.Render(fmt.Sprintf("%-10s", label))
	text := styleInk.Render(clip(value, width-16))
	if active {
		return styleAccent.Render("› ") + name + " " + text + cursor
	}
	return "  " + name + " " + text + cursor
}
