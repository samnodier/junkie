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
		"join": strings.Join,
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
  <link rel="stylesheet" href="/assets/app.css">
  <script src="https://unpkg.com/htmx.org@2.0.4"></script>
</head>
<body>
  <header class="topbar">
    <a class="brand" href="/dashboard" aria-label="junkie dashboard">
      <span class="brand-mark">j</span>
      <span>junkie</span>
    </a>
    {{if .User.ID}}
      <nav class="nav">
        <span>{{.User.DisplayName}}</span>
        <form method="post" action="/logout"><button class="link-button">Log out</button></form>
      </nav>
    {{else}}
      <nav class="nav">
        <a href="/login{{if .Next}}?next={{.Next}}{{end}}">Log in</a>
        <a class="nav-cta" href="/signup{{if .Next}}?next={{.Next}}{{end}}">Create account</a>
      </nav>
    {{end}}
  </header>
  <main class="page">
    {{if .Error}}<p class="notice">{{.Error}}</p>{{end}}
    {{template "content" .}}
  </main>
  <script>
    document.querySelectorAll('[data-seconds]').forEach((box) => {
      let left = Number(box.dataset.seconds || 0);
      const paint = () => {
        const m = String(Math.floor(left / 60)).padStart(2, '0');
        const s = String(left % 60).padStart(2, '0');
        box.textContent = m + ':' + s;
        if (left > 0) left -= 1;
      };
      paint();
      setInterval(paint, 1000);
    });
  </script>
</body>
</html>
{{end}}

{{define "login"}}{{template "shell" .}}{{end}}
{{define "signup"}}{{template "shell" .}}{{end}}
{{define "dashboard"}}{{template "shell" .}}{{end}}
{{define "room"}}{{template "shell" .}}{{end}}

