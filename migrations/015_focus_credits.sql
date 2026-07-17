-- Exact focus accounting across overlapping room runs: every completed focus
-- session claims its wall-clock window per participant, and a later-completing
-- session credits only the part of its window nobody claimed yet. The same
-- minute never counts twice and no real minute is dropped. This replaces the
-- all-or-nothing priority-of-joining rule, which credited whole sessions to
-- the earliest-joined live run and zeroed the others. Rows only matter while
-- an overlapping run can still complete, so they're pruned after two days.
CREATE TABLE IF NOT EXISTS focus_credits (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS focus_credits_user_window ON focus_credits (user_id, ended_at);
