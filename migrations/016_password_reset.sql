-- Password reset links: single-use, expiring tokens minted either by the
-- Discord bot (/junkie reset-password, DM'd to the linked account) or by the
-- owner from the admin space for users without a linked Discord. Only the
-- token hash is stored, mirroring sessions and discord_link_tokens.
CREATE TABLE IF NOT EXISTS password_reset_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    admin_issued BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS password_reset_tokens_user_idx ON password_reset_tokens (user_id);
