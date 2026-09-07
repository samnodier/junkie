-- When each participant actually joined the run.
--
-- Needed once someone can join a temporary room mid-session: crediting the
-- whole phase window to everyone would hand a latecomer focus minutes for
-- time they spent elsewhere. The credit now starts from the later of the
-- session's start and the moment they joined.
--
-- Existing rows default to now(), which is in the past relative to any
-- session that completes after this deploys, so they credit exactly as before.
ALTER TABLE timer_participants
    ADD COLUMN IF NOT EXISTS joined_at TIMESTAMPTZ NOT NULL DEFAULT now();
