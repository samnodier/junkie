-- Temporary "focus rooms" (Forest-style): created from a config popup, joined
-- by link, and deleted the moment their run completes or empties out. The flag
-- keeps them out of the room list and the per-user room cap, and marks them for
-- the /f/{code} single-screen view and the abandoned-room janitor.
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS ephemeral BOOLEAN NOT NULL DEFAULT false;
