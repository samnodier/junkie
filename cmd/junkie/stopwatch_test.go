package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func activityMinutes(t *testing.T, a *app, u user) int {
	t.Helper()
	var m int
	_ = a.db.QueryRow(context.Background(), `SELECT COALESCE(SUM(focus_minutes), 0) FROM activity WHERE user_id = $1`, u.ID).Scan(&m)
	return m
}

func about(got, want int) bool { return got >= want-1 && got <= want+1 }

// A stopwatch room: someone who leaves early keeps the time they put in, and
// stopping credits whoever is still there for the time they were there --
// once each, never twice.
func TestRoomStopwatchCreditsLeaversAndStop(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")
	late := makeUser(t, a, "Late")
	ctx := context.Background()
	rm := makeTempRoom(t, a, host)
	if _, err := a.db.Exec(ctx, `UPDATE rooms SET stopwatch = true WHERE id = $1`, rm.ID); err != nil {
		t.Fatal(err)
	}
	rm.Stopwatch = true
	// The requested length is ignored: a stopwatch has none.
	if _, err := a.startRoomTimer(ctx, rm, host.ID, 25); err != nil {
		t.Fatal(err)
	}
	var runID string
	var focus, sessions int
	var flagged bool
	if err := a.db.QueryRow(ctx, `
		UPDATE timer_runs SET phase = 'focus', phase_started_at = now() - interval '40 minutes',
			phase_ends_at = now() + interval '140 minutes'
		WHERE room_id = $1 AND ended_at IS NULL
		RETURNING id, focus_minutes, total_sessions, stopwatch`, rm.ID).Scan(&runID, &focus, &sessions, &flagged); err != nil {
		t.Fatal(err)
	}
	if !flagged || focus != stopwatchMaxMinutes || sessions != 1 {
		t.Fatalf("run = stopwatch %v, %d minutes, %d sessions; want a single open-ended session", flagged, focus, sessions)
	}
	if _, err := a.db.Exec(ctx, `UPDATE timer_participants SET joined_at = now() - interval '40 minutes' WHERE timer_run_id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	a.addRoomMember(ctx, rm.ID, late.ID)
	if _, err := a.db.Exec(ctx, `
		INSERT INTO timer_participants (timer_run_id, user_id, confirmed_session, joined_at)
		VALUES ($1, $2, 1, now() - interval '10 minutes')`, runID, late.ID); err != nil {
		t.Fatal(err)
	}

	if ended, err := a.leaveTimer(ctx, rm, late.ID); err != nil || ended {
		t.Fatalf("leave: ended=%v err=%v, want the run to carry on for the host", ended, err)
	}
	if got := activityMinutes(t, a, late); !about(got, 10) {
		t.Errorf("leaver credited %d minutes, want about 10", got)
	}
	if got := activityMinutes(t, a, host); got != 0 {
		t.Errorf("host credited %d minutes before stopping, want 0", got)
	}

	if stopped, err := a.stopRoomStopwatch(ctx, rm.ID); err != nil || !stopped {
		t.Fatalf("stop: stopped=%v err=%v", stopped, err)
	}
	timer, transitioned, err := a.normalizeTimer(ctx, rm.ID, host.ID)
	if err != nil || !transitioned || timer != nil {
		t.Fatalf("after stop: timer=%+v transitioned=%v err=%v, want the run ended", timer, transitioned, err)
	}
	if got := activityMinutes(t, a, host); !about(got, 40) {
		t.Errorf("host credited %d minutes, want about 40", got)
	}
	if got := activityMinutes(t, a, late); !about(got, 10) {
		t.Errorf("leaver has %d minutes after the stop, want still about 10", got)
	}
	// A second stop has nothing to stop.
	if stopped, _ := a.stopRoomStopwatch(ctx, rm.ID); stopped {
		t.Error("stopped a stopwatch that had already ended")
	}
}

// A countdown room has no stop: the lever only exists on a stopwatch run.
func TestStopDoesNothingToACountdown(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")
	ctx := context.Background()
	rm := makeTempRoom(t, a, host)
	if _, err := a.startRoomTimer(ctx, rm, host.ID, rm.FocusMinutes); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'focus', phase_ends_at = now() + interval '20 minutes' WHERE room_id = $1`, rm.ID); err != nil {
		t.Fatal(err)
	}
	if stopped, err := a.stopRoomStopwatch(ctx, rm.ID); err != nil || stopped {
		t.Fatalf("stopped=%v err=%v, want a countdown left alone", stopped, err)
	}
}

