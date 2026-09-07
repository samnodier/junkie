-- A room's own end-of-block chime.
--
-- Stored here rather than in object storage: at half a megabyte a room this
-- is smaller than some avatars, it matches how avatars are already kept
-- (008), and a filesystem was never an option on an instance whose disk is
-- ephemeral. sound_name keeps the file's original name so the picker can show
-- what is actually set rather than just "custom".
ALTER TABLE rooms
    ADD COLUMN IF NOT EXISTS sound BYTEA,
    ADD COLUMN IF NOT EXISTS sound_name TEXT,
    ADD COLUMN IF NOT EXISTS sound_type TEXT,
    ADD COLUMN IF NOT EXISTS sound_updated_at TIMESTAMPTZ;
