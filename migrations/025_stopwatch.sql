-- Stopwatch mode: a run that counts up until somebody stops it, instead of
-- counting down a length chosen in advance.
--
-- A stopwatch run is an ordinary single-session focus run whose deadline is
-- the stopwatch ceiling; stopping it pulls that deadline in to "now", and the
-- existing phase machinery credits the elapsed time and ends the run. The
-- flag on the run tells every screen to count up rather than down.
--
-- The flag on the room is the mode a temporary room was created in, since
-- the room exists before its run does. Ordinary rooms never set it.
ALTER TABLE timer_runs ADD COLUMN IF NOT EXISTS stopwatch BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS stopwatch BOOLEAN NOT NULL DEFAULT false;
