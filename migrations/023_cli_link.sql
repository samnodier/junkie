-- Device-code pairing for the terminal client.
--
-- `junkie login` used to ask for the password in the terminal, which means
-- having it to hand and typing a credential into a program that did not need
-- to see it. Instead the CLI opens one of these rows, shows the short
-- user_code, and the person approves it in a browser where they are already
-- signed in.
--
-- Only the hash of the device token is stored, like sessions, password reset
-- tokens and discord link tokens: the plaintext lives in the CLI's memory
-- until it is exchanged. The session the CLI ends up with is minted at
-- exchange time and stored in `sessions` like any other, so nothing here ever
-- holds a usable credential.
CREATE TABLE IF NOT EXISTS cli_link_codes (
    device_hash TEXT PRIMARY KEY,
    -- Short, and read aloud off a terminal, so it is drawn from an alphabet
    -- with no 0/O or 1/I/L in it. Unique so an approval names one request.
    user_code   TEXT NOT NULL UNIQUE,
    -- Null until somebody approves it; the poll that finds it set is the one
    -- that mints the session and deletes the row.
    user_id     UUID REFERENCES users(id) ON DELETE CASCADE,
    approved_at TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS cli_link_codes_user_code_idx ON cli_link_codes (user_code);
CREATE INDEX IF NOT EXISTS cli_link_codes_expires_idx ON cli_link_codes (expires_at);
