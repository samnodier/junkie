package main

import (
	"html/template"
	"strings"
	"time"
)

func parseTemplates() *template.Template {
	funcs := template.FuncMap{
		"secondsUntil": func(t time.Time) int {
			seconds := int(time.Until(t).Seconds())
			if seconds < 0 {
				return 0
			}
			return seconds
		},
		"mul": func(a, b int) int { return a * b },
		"join": strings.Join,
		"focusHours": func(minutes int) int {
			return (minutes + 30) / 60
		},
	}
	return template.Must(template.New("junkie").Funcs(funcs).Parse(layoutTemplates))
}

const layoutTemplates = `
{{define "shell"}}
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} · junkie</title>
  <script>
    (function () {
      var k = 'junkie:theme';
      var t = localStorage.getItem(k);
      if (t !== 'light' && t !== 'dark') {
        t = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
      }
      document.documentElement.setAttribute('data-theme', t);
    })();
  </script>
  <link rel="stylesheet" href="/assets/app.css">
  <script src="https://unpkg.com/htmx.org@2.0.4"></script>
</head>
<body>
  <header class="topbar">
    <a class="brand" href="/" aria-label="junkie home">
      <span class="brand-mark">j</span>
      <span>junkie</span>
    </a>
    <div class="topbar-actions">
      <button type="button" class="theme-toggle" id="theme-toggle" aria-label="Toggle theme">
        <span class="theme-icon theme-icon-light" aria-hidden="true">Light</span>
        <span class="theme-icon theme-icon-dark" aria-hidden="true">Dark</span>
      </button>
      {{if eq .Title "Dashboard"}}
        {{if .User.ID}}
          <nav class="nav nav-compact">
            <span>{{.User.DisplayName}}</span>
            <form method="post" action="/logout"><button class="link-button">Log out</button></form>
          </nav>
        {{end}}
        <button type="button" class="menu-drawer-trigger" aria-label="Open menu" aria-expanded="false">
          <span class="menu-bar" aria-hidden="true"></span>
          <span class="menu-bar" aria-hidden="true"></span>
          <span class="menu-bar" aria-hidden="true"></span>
        </button>
      {{else if .User.ID}}
        <nav class="nav">
          <span>{{.User.DisplayName}}</span>
          <form method="post" action="/logout"><button class="link-button">Log out</button></form>
        </nav>
      {{end}}
    </div>
  </header>
  <main class="page">
    {{if .Error}}<p class="notice">{{.Error}}</p>{{end}}
    {{template "content" .}}
  </main>
  {{if eq .Title "Dashboard"}}
    {{if .GuestMode}}
      {{template "menu-drawer-guest" .}}
    {{else}}
      {{template "menu-drawer-user" .}}
    {{end}}
  {{end}}
  <script>
    (function () {
      const CIRC = 2 * Math.PI * 88;
      const clampMinutes = (n) => Math.min(180, Math.max(5, n));

      const setRing = (timer, ratio) => {
        const ring = timer?.querySelector('.circle-timer-progress');
        if (!ring) return;
        const r = Math.max(0, Math.min(1, ratio));
        ring.style.strokeDashoffset = String(CIRC * (1 - r));
      };

      const wireIdleTimer = (form) => {
        const timer = form.querySelector('.circle-timer.idle');
        const input = form.querySelector('input[name="focus_minutes"]');
        if (!timer || !input) return;

        const syncDigits = () => {
          const label = input.closest('.circle-timer-time');
          label?.classList.toggle('digits-3', String(input.value).length >= 3);
        };

        const syncRing = () => {
          const minutes = clampMinutes(Number(input.value) || 50);
          input.value = minutes;
          syncDigits();
          setRing(timer, minutes / 180);
        };

        timer.querySelectorAll('.circle-timer-step').forEach((btn) => {
          btn.addEventListener('click', (event) => {
            event.preventDefault();
            event.stopPropagation();
            input.value = clampMinutes(Number(input.value) + Number(btn.dataset.delta));
            syncRing();
          });
        });

        input.addEventListener('click', (event) => event.stopPropagation());
        input.addEventListener('input', syncRing);
        input.addEventListener('change', syncRing);

        timer.addEventListener('click', () => form.requestSubmit());
        syncRing();
      };

      document.querySelectorAll('.circle-timer-form').forEach(wireIdleTimer);

      document.querySelectorAll('[data-seconds]').forEach((box) => {
        let left = Number(box.dataset.seconds || 0);
        const total = Number(box.dataset.total || left) || 1;
        const timer = box.closest('.circle-timer');
        const paint = () => {
          const m = String(Math.floor(left / 60)).padStart(2, '0');
          const s = String(left % 60).padStart(2, '0');
          box.textContent = m + ':' + s;
          setRing(timer, left / total);
          if (left > 0) left -= 1;
        };
        paint();
        setInterval(paint, 1000);
      });

      window.junkieCircleTimer = { CIRC, clampMinutes, setRing, wireIdleTimer };

      const todoFocusKey = 'junkie:todoFocus';
      if (sessionStorage.getItem(todoFocusKey)) {
        sessionStorage.removeItem(todoFocusKey);
        document.querySelector('form.inline-form input[name="text"]')?.focus();
      }

      document.querySelectorAll('form.inline-form').forEach((form) => {
        const input = form.querySelector('input[name="text"]');
        if (!input) return;
        const action = form.getAttribute('action') || '';
        if (!action.endsWith('/todos')) return;
        form.addEventListener('submit', () => {
          sessionStorage.setItem(todoFocusKey, '1');
        });
      });

      const wireMenuDrawer = () => {
        const trigger = document.querySelector('.menu-drawer-trigger');
        const drawer = document.querySelector('.menu-drawer');
        const backdrop = document.querySelector('.menu-drawer-backdrop');
        const closeBtn = document.querySelector('.menu-drawer-close');
        if (!trigger || !drawer) return;

        const open = () => {
          drawer.classList.add('open');
          backdrop?.removeAttribute('hidden');
          drawer.setAttribute('aria-hidden', 'false');
          trigger.setAttribute('aria-expanded', 'true');
          document.body.classList.add('menu-drawer-open');
        };
        const shut = () => {
          drawer.classList.remove('open');
          backdrop?.setAttribute('hidden', '');
          drawer.setAttribute('aria-hidden', 'true');
          trigger.setAttribute('aria-expanded', 'false');
          document.body.classList.remove('menu-drawer-open');
        };

        trigger.addEventListener('click', open);
        closeBtn?.addEventListener('click', shut);
        backdrop?.addEventListener('click', shut);
        document.addEventListener('keydown', (event) => {
          if (event.key === 'Escape') shut();
        });
      };
      wireMenuDrawer();

      const themeKey = 'junkie:theme';
      const themeToggle = document.getElementById('theme-toggle');
      const syncThemeToggle = () => {
        const dark = document.documentElement.getAttribute('data-theme') === 'dark';
        themeToggle?.setAttribute('aria-label', dark ? 'Switch to light mode' : 'Switch to dark mode');
      };
      themeToggle?.addEventListener('click', () => {
        const next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
        document.documentElement.setAttribute('data-theme', next);
        localStorage.setItem(themeKey, next);
        syncThemeToggle();
      });
      syncThemeToggle();

      const wireFocusTodosPeek = () => {
        const toggle = document.querySelector('.focus-todos-toggle');
        const panel = document.querySelector('.focus-todos-panel');
        if (!toggle || !panel) return;

        let hideTimer = null;

        const hide = () => {
          panel.classList.remove('is-open');
          toggle.classList.remove('is-open');
          toggle.setAttribute('aria-expanded', 'false');
          panel.setAttribute('aria-hidden', 'true');
          if (hideTimer) {
            clearTimeout(hideTimer);
            hideTimer = null;
          }
        };

        const show = () => {
          panel.classList.add('is-open');
          toggle.classList.add('is-open');
          toggle.setAttribute('aria-expanded', 'true');
          panel.setAttribute('aria-hidden', 'false');
          if (hideTimer) clearTimeout(hideTimer);
          hideTimer = setTimeout(hide, 10000);
        };

        toggle.addEventListener('click', () => {
          if (panel.classList.contains('is-open')) hide();
          else show();
        });
      };
      wireFocusTodosPeek();
    })();
  </script>
</body>
</html>
{{end}}

{{define "heatmap"}}
<div class="heatmap-chart">
  <p class="heatmap-summary">{{focusHours .ActivityTotalMinutes}} hours focused in the last year</p>
  <div class="heatmap-layout">
    <div class="heatmap-dow" aria-hidden="true">
      <span></span><span>Mon</span><span></span><span>Wed</span><span></span><span>Fri</span><span></span>
    </div>
    <div class="heatmap-main">
      <div class="heatmap-months" style="--weeks: {{.ActivityWeeks}}">
        {{range .ActivityMonths}}<span class="heatmap-month" style="--col: {{.Col}}">{{.Label}}</span>{{end}}
      </div>
      <div class="heatmap-wrap">
        <div class="heatmap" style="--weeks: {{.ActivityWeeks}}" aria-label="Focus activity heat map">
          {{range .Activity}}{{if .Empty}}<span class="cell cell-empty"></span>{{else}}<span class="cell l{{.Level}}" title="{{.Date}}: {{.Minutes}} min"></span>{{end}}{{end}}
        </div>
      </div>
    </div>
  </div>
  <div class="heatmap-legend" aria-hidden="true">
    <span>Less</span>
    <span class="cell l0"></span>
    <span class="cell l1"></span>
    <span class="cell l2"></span>
    <span class="cell l3"></span>
    <span class="cell l4"></span>
    <span>More</span>
  </div>
</div>
{{end}}

{{define "todo-remove-icon"}}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg>{{end}}
{{define "todo-delete-icon"}}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>{{end}}

{{define "todo-row"}}
  <li class="{{if .Removed}}removed{{else if .Done}}done{{end}}">
    <form method="post" action="/todo/{{.ID}}/toggle"><button type="submit" class="check" aria-label="{{if .Done}}Mark incomplete{{else}}Mark complete{{end}}">{{if .Done}}✓{{else}}○{{end}}</button></form>
    <span>{{if .DisplayName}}<strong>{{.DisplayName}}</strong> — {{end}}{{.Text}}</span>
    {{if .Removed}}
      <form method="post" action="/todo/{{.ID}}/delete"><button type="submit" class="todo-action todo-delete" aria-label="Delete permanently">{{template "todo-delete-icon" .}}</button></form>
    {{else}}
      <form method="post" action="/todo/{{.ID}}/remove"><button type="submit" class="todo-action todo-remove" aria-label="Remove">{{template "todo-remove-icon" .}}</button></form>
    {{end}}
  </li>
{{end}}

{{define "todo-row-focus"}}
  <li class="{{if .Done}}done{{end}}">
    <form method="post" action="/todo/{{.ID}}/toggle"><button type="submit" class="check" aria-label="{{if .Done}}Mark incomplete{{else}}Mark complete{{end}}">{{if .Done}}✓{{else}}○{{end}}</button></form>
    <span>{{.Text}}</span>
  </li>
{{end}}

{{define "login"}}{{template "shell" .}}{{end}}
{{define "signup"}}{{template "shell" .}}{{end}}
{{define "dashboard"}}{{template "shell" .}}{{end}}
{{define "room"}}{{template "shell" .}}{{end}}

{{define "menu-drawer-guest"}}
<div class="menu-drawer-backdrop" hidden></div>
<aside class="menu-drawer" aria-hidden="true">
  <div class="menu-drawer-head">
    <h2>Rooms</h2>
    <button type="button" class="menu-drawer-close" aria-label="Close">×</button>
  </div>
  <nav class="menu-drawer-nav">
    <a href="/login{{if .Next}}?next={{.Next}}{{end}}">Log in</a>
    <a href="/signup{{if .Next}}?next={{.Next}}{{end}}">Create account</a>
  </nav>
</aside>
{{end}}

{{define "menu-drawer-user"}}
<div class="menu-drawer-backdrop" hidden></div>
<aside class="menu-drawer" aria-hidden="true">
  <div class="menu-drawer-head">
    <h2>Rooms</h2>
    <button type="button" class="menu-drawer-close" aria-label="Close">×</button>
  </div>
  <form class="room-create" method="post" action="/rooms">
    <input name="name" placeholder="Room name">
    <button>Create</button>
  </form>
  <div class="room-list">
    {{range .Rooms}}
      <a class="room-row" href="/r/{{.Code}}">
        <strong>{{.Name}}</strong>
        <span>/r/{{.Code}} · {{.AutoSessions}}×{{.FocusMinutes}}/{{.BreakMinutes}}</span>
      </a>
    {{else}}
      <p class="empty">No rooms yet.</p>
    {{end}}
  </div>
</aside>
{{end}}

{{define "content"}}
  {{if eq .Title "Log in"}}
    <section class="auth-card">
      <h1>Log in</h1>
      <form class="stack" method="post" action="/login">
        {{if .Next}}<input type="hidden" name="next" value="{{.Next}}">{{end}}
        <label>Username <input name="username" autocomplete="username" required></label>
        <label>Password <input type="password" name="password" autocomplete="current-password" required></label>
        <button>Log in</button>
      </form>
      <p class="muted"><a href="/signup{{if .Next}}?next={{.Next}}{{end}}">Create account</a> · <a href="/">Back</a></p>
    </section>
  {{else if eq .Title "Create account"}}
    <section class="auth-card">
      <h1>Create account</h1>
      <form class="stack" method="post" action="/signup">
        {{if .Next}}<input type="hidden" name="next" value="{{.Next}}">{{end}}
        <label>Display name <input name="display_name" autocomplete="name" required></label>
        <label>Username <input name="username" autocomplete="username" required></label>
        <label>Password <input type="password" name="password" autocomplete="new-password" required></label>
        <button>Create account</button>
      </form>
      <p class="muted"><a href="/login{{if .Next}}?next={{.Next}}{{end}}">Log in</a> · <a href="/">Back</a></p>
    </section>
  {{else if eq .Title "Dashboard"}}
    {{if .GuestMode}}
    <div id="guest-desk"></div>
    <script src="/assets/guest.js"></script>
    {{else}}
    {{if .SoloTimer}}
    <section class="focus-desk">
      <div class="focus-desk-main">
        <article class="circle-timer-wrap">
          <div class="circle-timer running" role="timer" aria-label="Focus countdown">
            <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
              <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none" stroke-width="10"/>
              <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-width="10" stroke-dasharray="553" stroke-dashoffset="0"/>
            </svg>
            <div class="circle-timer-core">
              <div class="circle-timer-countdown countdown" data-seconds="{{secondsUntil .SoloTimer.PhaseEndsAt}}" data-total="{{mul .SoloTimer.FocusMinutes 60}}">--:--</div>
            </div>
          </div>
          <form method="post" action="/solo/cancel">
            <button type="submit" class="timer-cancel">Cancel focus</button>
          </form>
        </article>
      </div>
      <button type="button" class="focus-todos-toggle" aria-expanded="false" aria-controls="focus-todos-panel">Todos</button>
      <aside class="focus-todos-panel" id="focus-todos-panel" aria-hidden="true">
        <div class="panel-title">
          <h2>Todos</h2>
        </div>
        <ul class="todo-list">
          {{range .PersonalTodos}}
            {{if not .Removed}}{{template "todo-row-focus" .}}{{end}}
          {{else}}
            <li class="empty">No active tasks.</li>
          {{end}}
        </ul>
      </aside>
    </section>
    {{else}}
    <section class="grid two desk-grid">
      <article class="circle-timer-wrap">
        <form class="circle-timer-form" method="post" action="/solo/start">
          <div class="circle-timer idle" role="group" aria-label="Set focus duration">
            <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
              <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none" stroke-width="10"/>
              <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-width="10" stroke-dasharray="553" stroke-dashoffset="0"/>
            </svg>
            <div class="circle-timer-core">
              <button type="button" class="circle-timer-step" data-delta="-5" aria-label="Decrease 5 minutes">−</button>
              <label class="circle-timer-time">
                <input type="number" name="focus_minutes" min="5" max="180" value="50" aria-label="Focus minutes">
                <span class="circle-timer-suffix">min</span>
              </label>
              <button type="button" class="circle-timer-step" data-delta="5" aria-label="Increase 5 minutes">+</button>
            </div>
            <span class="circle-timer-hint">Tap to start</span>
          </div>
        </form>
      </article>

      <article class="panel">
        <div class="panel-title">
          <h2>Private todos</h2>
          <span>Only visible here</span>
        </div>
        <form class="inline-form" method="post" action="/todos">
          <input name="text" placeholder="What do you need to do?" required>
          <button type="submit" class="todo-add-plus" aria-label="Add task">+</button>
        </form>
        <ul class="todo-list">
          {{range .PersonalTodos}}
            {{template "todo-row" .}}
          {{end}}
        </ul>
      </article>
    </section>
    {{end}}

    {{if not .SoloTimer}}
    <details class="work-map-collapsible">
      <summary class="work-map-link">Work map</summary>
      <article class="panel work-map-panel">
        <div class="panel-title">
          <h2>Work map</h2>
          <span>Focused minutes per day</span>
        </div>
        {{template "heatmap" .}}
      </article>
    </details>
    {{end}}
    {{end}}
  {{else}}
    <section class="{{if .FocusMode}}focus-shell{{else}}room-shell{{end}}">
      <div class="room-header">
        <div>
          <p class="eyebrow">Room code / {{.Room.Code}}</p>
          <h1>{{.Room.Name}}</h1>
          <p class="muted">Share this link: <code>/r/{{.Room.Code}}</code></p>
        </div>
        {{if not .FocusMode}}
          <form class="inline-form" method="post" action="/r/{{.Room.Code}}/rename">
            <input name="name" value="{{.Room.Name}}" aria-label="Room name">
            <button>Rename</button>
          </form>
        {{end}}
      </div>

      {{if .Timer}}
        <article class="timer-card {{.Timer.Phase}}">
          <p class="eyebrow">{{.Timer.Phase}} · session {{.Timer.CurrentSession}} of {{.Timer.TotalSessions}}</p>
          <div class="countdown" data-seconds="{{secondsUntil .Timer.PhaseEndsAt}}">--:--</div>
          <p class="muted">In this timer: {{join .Timer.Participants ", "}}</p>
          {{if and (eq .Timer.Phase "break") (not .Timer.Participant)}}
            <form method="post" action="/r/{{.Room.Code}}/timer-join"><button>Join for the next focus block</button></form>
          {{end}}
          {{if and (eq .Timer.Phase "focus") (not .Timer.Participant)}}
            <p class="notice">A focus block is running. You can watch now and join during the next break.</p>
          {{end}}
          {{if and (eq .Timer.Phase "focus") .Timer.Participant}}
            <form method="post" action="/r/{{.Room.Code}}/timer-leave">
              <button type="submit" class="timer-cancel">Leave focus block</button>
            </form>
          {{end}}
        </article>
      {{else if not .FocusMode}}
        <article class="timer-card idle">
          <p class="eyebrow">Ready room</p>
          <h2>No active timer.</h2>
          <form class="settings" method="post" action="/r/{{.Room.Code}}/settings">
            <label>Focus <input type="number" name="focus_minutes" min="5" max="180" value="{{.Room.FocusMinutes}}"></label>
            <label>Break <input type="number" name="break_minutes" min="1" max="60" value="{{.Room.BreakMinutes}}"></label>
            <label>Sessions <input type="number" name="auto_sessions" min="1" max="12" value="{{.Room.AutoSessions}}"></label>
            <button>Save settings</button>
          </form>
          <form method="post" action="/r/{{.Room.Code}}/timer-start"><button class="big-action">Start focus run</button></form>
        </article>
      {{end}}

      {{if .FocusMode}}
        <section class="focus-message">
          <h2>Focus mode is on.</h2>
          <p>The todo board is hidden until the focus block ends so junkie does not become the distraction.</p>
        </section>
      {{else}}
        <section class="grid two">
          <article class="panel">
            <div class="panel-title">
              <h2>Room todos</h2>
              <span>Everything here is public to the room</span>
            </div>
            <form class="inline-form" method="post" action="/r/{{.Room.Code}}/todos">
              <input name="text" placeholder="What are you working on?" required>
              <button type="submit" class="todo-add-plus" aria-label="Add task">+</button>
            </form>
            <ul class="todo-list">
              {{range .RoomTodos}}
                {{template "todo-row" .}}
              {{end}}
            </ul>
          </article>
          <article class="panel">
            <div class="panel-title">
              <h2>Room controls</h2>
              <span>Anyone can edit; only creator can delete</span>
            </div>
            <p class="muted">During focus, late joiners can watch the countdown but cannot enter the active block. During break, anyone can join the next block.</p>
            <form method="post" action="/r/{{.Room.Code}}/delete" onsubmit="return confirm('Delete this room for everyone?')">
              <button class="danger">Delete room</button>
            </form>
          </article>
        </section>
      {{end}}
    </section>
    <script>
      const ws = new WebSocket((location.protocol === 'https:' ? 'wss' : 'ws') + '://' + location.host + '/ws/r/{{.Room.Code}}');
      ws.onmessage = () => setTimeout(() => location.reload(), 200);
    </script>
  {{end}}
{{end}}
`

