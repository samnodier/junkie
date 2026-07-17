package main

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRequestedRoomFocusMinutes(t *testing.T) {
	tests := []struct {
		name     string
		values   url.Values
		fallback int
		want     int
		wantErr  bool
	}{
		{name: "saved room default", values: url.Values{}, fallback: 45, want: 45},
		{name: "focus minutes override", values: url.Values{"focus_minutes": {"75"}}, fallback: 45, want: 75},
		{name: "minutes alias", values: url.Values{"minutes": {"30"}}, fallback: 45, want: 30},
		{name: "minimum", values: url.Values{"focus_minutes": {"5"}}, fallback: 45, want: 5},
		{name: "maximum", values: url.Values{"focus_minutes": {"180"}}, fallback: 45, want: 180},
		{name: "below minimum", values: url.Values{"focus_minutes": {"4"}}, fallback: 45, wantErr: true},
		{name: "above maximum", values: url.Values{"focus_minutes": {"181"}}, fallback: 45, wantErr: true},
		{name: "not a number", values: url.Values{"focus_minutes": {"later"}}, fallback: 45, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/r/JUNK-IES/timer-start", strings.NewReader(tt.values.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			got, err := requestedRoomFocusMinutes(request, tt.fallback)
			if (err != nil) != tt.wantErr {
				t.Fatalf("requestedRoomFocusMinutes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("requestedRoomFocusMinutes() = %d, want %d", got, tt.want)
			}
		})
	}
}
