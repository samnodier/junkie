-- Mutual connections between users, stored once per pair with the lower
-- UUID first so a pair can't appear twice in either order.
CREATE TABLE IF NOT EXISTS connections (
    user_a UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_a, user_b),
    CONSTRAINT connections_ordered CHECK (user_a < user_b)
);
CREATE INDEX IF NOT EXISTS idx_connections_user_b ON connections(user_b);

-- One outstanding single-use connect invite per user; the raw token only
-- ever appears in the generated link, the table keeps its hash.
CREATE TABLE IF NOT EXISTS connect_tokens (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
