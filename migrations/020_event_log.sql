-- Widen the staff audit log into the service's event log.
--
-- It already recorded what administrators did. What it never recorded is what
-- happened -- who joined a room, who was removed from one, who was made an
-- admin, when a room changed hands. Those are exactly the events someone
-- later disputes, and they are impossible to reconstruct after the fact.
--
-- Deliberately not recorded: focus sessions. "Someone focused for 25 minutes"
-- is the most numerous event in the system and the least useful to read back,
-- and the work map already tells that story better.
ALTER TABLE admin_audit_log
    ADD COLUMN IF NOT EXISTS room_id UUID REFERENCES rooms(id) ON DELETE CASCADE;

-- The room log reads one room newest-first, so index it that way.
CREATE INDEX IF NOT EXISTS idx_audit_log_room_created
    ON admin_audit_log(room_id, created_at DESC)
    WHERE room_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_audit_log_created
    ON admin_audit_log(created_at DESC);

-- actor_user_id already ON DELETE SET NULL: an account closing must not erase
-- the record of what it did to other people's rooms, so the row outlives the
-- user and simply loses the name.
