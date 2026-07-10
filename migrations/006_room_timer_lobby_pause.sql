ALTER TABLE timer_runs
    ADD COLUMN IF NOT EXISTS paused_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS paused_remaining_seconds INTEGER;

ALTER TABLE timer_runs DROP CONSTRAINT IF EXISTS timer_runs_phase_check;
ALTER TABLE timer_runs
    ADD CONSTRAINT timer_runs_phase_check
    CHECK (phase IN ('idle', 'lobby', 'focus', 'break', 'ended'));

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'timer_runs_pause_state_check'
          AND conrelid = 'timer_runs'::regclass
    ) THEN
        ALTER TABLE timer_runs
            ADD CONSTRAINT timer_runs_pause_state_check CHECK (
                (paused_at IS NULL AND paused_remaining_seconds IS NULL)
                OR
                (phase = 'break' AND paused_at IS NOT NULL AND paused_remaining_seconds IS NOT NULL
                    AND paused_remaining_seconds >= 0)
            );
    END IF;
END $$;
