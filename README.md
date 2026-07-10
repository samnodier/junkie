# junkie

`junkie` is a small shared focus app for study groups. It gives people a link they can open in the browser, create an account, join a persistent room, share public room todos, and run locked focus sessions with break windows.

There is no planting mechanic. Progress is represented by a GitHub-inspired work map of focused minutes per day.

## Where your data lives

junkie splits storage by whether you have an account. Solo use does not require signing up.

### Without an account (guest / solo)

These stay in **browser `localStorage` only** on the device you are using. They are **not** stored in PostgreSQL:

- Private todos (`junkie:todos`)
- Solo timer state (`junkie:soloTimer`)
- Work map / focus minutes per day (`junkie:activity`)

You can use junkie without creating an account. That data is tied to one browser profile on one device. Clearing site data or switching browsers or devices starts you over. Guest data is **not** automatically copied into an account when you sign up.

### With an account

These are stored in **PostgreSQL** on the server so they persist across sessions and devices:

- Account credentials and session
- Private todos
- Solo timer runs and completed focus minutes (work map)
- Room membership, room settings, room todos, and shared room timers

Accounts are only required to create or join shared rooms. Everything else works as a guest.

### Why guest data stays local

Keeping solo progress in the browser avoids anonymous rows in the database, cleanup overhead, and extra privacy/account complexity. It also keeps the hosted database small. Server storage is reserved for features that need it—mainly shared rooms and cross-device persistence.

### Keeping progress long-term

Create an account and use junkie while logged in. Your todos, timer history, and work map will then live in PostgreSQL and follow you across devices. **Cross-device sync requires signing in.** If you used junkie as a guest first, you will need to re-enter todos manually; guest `localStorage` does not migrate to your account today.

## V1 behavior

- Use solo without an account: private todos, solo timer, and work map stay in browser `localStorage` on your device.
- Accounts are only required to create or join shared rooms.
- Username/password accounts backed by PostgreSQL for room features.
- Persistent rooms with generated invite codes at `/r/{code}`.
- Anyone in a room can rename the room and edit timer defaults.
- Only the room creator can delete the room.
- Room todos are public to everyone in that room.
- Room timers run from server timestamps, so clients count down locally without timer spam.
- Focus blocks lock late joins out of the active timer.
- Break blocks allow new people to join for the next focus block.
- Participant avatars show who intentionally joined the active focus block, not
  who currently has the room tab visible or an open WebSocket. Hiding or closing
  a tab does not leave a block; participation ends through **Leave focus block**
  or when the timer ends.
- Focus mode hides todo boards so the app does not become the distraction.

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

Override it with `DATABASE_URL` if needed.

## Deployment and PostgreSQL

The server requires PostgreSQL. SQLite is not supported or bundled. People using a deployed junkie instance need only a browser; they do not need PostgreSQL or any local application installed.

Set `DATABASE_URL` to the connection string for your hosted PostgreSQL database:

```sh
DATABASE_URL='postgres://user:password@host/database?sslmode=require'
```

The server applies every idempotent SQL file in `migrations/` at startup, including role and audit-log schema changes. Run one app instance during a migration rollout, or apply the same migrations through your deployment pipeline before scaling out.

Managed PostgreSQL products such as Neon, Supabase, and Fly Managed Postgres are examples of deployment options. Plans and free-tier availability change, so check current pricing and limits rather than relying on a particular free offering.

Guest mode remains entirely in that browser's `localStorage`. A hosted database only persists signed-in account data across devices.

## Admin and owner access

Every account has one database-backed role:

- `user`: regular app access; cannot open `/admin`
- `admin`: operational aggregates, account metadata, room metadata, and audited room deletion
- `owner`: all admin access, aggregate per-user focus summaries, and audited promotion/demotion of admins

There is intentionally no public or in-app navigation link to `/admin`. Authorization is enforced server-side. Admins never receive password hashes, session tokens, private todo text, or detailed per-user activity history.

To bootstrap the first owner:

1. Create or sign in to the intended account once.
2. Set `JUNKIE_OWNER_USERNAME` to that account's username.
3. Restart junkie.

At startup, the configured account is promoted to `owner`. Changing or removing the variable never demotes an existing owner. A missing username is logged without exposing the configured value.

A trusted database operator can instead promote an initial owner directly:

```sql
UPDATE users SET role = 'owner' WHERE username = 'your_username';
```

Keep at least one owner. Owner accounts cannot be demoted through the web admin interface; changing an owner role requires trusted database access.

Admin mutations require same-origin browser requests, use `POST`, re-check actor and target roles from PostgreSQL, and write to `admin_audit_log`. Session cookies are `HttpOnly`, `SameSite=Lax`, and marked `Secure` when served over HTTPS.

## Later

- Terminal client using the same account and room API.
- Discord sign-in and a small bot for notifications/link sharing.
- Production deploy on a host that supports a long-running Go process and WebSockets, such as Fly.io or Railway.
