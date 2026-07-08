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

Create an account and use junkie while logged in. Your todos, timer history, and work map will then live in PostgreSQL and follow you across devices. If you used junkie as a guest first, you will need to re-enter todos manually; guest `localStorage` does not migrate to your account today.

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

## Later

- Terminal client using the same account and room API.
- Discord sign-in and a small bot for notifications/link sharing.
- Production deploy on a host that supports a long-running Go process and WebSockets, such as Fly.io or Railway.