func postAs(a *app, u user, h http.HandlerFunc, path string, form url.Values) *httptest.ResponseRecorder {
	a.asUser(u)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// A private stopwatch banks the minutes it counted and then offers a break,
// the same as a countdown that ran out; a second stop can't bank them again.
func TestSoloStopwatchBanksElapsedMinutes(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Solo")
	ctx := context.Background()

	postAs(a, u, a.startSoloTimer, "/solo/start", url.Values{"mode": {"stopwatch"}})
	timer, err := a.activeSoloTimer(ctx, u.ID)
	if err != nil || timer == nil || !timer.Stopwatch || timer.Phase != "focus" {
		t.Fatalf("after start: %+v %v, want a running stopwatch", timer, err)
	}
	if _, err := a.db.Exec(ctx, `UPDATE timer_runs SET phase_started_at = now() - interval '25 minutes' WHERE id = $1`, timer.ID); err != nil {
		t.Fatal(err)
	}

	postAs(a, u, a.stopSoloStopwatch, "/solo/stop", nil)
	if got := activityMinutes(t, a, u); got != 25 {
		t.Errorf("credited %d minutes, want 25", got)
	}
	timer, err = a.normalizeSoloTimer(ctx, u.ID)
	if err != nil || timer == nil || timer.Phase != "break" || !soloBreakPending(timer) || !timer.Stopwatch {
		t.Fatalf("after stop: %+v %v, want a pending break on the stopwatch run", timer, err)
	}

	postAs(a, u, a.stopSoloStopwatch, "/solo/stop", nil)
	if got := activityMinutes(t, a, u); got != 25 {
		t.Errorf("second stop changed the credit to %d", got)
	}

	// "Back to it" from that break is another stopwatch.
	postAs(a, u, a.skipSoloBreak, "/solo/break/skip", nil)
	timer, _ = a.activeSoloTimer(ctx, u.ID)
	if timer == nil || !timer.Stopwatch || timer.Phase != "focus" {
		t.Fatalf("after skipping the break: %+v, want a fresh stopwatch", timer)
	}
}

// Stopped within seconds, a stopwatch has nothing to bank and just ends.
func TestSoloStopwatchStoppedImmediatelyEndsWithoutCredit(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Solo")
	postAs(a, u, a.startSoloTimer, "/solo/start", url.Values{"mode": {"stopwatch"}})
	postAs(a, u, a.stopSoloStopwatch, "/solo/stop", nil)
	if timer, _ := a.activeSoloTimer(context.Background(), u.ID); timer != nil {
		t.Fatalf("run still active: %+v", timer)
	}
	if got := activityMinutes(t, a, u); got != 0 {
		t.Errorf("credited %d minutes for a stopwatch stopped at once", got)
	}
}

// Waves are open around the block and closed during it, and only the fixed
// phrases are accepted.
func TestWaveIsClosedDuringFocus(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")
	ctx := context.Background()
	rm := makeRoom(t, a, host)
	path := "/r/" + rm.Code + "/wave"

	if rec := postAs(a, host, a.roomAction, path, url.Values{"kind": {"hi"}}); strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Fatalf("wave in an idle room was refused: %s", rec.Header().Get("Location"))
	}
	if rec := postAs(a, host, a.roomAction, path, url.Values{"kind": {"anything else"}}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown wave answered %d, want 404", rec.Code)
	}

	if _, err := a.startRoomTimer(ctx, rm, host.ID, rm.FocusMinutes); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(ctx, `UPDATE timer_runs SET phase = 'focus', phase_ends_at = now() + interval '20 minutes' WHERE room_id = $1`, rm.ID); err != nil {
		t.Fatal(err)
	}
	if rec := postAs(a, host, a.roomAction, path, url.Values{"kind": {"hi"}}); !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Errorf("wave during focus was accepted (Location %q)", rec.Header().Get("Location"))
	}
}
