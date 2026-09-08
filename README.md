# junkie

`junkie` is a small shared focus app for study groups. Open it in a browser, track what you need to finish, run solo or shared focus timers, and see progress on a GitHub-inspired work map of focused minutes per day.


| Light | Dark |
| ----- | ---- |
| ![The junkie desk in light mode: a focus ring beside a private todo list](assets/desk-light.png) | ![The junkie desk in dark mode: a focus ring beside a private todo list](assets/desk-dark.png) |




## How to use junkie



### Guest mode (no account)

You can use junkie without signing up. Everything stays in **browser** `localStorage` **on this device only** — it is not stored on the server:

- **Private todos** — add, complete, remove, and restore tasks on the desk. Completed and removed todos clean themselves up after 24 hours.
- **Solo timer** — run private focus sessions with optional breaks.
- **Work map** — a heatmap of focus minutes per day, stored locally.

Guest data is tied to one browser profile on one device. Clearing site data or switching browsers starts you over. Guest data is **not** copied into an account when you sign up later.

On the desk, the solo timer ring is adjustable before you start:

- Scroll the ring or use arrow keys to change by **±1 minute**.
- Use the **±5** buttons for larger steps.
- Tap the ring or press Enter/Space to start.

When a focus block ends, junkie offers a break. You can take it, adjust the break length on the ring, or **Skip break & continue** to start the next focus session immediately with the same duration.

### Accounts

Create an account when you want shared rooms or data that follows you across devices.

