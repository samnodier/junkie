-- API keys: a way for something that is not a browser -- a meeting-notes
-- automation, a script -- to drop todos into one list.
--
-- A key can do exactly one thing, add todos, and only to the list it was made
-- for: the owner's private todos (room_id null) or their own todos in one
-- room. It cannot read, complete, or delete anything, so a leaked key costs
-- some junk todos and nothing else.
--
-- Only the hash is stored, like sessions: the plaintext is shown once, when
-- the key is made. `prefix` is the first few characters, kept so the profile
-- page can tell keys apart without holding anything usable.
CREATE TABLE IF NOT EXISTS api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- A room key goes with its room; it must never fall back to aiming at
    -- the private list instead.
    room_id      UUID REFERENCES rooms(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    prefix       TEXT NOT NULL,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS api_keys_user_idx ON api_keys (user_id);

-- One row per Idempotency-Key a caller sent, holding the response it got, so
-- a retry after a timeout replays that answer instead of adding the todos a
-- second time. Scoped to the key, so two callers can't collide on a value.
-- Rows are swept after a day by the request path that writes them.
CREATE TABLE IF NOT EXISTS api_idempotency (
    api_key_id   UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    idem_key     TEXT NOT NULL,
    -- The body the key was first used with. The same key with a different
    -- body is a caller bug, and is refused rather than silently replayed.
    request_hash TEXT NOT NULL,
    status       INTEGER NOT NULL,
    response     JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (api_key_id, idem_key)
);

CREATE INDEX IF NOT EXISTS api_idempotency_created_idx ON api_idempotency (created_at);
