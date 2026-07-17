package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A reset link is account takeover, so issuing one takes the owner role —
// admins who can otherwise use the admin space must be refused before any
// token is minted (the handler exits before touching the database).
func TestAdminResetLinkForbidsNonOwner(t *testing.T) {
	a := testAppAs(user{ID: "admin-id", Username: "operator", Role: roleAdmin})
	request := httptest.NewRequest(http.MethodPost, "https://junkie.test/admin/users/user-id/reset-link", nil)
	request.Host = "junkie.test"
	request.Header.Set("Origin", "https://junkie.test")
	request.SetPathValue("id", "user-id")
	response := httptest.NewRecorder()

	a.requireAdminMutation(a.adminCreateResetLink)(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestResetPasswordPathIsNotAValidUsername(t *testing.T) {
	if validUsername("reset-password") {
		t.Error("reset-password must stay reserved so profile URLs cannot shadow the reset page")
	}
}
