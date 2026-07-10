-- Repair historical races before enforcing one authoritative active run per room.
UPDATE timer_runs
SET phase = 'ended',
    ended_at = COALESCE(ended_at, now())
WHERE room_id IS NOT NULL
  AND phase = 'ended'
  AND ended_at IS NULL;

WITH duplicate_runs AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY room_id
               ORDER BY created_at DESC, id DESC
           ) AS active_rank
    FROM timer_runs
    WHERE room_id IS NOT NULL
      AND ended_at IS NULL
      AND phase <> 'ended'
)
UPDATE timer_runs tr
SET phase = 'ended',
    ended_at = now()
FROM duplicate_runs duplicate
WHERE tr.id = duplicate.id
  AND duplicate.active_rank > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_timer_runs_one_active_room
ON timer_runs (room_id)
WHERE room_id IS NOT NULL
  AND ended_at IS NULL
  AND phase <> 'ended';
