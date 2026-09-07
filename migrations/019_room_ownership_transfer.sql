-- Handing a room to someone else, with a window to take it back.
--
-- The transfer is recorded rather than applied: creator_id changes only once
-- the window has run out, so an accidental click is undone by clearing these
-- two columns and nothing else. Storing the deadline (rather than a timer)
-- is what lets the change settle correctly on a free instance that slept
-- through it -- the next read of the room is what applies it.
ALTER TABLE rooms
    ADD COLUMN IF NOT EXISTS pending_owner_id UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS ownership_transfer_at TIMESTAMPTZ;

-- Cheap enough to scan without, but this keeps the settle-on-read lookup off
-- a sequential scan once there are many rooms.
CREATE INDEX IF NOT EXISTS idx_rooms_ownership_transfer
    ON rooms(ownership_transfer_at)
    WHERE ownership_transfer_at IS NOT NULL;