const appCSS = `
:root, [data-theme="light"] {
  color-scheme: light;
  --ink: #17211b;
  --muted: #657168;
  --paper: #f6f3ea;
  --card: #fffdf6;
  --line: #dcd3bd;
  --green: #2f7d4a;
  --deep: #0e3b2a;
  --mint: #cde8cf;
  --amber: #d9952f;
  --red: #a53d2f;
  --surface: #fffaf0;
  --input-bg: #fffaf0;
  --code-bg: #fff8e5;
  --notice-border: #ead39a;
  --notice-bg: #fff2c7;
  --card-mix: white;
  --grid-line: rgba(47, 125, 74, .08);
  --shadow: rgba(23, 33, 27, .08);
  --shadow-strong: rgba(23, 33, 27, .12);
  --backdrop: rgba(23, 33, 27, .28);
  --btn-text: #fff;
  --focus-glow: rgba(47, 125, 74, .25);
  --break-glow: rgba(217, 149, 47, .25);
  --heatmap-0: #ebedf0;
  --heatmap-1: #9be9a8;
  --heatmap-2: #40c463;
  --heatmap-3: #30a14e;
  --heatmap-4: #216e39;
}
[data-theme="dark"] {
  color-scheme: dark;
  --ink: #e4ebe6;
  --muted: #8fa095;
  --paper: #0f1411;
  --card: #1a221d;
  --line: #2c3830;
  --green: #4db870;
  --deep: #8fd9a8;
  --mint: #1e3a28;
  --amber: #e8a84a;
  --red: #d46a5c;
  --surface: #1e2822;
  --input-bg: #1e2822;
  --code-bg: #1e2822;
  --notice-border: #4a3f28;
  --notice-bg: #2a2418;
  --card-mix: #0f1411;
  --grid-line: rgba(77, 184, 112, .05);
  --shadow: rgba(0, 0, 0, .35);
  --shadow-strong: rgba(0, 0, 0, .5);
  --backdrop: rgba(0, 0, 0, .58);
  --btn-text: #0f1411;
  --focus-glow: rgba(77, 184, 112, .18);
  --break-glow: rgba(232, 168, 74, .16);
  --heatmap-0: #161b18;
  --heatmap-1: #0e4429;
  --heatmap-2: #006d32;
  --heatmap-3: #26a641;
  --heatmap-4: #39d353;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  min-height: 100vh;
  color: var(--ink);
  background:
    linear-gradient(var(--grid-line) 1px, transparent 1px),
    linear-gradient(90deg, var(--grid-line) 1px, transparent 1px),
    var(--paper);
  background-size: 22px 22px;
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
a { color: var(--deep); }
button, input {
  font: inherit;
}
button {
  border: 0;
  border-radius: 999px;
  background: var(--deep);
  color: var(--btn-text);
  padding: .8rem 1rem;
  cursor: pointer;
  font-weight: 750;
}
button:hover { filter: brightness(1.05); }
input {
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: var(--input-bg);
  padding: .85rem 1rem;
  color: var(--ink);
}
code {
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: .1rem .35rem;
  background: var(--code-bg);
}
.topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1rem clamp(1rem, 4vw, 3rem);
}
.topbar-actions {
  display: flex;
  align-items: center;
  gap: 1rem;
}
.theme-toggle {
  background: transparent;
  color: var(--muted);
  padding: .35rem .65rem;
  border: 1px solid var(--line);
  border-radius: 999px;
  font-size: .78rem;
  font-weight: 700;
  letter-spacing: .02em;
  line-height: 1;
}
.theme-toggle:hover {
  color: var(--ink);
  filter: none;
  border-color: color-mix(in srgb, var(--green) 45%, var(--line));
}
[data-theme="light"] .theme-icon-dark,
[data-theme="dark"] .theme-icon-light {
  display: none;
}
.brand {
  display: inline-flex;
  align-items: center;
  gap: .65rem;
  color: var(--ink);
  text-decoration: none;
  font-weight: 900;
  letter-spacing: -.04em;
}
.brand-mark {
  display: grid;
  place-items: center;
  width: 2rem;
  height: 2rem;
  border-radius: 40% 60% 50% 50%;
  background: var(--deep);
  color: var(--paper);
}
.nav {
  display: flex;
  align-items: center;
  gap: 1rem;
  color: var(--muted);
}
.nav a {
  color: var(--deep);
  text-decoration: none;
  font-weight: 700;
}
.nav-cta {
  border: 1px solid var(--deep);
  border-radius: 999px;
  padding: .45rem .85rem;
}
.link-button, .ghost {
  background: transparent;
  color: var(--deep);
  padding: .25rem .4rem;
}
.page {
  width: min(1120px, calc(100% - 2rem));
  margin: 0 auto 4rem;
}
.auth-card, .panel, .timer-card {
  background: color-mix(in srgb, var(--card) 92%, var(--card-mix));
  border: 1px solid var(--line);
  border-radius: 30px;
  box-shadow: 0 24px 80px var(--shadow);
}
.auth-card {
  max-width: 520px;
  margin: 8vh auto;
  padding: clamp(1.5rem, 5vw, 3rem);
}
h1, h2, p { margin-top: 0; }
h1 {
  font-family: Georgia, "Times New Roman", serif;
  font-size: clamp(2.35rem, 6vw, 5rem);
  line-height: .92;
  letter-spacing: -.07em;
}
h2 {
  font-size: 1.15rem;
  letter-spacing: -.03em;
}
.eyebrow {
  text-transform: uppercase;
  letter-spacing: .16em;
  font-size: .74rem;
  color: var(--green);
  font-weight: 900;
}
.muted, .empty { color: var(--muted); }
.notice {
  border: 1px solid var(--notice-border);
  border-radius: 16px;
  background: var(--notice-bg);
  padding: .9rem 1rem;
}
.stack { display: grid; gap: 1rem; }
.dashboard-hero, .room-header {
  display: grid;
  grid-template-columns: 1fr minmax(280px, 420px);
  gap: 1.25rem;
  align-items: end;
  margin: 2rem 0;
}
.room-create, .inline-form {
  display: flex;
  gap: .6rem;
  align-items: stretch;
}
.inline-form input {
  flex: 1 1 auto;
  min-width: 0;
}
.grid.two {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}
.panel {
  padding: 1.25rem;
  margin-bottom: 1rem;
  border-radius: 10px;
}
.panel-title {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  border-bottom: 1px solid var(--line);
  margin-bottom: 1rem;
  padding-bottom: .75rem;
}
.panel-title span { color: var(--muted); font-size: .9rem; }
.todo-list {
  list-style: none;
  margin: 1rem 0 0;
  padding: 0;
  display: grid;
  gap: .55rem;
}
.todo-list li {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: .7rem;
  border: 1px solid var(--line);
  border-radius: 9px;
  padding: .55rem;
  background: var(--surface);
}
.todo-list li.done {
  background: color-mix(in srgb, var(--muted) 14%, var(--surface));
  border-color: color-mix(in srgb, var(--muted) 40%, var(--line));
}
.todo-list li.done span {
  color: var(--muted);
  text-decoration: line-through;
}
.todo-list li.removed {
  background: color-mix(in srgb, var(--muted) 10%, var(--surface));
  border-color: color-mix(in srgb, var(--muted) 30%, var(--line));
  opacity: .82;
}
.todo-list li.removed span {
  color: var(--muted);
  text-decoration: line-through;
}
.todo-action {
  background: transparent;
  color: var(--muted);
  padding: .2rem;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 2rem;
  height: 2rem;
  border-radius: 8px;
  flex-shrink: 0;
}
.todo-action svg {
  width: 1.1rem;
  height: 1.1rem;
}
.todo-action:hover { filter: none; }
.todo-remove:hover {
  color: var(--deep);
  background: color-mix(in srgb, var(--mint) 55%, transparent);
}
.todo-delete {
  color: var(--red);
}
.todo-delete:hover {
  background: color-mix(in srgb, var(--red) 12%, transparent);
}
.check {
  width: 2rem;
  height: 2rem;
  padding: 0;
  background: var(--mint);
  color: var(--deep);
}
.todo-add-plus {
  flex: 0 0 auto;
  align-self: stretch;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 3.25rem;
  padding: .85rem 1rem;
  border: 1px solid transparent;
  border-radius: 14px;
  box-sizing: border-box;
  font-size: 1.35rem;
  line-height: inherit;
  font-weight: 500;
}
.room-list { display: grid; gap: .65rem; }
.room-row {
  display: grid;
  gap: .25rem;
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 1rem;
  color: var(--ink);
  text-decoration: none;
  background: var(--surface);
}
.room-row span { color: var(--muted); }
.nav-compact {
  gap: .65rem;
  font-size: .9rem;
}
.menu-drawer-trigger {
  display: grid;
  gap: 5px;
  width: 2.5rem;
  height: 2.5rem;
  padding: .55rem;
  background: transparent;
  color: var(--ink);
  border: 1px solid var(--line);
  border-radius: 10px;
  cursor: pointer;
  flex-shrink: 0;
}
.menu-drawer-trigger:hover {
  filter: none;
  border-color: color-mix(in srgb, var(--deep) 35%, var(--line));
  background: color-mix(in srgb, var(--card) 70%, var(--surface));
}
.menu-bar {
  display: block;
  height: 2px;
  background: currentColor;
  border-radius: 1px;
}
.menu-drawer-backdrop {
  position: fixed;
  inset: 0;
  z-index: 90;
  background: var(--backdrop);
}
.menu-drawer {
  position: fixed;
  top: 0;
  right: 0;
  z-index: 100;
  width: min(380px, 92vw);
  height: 100vh;
  padding: 1.25rem;
  overflow-y: auto;
  background: var(--card);
  border-left: 1px solid var(--line);
  box-shadow: -16px 0 48px var(--shadow-strong);
  transform: translateX(100%);
  transition: transform .28s ease;
  display: grid;
  gap: 1rem;
  align-content: start;
}
.menu-drawer.open {
  transform: translateX(0);
}
.menu-drawer-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
  margin-bottom: .25rem;
  padding-bottom: .75rem;
  border-bottom: 1px solid var(--line);
}
.menu-drawer-head h2 {
  margin: 0;
}
.menu-drawer-close {
  background: transparent;
  color: var(--muted);
  font-size: 1.6rem;
  line-height: 1;
  padding: .15rem .45rem;
  border: 0;
}
.menu-drawer-close:hover {
  color: var(--deep);
  filter: none;
}
.menu-drawer-nav {
  display: grid;
  gap: .5rem;
}
.menu-drawer-nav a {
  display: block;
  padding: .75rem 1rem;
  border: 1px solid var(--line);
  border-radius: 10px;
  color: var(--ink);
  text-decoration: none;
  font-weight: 700;
  background: var(--surface);
}
.menu-drawer-nav a:hover {
  border-color: color-mix(in srgb, var(--deep) 35%, var(--line));
}
.menu-drawer .room-create {
  margin-bottom: .5rem;
}
body.menu-drawer-open {
  overflow: hidden;
}
.heatmap-chart {
  width: 100%;
}
.heatmap-summary {
  color: var(--muted);
  font-size: .88rem;
  margin: 0 0 .75rem;
}
.heatmap-layout {
  display: flex;
  gap: .35rem;
  width: 100%;
}
.heatmap-dow {
  display: grid;
  grid-template-rows: repeat(7, 1fr);
  gap: 3px;
  font-size: .65rem;
  color: var(--muted);
  padding-top: 1.15rem;
  width: 1.75rem;
  flex-shrink: 0;
}
.heatmap-dow span {
  display: flex;
  align-items: center;
  line-height: 1;
}
.heatmap-main {
  flex: 1;
  min-width: 0;
}
.heatmap-months {
  display: grid;
  grid-template-columns: repeat(var(--weeks), minmax(0, 1fr));
  gap: 3px;
  font-size: .68rem;
  color: var(--muted);
  margin-bottom: 4px;
  min-height: 1rem;
}
.heatmap-month {
  grid-column: calc(var(--col) + 1);
}
.heatmap-wrap {
  overflow-x: auto;
  width: 100%;
  padding-bottom: .25rem;
}
.heatmap {
  display: grid;
  grid-template-rows: repeat(7, minmax(0, 1fr));
  grid-auto-flow: column;
  grid-auto-columns: minmax(0, 1fr);
  gap: 3px;
  width: 100%;
  min-height: 108px;
}
.heatmap-legend {
  display: flex;
  align-items: center;
  gap: .25rem;
  justify-content: flex-end;
  margin-top: .65rem;
  font-size: .68rem;
  color: var(--muted);
}
.heatmap-legend .cell {
  width: 11px;
  height: 11px;
  flex-shrink: 0;
}
.cell {
  aspect-ratio: 1;
  width: 100%;
  border-radius: 2px;
  background: var(--heatmap-0);
}
.cell-empty {
  visibility: hidden;
}
.cell.l0 { background: var(--heatmap-0); }
.cell.l1 { background: var(--heatmap-1); }
.cell.l2 { background: var(--heatmap-2); }
.cell.l3 { background: var(--heatmap-3); }
.cell.l4 { background: var(--heatmap-4); }
.circle-timer-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1rem;
  justify-content: center;
  padding: 1.5rem 0 2.5rem;
  margin-bottom: 0;
}
.desk-grid .circle-timer-wrap {
  padding: 1rem 0;
}
#guest-desk {
  display: flex;
  flex-direction: column;
}
.desk-grid {
  flex: 1;
  align-items: center;
  min-height: calc(100vh - 10rem);
}
.timer-cancel {
  background: var(--red);
  color: var(--btn-text);
  border: 0;
  font-weight: 700;
  font-size: .95rem;
  padding: .65rem 1.25rem;
}
.timer-cancel:hover {
  color: var(--btn-text);
  filter: brightness(1.08);
}
.work-map-collapsible {
  margin-bottom: 1rem;
}
.work-map-collapsible .work-map-panel {
  margin-top: .75rem;
}
.work-map-link {
  display: inline-block;
  cursor: pointer;
  color: var(--muted);
  font-weight: 700;
  font-size: .92rem;
  list-style: none;
}
.work-map-collapsible summary {
  list-style: none;
}
.work-map-collapsible summary::-webkit-details-marker {
  display: none;
}
.work-map-link:hover {
  color: var(--deep);
}
.circle-timer {
  position: relative;
  width: min(300px, 82vw);
  aspect-ratio: 1;
  display: grid;
  place-items: center;
  cursor: pointer;
  border: 0;
  background: transparent;
  padding: 0;
  font: inherit;
  color: inherit;
  overflow: visible;
}
.circle-timer.idle:hover .circle-timer-progress { stroke: var(--deep); }
.circle-timer-svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  transform: rotate(-90deg);
}
.circle-timer-track {
  stroke: var(--line);
}
.circle-timer-progress {
  stroke: var(--green);
  stroke-linecap: round;
  transition: stroke-dashoffset .35s ease, stroke .2s ease;
}
.circle-timer.running .circle-timer-progress { stroke: var(--deep); }
.circle-timer.running { cursor: default; }
.circle-timer-core {
  position: relative;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.4rem;
  width: 100%;
  height: 100%;
  pointer-events: none;
  overflow: visible;
}
.circle-timer-time {
  display: grid;
  justify-items: center;
  pointer-events: auto;
  overflow: visible;
}
.circle-timer-time input {
  width: 3.5ch;
  min-width: 3.5ch;
  text-align: center;
  font-size: clamp(2rem, 8vw, 2.6rem);
  font-weight: 950;
  font-variant-numeric: tabular-nums;
  letter-spacing: -.04em;
  border: none;
  background: transparent;
  padding: 0;
  color: var(--ink);
  overflow: visible;
  -moz-appearance: textfield;
}
.circle-timer-time.digits-3 input {
  font-size: clamp(1.7rem, 6.5vw, 2.1rem);
  letter-spacing: -.03em;
}
.circle-timer-time input::-webkit-outer-spin-button,
.circle-timer-time input::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.circle-timer-suffix {
  font-size: .78rem;
  color: var(--muted);
  font-weight: 600;
  letter-spacing: .04em;
}
.circle-timer-countdown {
  font-variant-numeric: tabular-nums;
  font-size: clamp(2rem, 9vw, 2.6rem);
  font-weight: 950;
  letter-spacing: -.06em;
  line-height: 1;
}
.circle-timer-step {
  width: 2.1rem;
  height: 2.1rem;
  padding: 0;
  border-radius: 50%;
  background: var(--mint);
  color: var(--deep);
  font-size: 1.2rem;
  line-height: 1;
  font-weight: 800;
  pointer-events: auto;
  flex-shrink: 0;
}
.circle-timer-step:hover { filter: brightness(1.04); }
.circle-timer-hint {
  position: absolute;
  bottom: -1.75rem;
  left: 50%;
  transform: translateX(-50%);
  font-size: .78rem;
  color: var(--muted);
  letter-spacing: .02em;
  white-space: nowrap;
  pointer-events: none;
}
.timer-card {
  padding: clamp(1.25rem, 4vw, 2rem);
  margin-bottom: 1rem;
  position: relative;
  overflow: hidden;
}
.timer-card.focus {
  background: radial-gradient(circle at top right, var(--focus-glow), transparent 36%), var(--card);
}
.timer-card.break {
  background: radial-gradient(circle at top right, var(--break-glow), transparent 36%), var(--card);
}
.countdown {
  font-variant-numeric: tabular-nums;
  font-size: clamp(4rem, 18vw, 11rem);
  line-height: .9;
  letter-spacing: -.08em;
  font-weight: 950;
}
.settings {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: .7rem;
  align-items: end;
}
.big-action { margin-top: 1rem; background: var(--green); }
.danger { background: var(--red); }
.focus-shell {
  min-height: calc(100vh - 10rem);
  display: grid;
  align-content: center;
}
.focus-desk {
  position: relative;
  min-height: calc(100vh - 10rem);
  display: grid;
  place-items: center;
}
.focus-desk-main {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.focus-todos-toggle {
  position: fixed;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  z-index: 21;
  padding: .65rem .45rem;
  border: 1px solid var(--line);
  border-right: 0;
  border-radius: .5rem 0 0 .5rem;
  background: var(--card);
  color: var(--muted);
  font-size: .78rem;
  font-weight: 700;
  letter-spacing: .04em;
  cursor: pointer;
  transition: right .25s ease, color .15s ease;
}
.focus-todos-toggle.is-open {
  right: min(320px, 85vw);
  color: var(--deep);
}
.focus-todos-toggle:hover {
  color: var(--deep);
}
.focus-todos-panel {
  position: fixed;
  top: 25vh;
  right: 0;
  height: 50vh;
  max-height: calc(100vh - 2rem);
  width: min(320px, 85vw);
  z-index: 20;
  background: var(--card);
  border: 1px solid var(--line);
  border-right: none;
  border-radius: 12px 0 0 12px;
  box-shadow: -4px 0 24px var(--shadow);
  padding: 1.25rem;
  overflow-y: auto;
  transform: translateX(100%);
  transition: transform .25s ease;
}
@media (max-height: 520px) {
  .focus-todos-panel {
    top: 1rem;
    height: calc(100vh - 2rem);
  }
}
.focus-todos-panel.is-open {
  transform: translateX(0);
}
.focus-message {
  text-align: center;
  color: var(--muted);
}
@media (max-width: 760px) {
  .dashboard-hero, .room-header, .grid.two, .settings, .desk-grid {
    grid-template-columns: 1fr;
  }
  .room-create, .inline-form {
    flex-direction: column;
  }
  .panel-title {
    display: block;
  }
}
@media (prefers-reduced-motion: reduce) {
  * { scroll-behavior: auto !important; }
  .menu-drawer { transition: none; }
  .focus-todos-panel { transition: none; }
  .focus-todos-toggle { transition: none; }
}
`
