-- Room-level roles, deliberately separate from the site-wide users.role in
-- 005_admin_roles.sql. A room admin can change that room's sound and manage
-- its membership -- nothing more. It grants no rights outside the room, no
-- view of another member's focus data, and no power to delete the room:
-- deleting stays with the creator alone.
--
-- The creator is NOT stored here. rooms.creator_id remains the single
-- ownership record, so the creator keeps their authority even if their
-- membership row is removed and re-added, and ownership transfer has exactly
-- one place to write.
ALTER TABLE room_members
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'member';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'room_members_role_valid'
          AND conrelid = 'room_members'::regclass
    ) THEN
        ALTER TABLE room_members
            ADD CONSTRAINT room_members_role_valid CHECK (role IN ('member', 'admin'));
    END IF;
END
$$;
