package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func testAppAs(u user) *app {
	return &app{
		currentUserOverride: func(*http.Request) (user, bool) {
			if u.ID == "" {
				return user{}, false
			}
			return u, true
		},
	}
}

func TestRoleMigrationDefaultsAndValidatesRole(t *testing.T) {
	migration, err := os.ReadFile("../../migrations/005_admin_roles.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(migration)
	for _, expected := range []string{
		"DEFAULT 'user'",
		"CHECK (role IN ('user', 'admin', 'owner'))",
		"CREATE TABLE IF NOT EXISTS admin_audit_log",
	} {
		if !strings.Contains(sql, expected) {
			t.Errorf("migration missing %q", expected)
		}
	}
}

func TestRequireAdminRedirectsUnauthenticated(t *testing.T) {
	a := testAppAs(user{})
	request := httptest.NewRequest(http.MethodGet, "/admin?tab=users", nil)
	response := httptest.NewRecorder()

	a.requireAdmin(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler was called")
	})(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if location := response.Header().Get("Location"); location != "/login?next=%2Fadmin%3Ftab%3Dusers" {
		t.Errorf("location = %q", location)
	}
}

func TestRequireAdminForbidsUser(t *testing.T) {
	a := testAppAs(user{ID: "user-id", Username: "reader", Role: roleUser})
	response := httptest.NewRecorder()

	a.requireAdmin(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler was called")
	})(response, httptest.NewRequest(http.MethodGet, "/admin", nil))

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if !strings.Contains(response.Body.String(), `<div id="app">`) {
		t.Error("SPA shell was not served on the forbidden page")
	}
}

func TestAdminCanAccessAdminSpace(t *testing.T) {
	a := testAppAs(user{ID: "admin-id", Username: "operator", Role: roleAdmin})
	called := false
	response := httptest.NewRecorder()
	a.requireAdmin(func(http.ResponseWriter, *http.Request) { called = true })(
		response,
		httptest.NewRequest(http.MethodGet, "/admin", nil),
	)
	if !called {
		t.Fatal("admin did not reach protected handler")
	}
}

func TestOwnerRoleTransitionsAndSoleOwnerProtection(t *testing.T) {
	if !canChangeRole(roleOwner, roleUser, roleAdmin) {
		t.Error("owner should be able to promote user")
	}
	if !canChangeRole(roleOwner, roleAdmin, roleUser) {
		t.Error("owner should be able to demote admin")
	}
	for _, test := range []struct{ actor, target, requested string }{
		{roleAdmin, roleUser, roleAdmin},
		{roleAdmin, roleAdmin, roleUser},
		{roleOwner, roleOwner, roleUser},
		{roleOwner, roleOwner, roleAdmin},
	} {
		if canChangeRole(test.actor, test.target, test.requested) {
			t.Errorf("%s changing %s to %s should be denied", test.actor, test.target, test.requested)
		}
	}
}

func TestAdminMutationRequiresSameOrigin(t *testing.T) {
	a := testAppAs(user{ID: "owner-id", Role: roleOwner})
	request := httptest.NewRequest(http.MethodPost, "https://junkie.test/admin/users/id/role", nil)
	request.Host = "junkie.test"
	response := httptest.NewRecorder()
	a.requireAdminMutation(func(http.ResponseWriter, *http.Request) {
		t.Fatal("mutation handler was called without Origin")
	})(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}
