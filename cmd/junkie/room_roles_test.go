package main

import (
	"os"
	"strings"
	"testing"
)

// migrate() re-runs every file on every boot, so 018 has to be idempotent --
// and it has to keep room roles to the two values canAdminRoomAs understands.
func TestRoomRolesMigrationIsIdempotentAndConstrained(t *testing.T) {
	migration, err := os.ReadFile("../../migrations/018_room_roles.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(migration)
	for _, expected := range []string{
		"ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'member'",
		"CHECK (role IN ('member', 'admin'))",
		"IF NOT EXISTS (",
	} {
		if !strings.Contains(sql, expected) {
			t.Errorf("migration missing %q", expected)
		}
	}
}

func TestCanAdminRoomAs(t *testing.T) {
	const creator, other = "creator-id", "other-id"

	for _, tc := range []struct {
		name       string
		userID     string
		memberRole string
		want       bool
	}{
		{"creator is always an admin", creator, roomRoleMember, true},
		{"creator with no membership row at all", creator, "", true},
		{"member promoted to admin", other, roomRoleAdmin, true},
		{"plain member is not an admin", other, roomRoleMember, false},
		{"non-member is not an admin", other, "", false},
		{"unknown role grants nothing", other, "owner", false},
		// A signed-out viewer reaches this through the public embed routes,
		// which carry no user at all; it must never read as the creator of a
		// room whose creator_id somehow came back empty.
		{"empty user is never an admin", "", roomRoleAdmin, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canAdminRoomAs(creator, tc.userID, tc.memberRole); got != tc.want {
				t.Errorf("canAdminRoomAs(%q, %q, %q) = %v, want %v", creator, tc.userID, tc.memberRole, got, tc.want)
			}
		})
	}
}

// The empty-creator case deserves its own test: an unset creator_id must not
// turn every non-member into an admin by matching "" == "".
func TestCanAdminRoomAsIgnoresEmptyCreator(t *testing.T) {
	if canAdminRoomAs("", "", roomRoleMember) {
		t.Error("empty creator and empty user must not grant admin")
	}
	if canAdminRoomAs("", "someone", roomRoleMember) {
		t.Error("empty creator must not grant admin to a plain member")
	}
}
