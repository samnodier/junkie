-- Waiting-room joins: a user parks here to be pulled into the room's next
-- focus opportunity automatically (run start, or the break of an active
-- run), instead of having to catch the lobby countdown live.
CREATE TABLE IF NOT EXISTS room_waiting (
    room_id UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (room_id, user_id)
);

-- Carry the Discord username through the link flow so the profile page can
-- show which Discord account is connected.
ALTER TABLE discord_link_tokens ADD COLUMN IF NOT EXISTS discord_username TEXT NOT NULL DEFAULT '';
ALTER TABLE discord_links ADD COLUMN IF NOT EXISTS discord_username TEXT NOT NULL DEFAULT '';