- **Sign up / sign in** — username and password. Usernames are lowercase letters, numbers, dots, dashes, and underscores (2–32 characters).
- **Username** — change it from Profile → Account.
- **Password change** — Profile → Security. Updating your password **signs out all other devices**.
- **Forgot your password?** — junkie never collects your email, so resets go through Discord. [Join the junkie Discord server](https://discord.gg/qEEzdXQHtK) and run `/junkie reset-password` in the `#junkie-bot` channel; the bot DMs you a single-use reset link that expires after 30 minutes (limited to 2 requests per account per day). (If your Discord is already linked, the command works from any server you share with the bot, or a DM with it.) If you can't use Discord at all, ask the admin in `#junkie-bot` or open a [GitHub issue](https://github.com/samnodier/junkie/issues) — the owner can issue you the same kind of reset link by hand. **Link your Discord before you need it**: linking requires being signed in, so it can't be done after the password is already forgotten.
- **Account deletion** — Profile → Danger zone. Permanently deletes your account, rooms you created, todos, and activity history. Requires your current password.
- **Profile picture** — upload or remove from Profile → Preferences.



### Solo focus (signed in)

Logged-in solo focus works like guest mode, but timer runs and completed focus minutes are stored in PostgreSQL and appear on your profile work map across devices.

- Adjust the ring (scroll ±1, buttons ±5, tap to start) before each session.
- Breaks can be taken, adjusted, or skipped with **Skip break & continue** to roll straight into the next focus block.
- Other tabs or devices signed into the same account pick up private todo changes live; solo timer phase changes still reload the desk (same as starting a session in another tab).



### Rooms

Accounts are required to create or join shared rooms. Each room has a persistent code and lives at `/r/{code}`.

**Joining and inviting**

- Create a room from the menu, or join with a room code or invite link.
- Each account can have up to **5 rooms**; deleting a room frees its slot.
- Invite links land on a confirmation screen before you join.
- When someone starts a focus lobby while junkie is in the background, you can get a **room invite notification** (toggle in Profile → Preferences).

**Room todos**

- Todos in a room are **public to every member**.
- Lists are grouped into **yours** and **everyone else's**.
- You can complete your own todos; you see teammates' todos read-only (with their display name and profile picture).
- Edits, removes, restores, and deletes apply only to **your** todos.
- Completed and removed todos are deleted automatically after 24 hours — the room board stays focused on current work.
- Room todo lists update **live** across the room page and the homepage desk switcher without full page reloads. When someone completes a task, other members see a short toast.
- On the homepage desk you can switch between **Private todos** and **Room todos** per room you belong to. Inside `/r/{code}` there is no switcher — that page is scoped to one room.

**Room timers**

- Shared focus runs from **server timestamps**; clients count down locally.
- Flow: **lobby** (optional join window) → **focus** → **break** → next focus session, for the configured number of sessions.
- The lobby card shows **who's joined so far** — an avatar stack and count right under the countdown ring, updated live.
- Anyone in the room can rename the room and edit timer defaults (focus length, break length, session count, auto-roll).
- Only the **room creator** can delete the room.
- During focus, late joins are locked out until the next break.
- **Pause / Resume** and **Skip break & continue** are available on breaks (any member, same as pause) — from the web room or as buttons on the Discord live message.
- **Auto-start breaks** (room settings): when **on**, breaks start automatically after focus. When **off**, the break waits on an adjustable ring until someone starts it or skips.
- **Abandoned pauses expire**: a break left paused for an hour — including a waiting break that nobody ever started — ends its run automatically, so the room is free for a fresh session instead of staying stuck on a resume that never comes. Nothing is lost: completed focus sessions were already credited.
- **Queued joins don't outlive the wait**: tapping Join mid-focus (or while the room is idle) parks you to board automatically — at the next break, or when the next run starts. That parking clears when its run ends for any reason (everyone left, sessions finished, an abandoned pause expired) and lapses after an hour regardless, so a fresh session starts with only the people who actually showed up for it, not whoever tapped Join hours or days earlier. Tap Join again and you're back in.
- **Session check-in** (room settings): when **on**, every participant must tap **"I'm here"** during each break to keep their seat in the next session — no-shows are dropped from the block when the next focus starts (they can rejoin at a later break). Joining, starting a run, or acting on the break (pause, resume, starting it) counts as your check-in; nobody is exempt, including whoever started the run. Skip break is disabled so the check-in window can't be cut short, and if nobody checks in the run ends. There's no check-in after the final session — the run ends when the last focus block does, with no trailing break. From Discord, tapping **Join** on the break message is your check-in (the break-control buttons count too, same as acting on the break from the web). During a check-in break the live message splits the room accordingly: the **In:** line names only those who have claimed the next session, and everyone still to check in is @mentioned on the reminder line beneath it, so the ping goes to exactly the people who need it (the first twenty, with a "+N more" note beyond that; anyone without a linked Discord account shows as a plain name).
- **Leave focus block** ends your participation in the active timer. Hiding or closing a tab does **not** leave — participant rings show who intentionally joined the block, not who has a tab open.
- **Focus mode** during an active session hides the full todo board so the timer stays central.



### Temporary focus rooms

Sometimes you just want a quick block with a few people, not a room that sticks around. From the menu, **Create temporary room** opens a config popup — set focus/break/sessions, whether breaks auto-run, and whether each session requires a check-in — and drops you onto a stripped-down screen at `/f/{code}` with a share link at the top.

- **Joined by link** — anyone signed in who opens the link is added and queued for the block, no confirmation step. They land straight on the waiting screen, so you can see who's here before you start.
- **Just the timer** — no todo board, no settings page, and the background grid is hidden for a distraction-free look. The invite link shows while you gather and during breaks, and disappears once focus starts.
- **Disposable** — the room deletes itself the moment the run finishes or everyone leaves, and sends everyone back home. Focus minutes still count toward each participant's work map.
- Temporary rooms **don't count** toward your 5-room limit and don't appear in your room list. Ones created but never started are cleaned up automatically.



### Connections

Connections are a separate, mutual link between two accounts — not room membership.

- From Profile → Connections, copy a **one-time connect link** and send it to someone. Each link works once; generate a new one for the next person.
- Opening a connect link shows a **confirmation screen** first — nothing is linked until the recipient clicks to accept (the Discord account link works the same way).
- After connecting, you each see the other's **focus heatmap only** on `/connections` and on public profile pages at `/{username}`.
- Profiles are **invisible to non-connections** — no public directory.
- There is no in-app way to disconnect or remove a connection yet, and the feed shows heatmaps only (no todos, rooms, or timers).



### Cross-device sync

Sign in on each device with the same account.


| What                | Sync                                          |
| ------------------- | --------------------------------------------- |
| Private todos       | Live across your tabs/devices (`/ws/me`)      |
| Room todos & timers | Live for all room members (room WebSocket)    |
| Solo timer phase    | Other tabs reload the desk when phase changes |
| Work map / activity | Stored server-side; same on every device      |
| Guest data          | Never syncs — local only                      |


On supported mobile browsers, junkie requests a **screen wake lock** while a timer is visible or a focus session is active so the phone is less likely to lock mid-session.

## Install on your phone

junkie is a PWA, so it installs to your home screen and runs fullscreen with no browser chrome.

- **iPhone / iPad** — open [junkie](https://junkie-blin.onrender.com) in Safari, tap Share, then **Add to Home Screen**.
- **Android** — either open it in Chrome and tap **Install app**, or download the signed APK from the [latest release](https://github.com/samnodier/junkie/releases/latest) and open it (you may need to allow installing from unknown sources).

Both are the same app pointing at the hosted site; the APK just wraps it so there's nothing to install from a browser.

## Terminal client

junkie also runs in the terminal. Bare `junkie` opens a full-screen desk you stay in — the countdown, your todos, and (once signed in) your rooms, with `tab` moving the countdown between your own block and each room's. No account is required: without one it is a guest desk on this machine, the same idea as the browser guest mode. Sign in to sync with the cloud account the web uses.

A block you start while signed in shows up on the web mid-countdown, and one you start on the web can be finished here — the server owns the clock either way, so closing the terminal never loses a block. Guest blocks live in `~/.local/share/junkie/` and likewise survive quitting; they are not copied into an account if you sign in later.

### Install

```sh
curl -fsSL https://raw.githubusercontent.com/samnodier/junkie/master/install.sh | sh
```

That puts a `junkie` binary in `~/.local/bin`. Set `JUNKIE_INSTALL_DIR` to choose somewhere else, or `JUNKIE_VERSION=vX.Y.Z` to pin a release. The script verifies the download against the release's published checksums before installing it.

With Go installed you can build it yourself instead:

```sh
go install github.com/samnodier/junkie/cmd/junkie-cli@latest
```

That names the binary `junkie-cli`, because `go install` takes the name from the directory and `cmd/junkie` is the server. Rename it to `junkie` if you want the shorter command.

### Signing in

You do not have to. `junkie` on its own opens a guest desk. Press `L` there, or run:

```sh
junkie login
```

It asks for your username and password — the same ones you use on the web — and stores the session in `~/.config/junkie/config.json`, mode `0600`. In a terminal it then opens the desk signed in. You stay signed in across terminals and reboots until the session expires **30 days after you signed in**; it does not renew as you use it, so roughly once a month you will be asked to sign in again. `junkie logout` ends it immediately, on the server as well as on disk.

Changing your password anywhere signs the terminal out too, because that ends every other session on the account.

### The desk

Run `junkie` on its own and you get the desk full-screen: a live countdown (the same block digits `junkie watch` used to be), your todos, and — signed in — what your rooms are doing. You stay in this program until you quit. A block ending offers the break on the same screen; it does not drop you back to the shell.

The keys are grouped along the bottom of the screen the way they are grouped here — the todo list, the block, and the desk itself:

| Group | Key | Does |
| ----- | --- | ---- |
| todos | `j` `k` | move down and up the list |
| todos | `g` `G` | jump to the top and the bottom of it |
| todos | `space` | complete or un-complete |
| todos | `a` / `e` / `d` / `u` | add · edit · remove · undo the last remove |
| block | `f` / `b` / `s` / `c` | start focus · take the break · skip it · cancel the block |
| desk | `tab` | show the next block: your own, then each room |
| desk | `w` | timer pane fills the window (same as `junkie watch`; `esc` or `q` returns) |
| desk | `r` / `q` | refresh · quit (`q` backs out of the zoomed pane first) |
| desk | `L` | sign in (guest desk) |
| desk | `y` / `n` | answer a room's join prompt |

In a narrow window each row drops its last hints, and in a short one they collapse back to a single line. A message the desk puts up — "no break is waiting" — clears itself after a few seconds, and the next key you press clears it immediately.

### Two blocks at once

Your own block and a room's block run independently — you can be in both — and the countdown shows one at a time. `tab` moves between them: your private block first, then each room you are in. Whichever is on screen is named above the digits and marked in the room list below them, so there is never a question of which block you are looking at.

With a room on screen the timer keys act on that room, and two more apply:

| Key | Does |
| --- | ---- |
| `f` / `b` / `s` | start a block · take the break · skip it |
| `i` | I'm in — join the block, or check in for the next one |
| `x` | leave the block — asks first, since `x` sits next to `i` and a block you have left will not always take you back |

The todo list follows the countdown. With a room on screen it is that room's list — your own todos to work, everyone else's to read, each named. `a` there adds to the room rather than to your private list.

The terminal is deliberately not the whole of junkie: room members, settings, temporary rooms and admin live on the web and in Discord. What it does is the part a terminal is good at — a block running in a pane while you work.

You do not have to reach for `tab` in the first place: `junkie` opens on a room whose block is live rather than on a private timer that is not running, and `junkie CODE` opens straight onto one. Answering `y` to a join prompt also brings that room's block to the front, since you just said you were joining it.

When someone starts a block in one of your rooms, the desk asks whether you want in and counts down the 30 seconds you have to answer. Not answering is an answer: the block starts without you. This only reaches you while the desk is open — when you are away, the Discord bot is what notifies you.

### Commands

| Command | Does |
| ------- | ---- |
| `junkie` | open the desk full-screen |
| `junkie CODE` | open the desk on that room's block |
| `junkie status` | timer, todo counts and room activity, in one glance |
| `junkie focus [MINUTES]` | start a private block (5–180, default 50) |
| `junkie break [MINUTES]` · `junkie skip` · `junkie cancel` | take the offered break, skip it, or end the block early |
| `junkie watch [CODE]` | the desk, timer pane filling the window — yours, or a room's |
| `junkie todos` · `junkie rooms` | list them |
| `junkie stats` | the work map: a year of focused days |
| `junkie room new NAME` · `junkie room join CODE` | create or join a room |
| `junkie room start [CODE] [MINUTES]` | start a room's block, opening the 30-second lobby |
| `junkie room enter` · `leave` · `checkin` · `skip` | act on the block that's running |
| `junkie whoami` · `junkie logout` | who this terminal is, and sign out |

`junkie room` commands take a room code, and you can leave it out when you are only in one room. Pasting a room's URL works as well as typing its code.

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

Linking Discord is also your password lifeline: `/junkie reset-password` DMs a single-use reset link to the linked account, so a forgotten password never needs an email (see [Accounts](#accounts)). This is why the public [junkie Discord server](https://discord.gg/qEEzdXQHtK) exists — joining it gives anyone a place to run these commands without needing their own server.

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

Guest mode talks to no server, so the desk and `junkie focus` work with nothing else running; `JUNKIE_URL` only matters once you sign in. To watch a block turn over without sitting out the clock, rewrite its end time — `endsAt` in `$JUNKIE_DATA/timer.json` for a guest block, or `phase_ends_at` on the open `timer_runs` row for a signed-in one.

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

