-- Per-session check-in: a room can require every participant to confirm,
-- during each break, that they're still there for the next session.
-- confirmed_session is the highest session number a participant has claimed a
-- seat in — starting a run or joining counts for the upcoming session, and any
-- break action (check-in, pause, resume, setting the length) counts for the
-- next one. The break->focus transition drops rows that never confirmed the
-- incoming session; nobody, including the starter, is exempt after session 1.
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS require_checkin BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE timer_participants ADD COLUMN IF NOT EXISTS confirmed_session INTEGER NOT NULL DEFAULT 1;
