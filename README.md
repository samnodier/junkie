# junkie

`junkie` is a small shared focus app for study groups. Open it in a browser, track what you need to finish, run solo or shared focus timers, and see progress on a GitHub-inspired work map of focused minutes per day.


| Light | Dark |
| ----- | ---- |
| ![The junkie desk in light mode: a focus ring beside a private todo list](assets/desk-light.png) | ![The junkie desk in dark mode: a focus ring beside a private todo list](assets/desk-dark.png) |




## How to use junkie

**[Full documentation lives at junkie-blin.onrender.com/docs](https://junkie-blin.onrender.com/docs)** — every screen, every setting, every command. The short version:

- **A block** is one run of the timer: focus, break, focus, for as many sessions as you set. Everything else hangs off that.
- **No account needed to try it.** Guest mode keeps todos, the solo timer and your work map in that browser's `localStorage`, on that device only. It never syncs, and it is never copied into an account you make later.
- **Accounts** are a username and a password — junkie collects no email address, so password resets go through [Discord](#discord-bot). Link your Discord *before* you need it.
- **Rooms** are shared spaces at `/r/{code}` with their own members, public todo board and timer. Up to 5 per account, 100 members each. Timer settings are open to every member; removing members and changing the room's sound need a room admin; deleting and transferring are the creator's.
- **Temporary rooms** at `/f/{code}` are throwaway blocks for a few people: joined by link, joinable mid-block, and deleted the moment the run ends. They are also the one place the session count can be changed while the block runs — a stepper on each break, for when six sessions turns out to be four.
- **Streaming** — a temporary room has an overlay twin at `/f/{code}/embed` for an OBS browser source, and the countdown can pop out into an always-on-top window.
- **Sound** — the chime toggle is per-device and per-person; the sound itself belongs to the room, and an admin can upload one (15 s, 512 KB, MP3/OGG/WAV).
- **Connections** are a mutual link between two accounts, made with a one-time link. Connected people see each other's focus heatmap and nothing else; profiles are invisible to everyone else.

Signed in, everything syncs live between your devices — private todos over `/ws/me`, room todos and timers over the room socket, the work map server-side. Guest data never syncs.

## Install on your phone

junkie is a PWA, so it installs to your home screen and runs fullscreen with no browser chrome.

- **iPhone / iPad** — open [junkie](https://junkie-blin.onrender.com) in Safari, tap Share, then **Add to Home Screen**.
- **Android** — either open it in Chrome and tap **Install app**, or download the signed APK from the [android release](https://github.com/samnodier/junkie/releases/tag/android) and open it (you may need to allow installing from unknown sources).

Both are the same app pointing at the hosted site; the APK just wraps it so there's nothing to install from a browser.

## Terminal client

junkie also runs in the terminal. Bare `junkie` opens a full-screen desk you stay in — the countdown, your todos, and (once signed in) your rooms, with `tab` moving the countdown between your own block and each room's. No account is required: without one it is a guest desk on this machine, the same idea as the browser guest mode. Sign in to sync with the cloud account the web uses.

A block you start while signed in shows up on the web mid-countdown, and one you start on the web can be finished here — the server owns the clock either way, so closing the terminal never loses a block. Guest blocks live in `~/.local/share/junkie/` and likewise survive quitting; they are not copied into an account if you sign in later.

### Install

```sh
curl -fsSL https://raw.githubusercontent.com/samnodier/junkie/master/install.sh | sh
```

That puts a `junkie` binary in `~/.local/bin`, which needs to be on your `PATH`. It picks the newest `vX.Y.Z` release for your platform and verifies the download against that release's published `checksums.txt` before installing it.

**To upgrade, run the same command again** — it always fetches the newest version and replaces what is there. `JUNKIE_VERSION=vX.Y.Z` pins a particular one, and `JUNKIE_INSTALL_DIR` installs somewhere other than `~/.local/bin`.

With Go installed you can build it from source instead:

```sh
go install github.com/samnodier/junkie/cmd/junkie-cli@latest
```

That names the binary `junkie-cli`, because `go install` takes the name from the directory and `cmd/junkie` is already the server. Rename it to `junkie` if you want the shorter command — the installed release is called `junkie`, and the rest of this section assumes that name.

### Versions

Releases are `vMAJOR.MINOR.PATCH` tags on this repository. Pushing one runs the `release-cli` workflow, which tests the module, builds the binary for Linux, macOS and Windows on both amd64 and arm64, and publishes them as a GitHub release with a `checksums.txt`. `junkie version` prints the version it was built from; a binary built from a checkout says `dev`.

The repository also carries an `android` tag for the [TWA build](#install-on-your-phone), which is not a CLI release. The installer asks for the newest `v*` tag by name rather than for whatever GitHub currently calls "latest", so publishing a new APK cannot break `curl | sh`.

### Signing in

You do not have to. `junkie` on its own opens a guest desk. Press `L` there, or run:

```sh
junkie login
```

It asks for your username and password — the same ones you use on the web — and stores the session in `~/.config/junkie/config.json`, mode `0600`. In a terminal it then opens the desk signed in. You stay signed in across terminals and reboots until the session expires **30 days after you signed in**; it does not renew as you use it, so roughly once a month you will be asked to sign in again. `junkie logout` ends it immediately, on the server as well as on disk.

Changing your password anywhere signs the terminal out too, because that ends every other session on the account.

### The desk

Run `junkie` on its own and you get the desk full-screen: a live countdown, your todos, and — signed in — what your rooms are doing. You stay in this program until you quit. A block ending offers the break on the same screen; it does not drop you back to the shell.

The desk is three panes — the block, the todos and the rooms. `tab` moves between them and `j`/`k` scroll whichever one has the focus, marked with a `‹`. The keys are grouped along the bottom of the screen the way they are grouped here:

| Group | Key | Does |
| ----- | --- | ---- |
| any | `j` `k` | scroll the focused pane: the blocks, the todos, or the rooms |
| any | `g` `G` | jump to the ends of the focused pane |
| todos | `space` | complete or un-complete |
| todos | `a` / `e` / `d` / `u` | add · edit · remove · undo the last remove |
| block | `f` / `b` / `s` | start focus · take the break · skip it |
| block | `x` | end the block on screen — your own, or leave a room's. Asks first either way; ending your own banks none of its minutes |
| desk | `A` | join a room by code, without leaving the desk |
| desk | `tab` `shift+tab` | move the focus between the three panes |
| desk | `w` | timer pane fills the window (`esc` or `q` returns, `x` ends the block) |
| desk | `r` / `q` | refresh · quit (`q` backs out of the zoomed pane first) |
| desk | `L` | sign in (guest desk) |
| desk | `y` / `n` | answer a room's join prompt |

In a narrow window each row drops its last hints, and in a short one they collapse back to a single line. A message the desk puts up — "no break is waiting" — clears itself after a few seconds, and the next key you press clears it immediately.

### Two blocks at once

Your own block and a room's block run independently — you can be in both, and starting or joining one never asks you to give up the other — and the countdown shows one at a time. `j` and `k` move between them with the block pane focused: your private block first, then each room you are in. Whichever is on screen is named above the digits and marked in the room list below them, so there is never a question of which block you are looking at.

With a room on screen the timer keys act on that room, and two more apply:

| Key | Does |
| --- | ---- |
| `f` / `b` / `s` | start a block · take the break · skip it |
| `i` | I'm in — join the block, or check in for the next one |
| `x` | leave the block — the same key that ends your own block, so there is one to remember. Asks first: a block you have left will not always take you back |

The todo list follows the countdown. With a room on screen it is that room's list — your own todos to work, everyone else's to read, each named. `a` there adds to the room rather than to your private list.

The terminal is deliberately not the whole of junkie: room members, settings, temporary rooms and admin live on the web and in Discord. What it does is the part a terminal is good at — a block running in a pane while you work.

You do not have to reach for `tab` in the first place: `junkie` opens on a room whose block is live rather than on a private timer that is not running, and `junkie CODE` opens straight onto one. Answering `y` to a join prompt also brings that room's block to the front, since you just said you were joining it.

When someone starts a block in one of your rooms, the desk asks whether you want in and counts down the 30 seconds you have to answer. Not answering is an answer: the block starts without you. This only reaches you while the desk is open — when you are away, the Discord bot is what notifies you.

### Commands

| Command | Does |
| ------- | ---- |
| `junkie` | open the desk full-screen |
| `junkie CODE` | open the desk on that room's block |
| `junkie status` | timer, todo counts and every room's activity, in one glance |
| `junkie todos` | list your private todos |
| `junkie stats` | the work map: a year of focused days |
| `junkie room new NAME` · `junkie room join CODE` | create or join a room (or press `A` in the desk) |
| `junkie room start [CODE] [MINUTES]` | start a room's block, opening the 30-second lobby |
| `junkie room enter` · `leave` · `checkin` · `skip` | act on the block that's running |
| `junkie whoami` · `junkie logout` | who this terminal is, and sign out |

`junkie room` commands take a room code, and you can leave it out when you are only in one room. Pasting a room's URL works as well as typing its code.

Running a block is done in the desk, not from the shell: `f`, `b`, `s` and `x` are one keystroke each and there is nothing a `junkie focus` would add over pressing `f`. The commands that remain are the ones worth reading from a script or a status bar.

### Sizing

The countdown fits itself to the window, so a terminal parked down the side of a screen is a first-class way to run it — it drops to smaller digits, then to a line of text, then to the numbers alone, rather than wrapping.

```
   ███ ███     █ █ ███
     █   █  █  █ █   █          focus
   ███ ███     ███   █          22:47      22:47
   █   █    █    █   █
   ███ ███       █   █

   ~34 columns                  ~16          ~8
```

### Scripting

Every read command takes `--json`. Piped output is never truncated and `junkie` on its own prints the status instead of opening the full-screen desk, so it stays usable from a script.

`JUNKIE_URL` points a command at another server — a local one, say — without disturbing the login you already have. `JUNKIE_CONFIG` moves the config file, and `JUNKIE_DATA` the guest todos and timer.

### Not in the terminal

Avatars, connections and public profiles, the admin space, Discord linking and password reset all stay on the web, where they belong. Guest data in the terminal is this machine only, and does not merge into an account.

## Discord bot

junkie has a Discord bot that runs a server's focus room from chat: live countdown messages showing who's in, break notifications, Join and break-control buttons, stats, and a heatmap picture — no browser needed once you're set up.

**Just want to link your account or reset your password?** [Join the junkie Discord server](https://discord.gg/qEEzdXQHtK) and use `/junkie link` or `/junkie reset-password` in `#junkie-bot`. Your commands and the bot's replies there are private — nobody else in the channel can see them.

**Add it to your own server** (needs Manage Server permission there):

> [https://discord.com/oauth2/authorize?client_id=1526585859044933684&scope=bot+applications.commands&permissions=2048](https://discord.com/oauth2/authorize?client_id=1526585859044933684&scope=bot+applications.commands&permissions=2048)

The bot only asks for Send Messages. Once added: each participant runs `/junkie link` once to connect their junkie account, then an admin runs `/junkie register` in the channel the timer should post to (pass an existing room code to connect it, or omit to create a fresh room). Move notifications later with `/junkie channel [#channel]`. `/junkie help` lists all commands.

Room settings are fully manageable from Discord with `/junkie config` — the timer as `timer:30/5/3`, plus `checkin:On/Off` (session check-in) and `auto-breaks:On/Off` (auto-start breaks). Supply only what you want to change; run `/junkie config` bare to see the current settings. As on the web, settings can't change mid-run.

The live message states each phase's length and its deadline both as a wall-clock time and as a countdown ("session 1 of 2 · 80 min. Break at 21:19 · in an hour"), each rendered in your own timezone and kept current by Discord itself. Both are needed: the countdown alone rounds to one coarse unit, so a long block reads as "in an hour" for most of its length.

Breaks are fully controllable from Discord too. During a break, the live message carries the same controls the web room has, next to Join: **Start break** when a break is waiting (auto-start breaks off), **Pause break** while it's running, **Resume break** while it's paused, and **Skip break** to jump straight to the next focus session (hidden in check-in rooms, same as the web). Tapping any of them requires a linked account that's a room member, and — like on the web — counts as your check-in when session check-in is on.

Linking Discord is also your password lifeline: `/junkie reset-password` DMs a single-use reset link to the linked account, so a forgotten password never needs an email (see [the docs](https://junkie-blin.onrender.com/docs#accounts)). This is why the public [junkie Discord server](https://discord.gg/qEEzdXQHtK) exists — joining it gives anyone a place to run these commands without needing their own server.

Self-hosting? The bot is optional — it starts only when `DISCORD_BOT_TOKEN`, `DISCORD_APPLICATION_ID`, and `PUBLIC_BASE_URL` are set (see `.env.example`), and you'd mint your own invite link with your application's client id.

## Where your data lives

**Guest** — in the browser, todos, solo timer state, and your work map stay in `localStorage` on that device. In the terminal they live in `~/.local/share/junkie/` (`JUNKIE_DATA` to move them). Nothing reaches the server, and the two guest stores do not share.

**Signed in** — everything lives in PostgreSQL and follows you across devices: account and session, todos, timer runs and focus minutes, rooms and their settings, profile pictures, and connections.

**Finished todos don't stick around.** Once a todo has been completed or removed for 24 hours, junkie deletes it permanently — for every user, in private lists and rooms alike (guest todos follow the same rule locally). Un-completing or restoring a todo within that day resets its clock. Your work map keeps the focus history; the list stays about what's next.

Guest data does not migrate into an account; sign up first if you want to keep progress long-term.

## Admin and owner access

Accounts have one of three roles. `user` is the default and cannot open `/admin`. `admin` adds operational aggregates, account and room metadata, and room deletion. `owner` adds per-user focus summaries and promoting or demoting admins. Admin actions are audited.

To bootstrap the first owner, sign in to the intended account once, set `JUNKIE_OWNER_USERNAME` to its username, and restart. That account is promoted at startup. Clearing the variable never demotes an existing owner, and owners can only be changed with database access.

## Local development

Start Postgres:

```sh
docker compose up -d
```

Run the app:

```sh
GOPATH="$PWD/.gopath" GOCACHE="$PWD/.gocache" go run ./cmd/junkie
```

Open:

```text
http://localhost:8080
```

The default database URL is:

```text
postgres://junkie:junkie@localhost:5432/junkie?sslmode=disable
```

Override it with `DATABASE_URL` if needed. Migrations in `migrations/` run automatically at startup.

### Terminal client

The terminal client is a second binary in the same module, so it builds from the same checkout:

```sh
go build -o /tmp/junkie-dev/bin/junkie ./cmd/junkie-cli
export PATH=/tmp/junkie-dev/bin:$PATH
```

Build it *as* `junkie` rather than letting `go build` name it after the directory. The released binary is called `junkie`, and `junkie-cli` is an artefact of the source layout that no user ever sees. Keep it out of the repo root as well — `./junkie` is where the server's own build output lands.

Give it its own config and guest data and it will leave the session and files you use day to day alone:

```sh
export JUNKIE_URL=http://localhost:8080
export JUNKIE_CONFIG=/tmp/junkie-dev/config.json
export JUNKIE_DATA=/tmp/junkie-dev/data
```

Guest mode talks to no server, so the desk works with nothing else running; `JUNKIE_URL` only matters once you sign in. To watch a block turn over without sitting out the clock, rewrite its end time — `endsAt` in `$JUNKIE_DATA/timer.json` for a guest block, or `phase_ends_at` on the open `timer_runs` row for a signed-in one.

### Tests

```sh
go test ./... -race     # server and terminal client
npm --prefix web test   # front end (vitest)
```

Neither suite needs a database or a running server.

## Deployment

The server needs PostgreSQL; SQLite is not supported. People *using* a deployed instance need only a browser.

See [DEPLOY.md](DEPLOY.md) for Render + Neon or self-hosting with the bundled compose files and Caddy.

If you put junkie behind your own proxy, make sure it sets `X-Forwarded-Proto` and `X-Forwarded-For` — the app relies on them to mark cookies `Secure` and to tell clients apart. Caddy and most platform routers do this already. `GET /healthz` returns `200 ok` when the app can reach the database.

## Security

Sessions are cookie-based and stored only as hashes. Cross-site requests and WebSocket handshakes are rejected, sign-in and signup are rate limited, and responses carry a Content-Security-Policy with all scripts served from the app itself.

The terminal client strips control characters from anything the server sends before drawing it. Room names and todo text are written by other people, and a terminal executes escape sequences that a browser would only display.

Found a vulnerability? Open an issue or contact the maintainer rather than filing a public exploit.



## Later

- Discord sign-in (the bot exists — see the Discord bot section above; OAuth sign-in does not yet). OAuth would also close the last password-reset gap: verifying your Discord in the browser instead of over DM works even without sharing a server with the bot.