{{define "content"}}
  {{if eq .Title "Log in"}}
    <section class="auth-card">
      <p class="eyebrow">Rooms only</p>
      <h1>Log in to join or create a room.</h1>
      <p class="muted">You can use junkie solo without an account. Accounts are only needed for shared rooms.</p>
      <form class="stack" method="post" action="/login">
        {{if .Next}}<input type="hidden" name="next" value="{{.Next}}">{{end}}
        <label>Username <input name="username" autocomplete="username" required></label>
        <label>Password <input type="password" name="password" autocomplete="current-password" required></label>
        <button>Log in</button>
      </form>
      <p class="muted">New here? <a href="/signup{{if .Next}}?next={{.Next}}{{end}}">Create an account</a> · <a href="/dashboard">Keep using solo mode</a></p>
    </section>
  {{else if eq .Title "Create account"}}
    <section class="auth-card">
      <p class="eyebrow">Rooms only</p>
      <h1>Create an account for shared rooms.</h1>
      <p class="muted">Solo focus, private todos, and your work map work without signing up.</p>
      <form class="stack" method="post" action="/signup">
        {{if .Next}}<input type="hidden" name="next" value="{{.Next}}">{{end}}
        <label>Display name <input name="display_name" autocomplete="name" required></label>
        <label>Username <input name="username" autocomplete="username" required></label>
        <label>Password <input type="password" name="password" autocomplete="new-password" required></label>
        <button>Create account</button>
      </form>
      <p class="muted">Already have an account? <a href="/login{{if .Next}}?next={{.Next}}{{end}}">Log in</a> · <a href="/dashboard">Keep using solo mode</a></p>
    </section>
  {{else if eq .Title "Dashboard"}}
    {{if .GuestMode}}
    <section class="dashboard-hero">
      <div>
        <p class="eyebrow">Personal desk</p>
        <h1>Use junkie solo. No account needed.</h1>
        <p class="muted">Your private list, timer, and work map stay on this device until you sign up.</p>
      </div>
    </section>
    <div id="guest-desk"></div>
    <script src="/assets/guest.js"></script>
    {{else}}
    <section class="dashboard-hero">
      <div>
        <p class="eyebrow">Personal desk</p>
        <h1>Your work map, rooms, and private list.</h1>
      </div>
      <form class="room-create" method="post" action="/rooms">
        <input name="name" placeholder="Room name, e.g. Study hall">
        <button>Create room</button>
      </form>
    </section>

    {{if .SoloTimer}}
      <article class="timer-card focus">
        <p class="eyebrow">Solo focus · hidden-desk mode</p>
        <div class="countdown" data-seconds="{{secondsUntil .SoloTimer.PhaseEndsAt}}">--:--</div>
        <p class="muted">Private todos and rooms are hidden until this block ends.</p>
      </article>
    {{else}}
    <section class="grid two">
      <article class="panel">
        <div class="panel-title">
          <h2>Private todos</h2>
          <span>Only visible here</span>
        </div>
        <form class="inline-form" method="post" action="/todos">
          <input name="text" placeholder="What do you need to do?" required>
          <button>Add</button>
        </form>
        <ul class="todo-list">
          {{range .PersonalTodos}}
            <li class="{{if .Done}}done{{end}}">
              <form method="post" action="/todo/{{.ID}}/toggle"><button class="check">{{if .Done}}✓{{else}}○{{end}}</button></form>
              <span>{{.Text}}</span>
              <form method="post" action="/todo/{{.ID}}/delete"><button class="ghost">Delete</button></form>
            </li>
          {{else}}
            <li class="empty">Add a private task before joining a room.</li>
          {{end}}
        </ul>
      </article>

      <article class="panel">
        <div class="panel-title">
          <h2>Rooms</h2>
          <span>Links are the invite</span>
        </div>
        <div class="room-list">
          {{range .Rooms}}
            <a class="room-row" href="/r/{{.Code}}">
              <strong>{{.Name}}</strong>
              <span>/r/{{.Code}} · {{.AutoSessions}}×{{.FocusMinutes}}/{{.BreakMinutes}}</span>
            </a>
          {{else}}
            <p class="empty">Create a room, then share its link with your Discord study group.</p>
          {{end}}
        </div>
      </article>
    </section>

    <article class="timer-card idle">
      <p class="eyebrow">Solo timer</p>
      <h2>Run a private focus block.</h2>
      <form class="inline-form" method="post" action="/solo/start">
        <input type="number" name="focus_minutes" min="5" max="180" value="50" aria-label="Focus minutes">
        <button>Start solo focus</button>
      </form>
    </article>
    {{end}}

    <article class="panel">
      <div class="panel-title">
        <h2>Work map</h2>
        <span>Focused minutes per day</span>
      </div>
      <div class="heatmap" aria-label="Activity heat map">
        {{range .Activity}}<span class="cell l{{.Level}}" title="{{.Date}}: {{.Minutes}} min"></span>{{end}}
      </div>
    </article>
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
              <button>Add</button>
            </form>
            <ul class="todo-list">
              {{range .RoomTodos}}
                <li class="{{if .Done}}done{{end}}">
                  <form method="post" action="/todo/{{.ID}}/toggle"><button class="check">{{if .Done}}✓{{else}}○{{end}}</button></form>
                  <span><strong>{{.DisplayName}}</strong> — {{.Text}}</span>
                  <form method="post" action="/todo/{{.ID}}/delete"><button class="ghost">Delete</button></form>
                </li>
              {{else}}
                <li class="empty">No public room todos yet.</li>
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
:root {
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
}
* { box-sizing: border-box; }
body {
  margin: 0;
  min-height: 100vh;
  color: var(--ink);
  background:
    linear-gradient(rgba(47,125,74,.08) 1px, transparent 1px),
    linear-gradient(90deg, rgba(47,125,74,.08) 1px, transparent 1px),
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
  color: white;
  padding: .8rem 1rem;
  cursor: pointer;
  font-weight: 750;
}
button:hover { filter: brightness(1.05); }
input {
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: #fffaf0;
  padding: .85rem 1rem;
  color: var(--ink);
}
code {
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: .1rem .35rem;
  background: #fff8e5;
}
.topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1rem clamp(1rem, 4vw, 3rem);
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
  background: color-mix(in srgb, var(--card) 92%, white);
  border: 1px solid var(--line);
  border-radius: 30px;
  box-shadow: 0 24px 80px rgba(23,33,27,.08);
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
  border: 1px solid #ead39a;
  border-radius: 16px;
  background: #fff2c7;
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
}
.grid.two {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
}
.panel {
  padding: 1.25rem;
  margin-bottom: 1rem;
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
  border-radius: 16px;
  padding: .55rem;
  background: #fffaf0;
}
.todo-list li.done span {
  color: var(--muted);
  text-decoration: line-through;
}
.check {
  width: 2rem;
  height: 2rem;
  padding: 0;
  background: var(--mint);
  color: var(--deep);
}
.room-list { display: grid; gap: .65rem; }
.room-row {
  display: grid;
  gap: .25rem;
  border: 1px solid var(--line);
  border-radius: 18px;
  padding: 1rem;
  color: var(--ink);
  text-decoration: none;
  background: #fffaf0;
}
.room-row span { color: var(--muted); }
.heatmap {
  display: grid;
  grid-template-columns: repeat(12, 1fr);
  gap: .35rem;
}
.cell {
  aspect-ratio: 1;
  border-radius: 6px;
  background: #e8dfc9;
  border: 1px solid rgba(23,33,27,.05);
}
.cell.l1 { background: #cfe7c8; }
.cell.l2 { background: #94ca86; }
.cell.l3 { background: #4e9d5c; }
.cell.l4 { background: #0e6b43; }
.timer-card {
  padding: clamp(1.25rem, 4vw, 2rem);
  margin-bottom: 1rem;
  position: relative;
  overflow: hidden;
}
.timer-card.focus {
  background: radial-gradient(circle at top right, rgba(47,125,74,.25), transparent 36%), var(--card);
}
.timer-card.break {
  background: radial-gradient(circle at top right, rgba(217,149,47,.25), transparent 36%), var(--card);
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
.focus-message {
  text-align: center;
  color: var(--muted);
}
@media (max-width: 760px) {
  .dashboard-hero, .room-header, .grid.two, .settings {
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
}
`
