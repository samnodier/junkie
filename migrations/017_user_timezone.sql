-- Focus sessions are credited to a calendar day, and that day was the
-- database server's (Postgres runs UTC), not the user's. A block finished at
-- 01:00 in Lagos landed on the previous day's square; one finished at 20:00 in
-- New York landed on the next. The heatmap, streaks and /junkie stats were all
-- off by a day for anyone far from UTC.
--
-- The browser reports its own IANA zone (Intl.DateTimeFormat), so nothing is
-- asked of the user. NULL means "not reported yet" and reads as UTC, which is
-- exactly the old behaviour — existing rows keep working untouched.
ALTER TABLE users ADD COLUMN IF NOT EXISTS timezone TEXT;
