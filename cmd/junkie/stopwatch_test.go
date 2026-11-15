package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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
			phase_ends_at = now() + interval '320 minutes'
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

// A room stopwatch that reaches its ceiling pauses instead of ending: the
// stretch is banked, anyone in it can continue for one more stretch, and the
// clock carries on from where it held. Stopping it from the pause closes
// the run without crediting anything twice.
func TestRoomStopwatchPausesAtCeilingAndContinues(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")
	ctx := context.Background()
	rm := makeTempRoom(t, a, host)
	if _, err := a.db.Exec(ctx, `UPDATE rooms SET stopwatch = true WHERE id = $1`, rm.ID); err != nil {
		t.Fatal(err)
	}
	rm.Stopwatch = true
	if _, err := a.startRoomTimer(ctx, rm, host.ID, 25); err != nil {
		t.Fatal(err)
	}
	// The whole stretch ran: started six hours ago, ceiling reached just now.
	var runID string
	if err := a.db.QueryRow(ctx, `
		UPDATE timer_runs SET phase = 'focus', phase_started_at = now() - interval '360 minutes',
			phase_ends_at = now() - interval '2 seconds'
		WHERE room_id = $1 AND ended_at IS NULL RETURNING id`, rm.ID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(ctx, `UPDATE timer_participants SET joined_at = now() - interval '360 minutes' WHERE timer_run_id = $1`, runID); err != nil {
		t.Fatal(err)
	}

	timer, transitioned, err := a.normalizeTimer(ctx, rm.ID, host.ID)
	if err != nil || !transitioned || !timer.StopwatchPaused() {
		t.Fatalf("at the ceiling: timer=%+v transitioned=%v err=%v, want it paused", timer, transitioned, err)
	}
	if got := timer.StopwatchBankedSeconds(); got != stopwatchMaxMinutes*60 {
		t.Errorf("paused at %d seconds, want %d", got, stopwatchMaxMinutes*60)
	}
	if got := activityMinutes(t, a, host); !about(got, 360) {
		t.Errorf("credited %d minutes at the pause, want about 360", got)
	}

	// Continue: one more stretch, counted on top of the first.
	timer, changed, err := a.skipRoomBreak(ctx, rm.ID, host.ID)
	if err != nil || !changed || timer == nil || timer.Phase != "focus" || timer.StopwatchPaused() {
		t.Fatalf("after continue: timer=%+v changed=%v err=%v, want it running", timer, changed, err)
	}
	if got := timer.StopwatchBankedSeconds(); got != stopwatchMaxMinutes*60 {
		t.Errorf("continued from %d seconds, want %d", got, stopwatchMaxMinutes*60)
	}
	if left := time.Until(timer.PhaseEndsAt); left < 359*time.Minute || left > 361*time.Minute {
		t.Errorf("continued stretch ends in %v, want about six hours", left)
	}

	// Run that stretch out too, then stop from the second pause.
	if _, err := a.db.Exec(ctx, `
		UPDATE timer_runs SET phase_started_at = now() - interval '360 minutes', phase_ends_at = now() - interval '2 seconds'
		WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	if timer, _, err = a.normalizeTimer(ctx, rm.ID, host.ID); err != nil || !timer.StopwatchPaused() {
		t.Fatalf("second ceiling: timer=%+v err=%v, want it paused again", timer, err)
	}
	if got := timer.StopwatchBankedSeconds(); got != 2*stopwatchMaxMinutes*60 {
		t.Errorf("second pause at %d seconds, want %d", got, 2*stopwatchMaxMinutes*60)
	}
	if stopped, err := a.stopRoomStopwatch(ctx, rm.ID); err != nil || !stopped {
		t.Fatalf("stop from the pause: stopped=%v err=%v", stopped, err)
	}
	timer, transitioned, err = a.normalizeTimer(ctx, rm.ID, host.ID)
	if err != nil || !transitioned || timer != nil {
		t.Fatalf("after stop: timer=%+v transitioned=%v err=%v, want the run ended", timer, transitioned, err)
	}
	// The two stretches overlap on the wall clock in this test (both were
	// backdated to the same six hours), so the claim ledger credits the
	// window once: what matters is that stopping added nothing.
	if got := activityMinutes(t, a, host); !about(got, 360) {
		t.Errorf("credited %d minutes in all, want about 360", got)
	}
}

// A pause nobody answers for an hour ends the run, like any abandoned pause;
// the stretch it paused on stays banked.
func TestRoomStopwatchPauseLeftForAnHourEnds(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")
	ctx := context.Background()
	rm := makeTempRoom(t, a, host)
	if _, err := a.db.Exec(ctx, `UPDATE rooms SET stopwatch = true WHERE id = $1`, rm.ID); err != nil {
		t.Fatal(err)
	}
	rm.Stopwatch = true
	if _, err := a.startRoomTimer(ctx, rm, host.ID, 25); err != nil {
		t.Fatal(err)
	}
	var runID string
	if err := a.db.QueryRow(ctx, `
		UPDATE timer_runs SET phase = 'focus', phase_started_at = now() - interval '390 minutes',
			phase_ends_at = now() - interval '30 minutes'
		WHERE room_id = $1 AND ended_at IS NULL RETURNING id`, rm.ID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(ctx, `UPDATE timer_participants SET joined_at = now() - interval '390 minutes' WHERE timer_run_id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	// Half an hour past the ceiling: paused, dated at the ceiling.
	timer, _, err := a.normalizeTimer(ctx, rm.ID, host.ID)
	if err != nil || !timer.StopwatchPaused() {
		t.Fatalf("timer=%+v err=%v, want it paused", timer, err)
	}
	// Another half hour and a bit: the pause has stood for over an hour.
	if _, err := a.db.Exec(ctx, `
		UPDATE timer_runs SET phase_started_at = phase_started_at - interval '31 minutes',
			phase_ends_at = phase_ends_at - interval '31 minutes', paused_at = paused_at - interval '31 minutes'
		WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	timer, transitioned, err := a.normalizeTimer(ctx, rm.ID, host.ID)
	if err != nil || !transitioned || timer != nil {
		t.Fatalf("after an hour paused: timer=%+v transitioned=%v err=%v, want the run ended", timer, transitioned, err)
	}
	if got := activityMinutes(t, a, host); !about(got, 360) {
		t.Errorf("credited %d minutes, want the stretch (about 360) kept", got)
	}
}

// The private stopwatch does the same: pauses at the ceiling with the stretch
// banked, continues from where it held, stops into the usual break offer,
// and a pause left for an hour ends the run.
func TestSoloStopwatchPausesContinuesAndExpires(t *testing.T) {
	a := newTestApp(t)
	u := makeUser(t, a, "Solo")
	ctx := context.Background()
	ranOut := `UPDATE timer_runs SET phase_started_at = now() - interval '360 minutes', phase_ends_at = now() - interval '2 seconds'
		WHERE user_id = $1 AND room_id IS NULL AND ended_at IS NULL`

	postAs(a, u, a.startSoloTimer, "/solo/start", url.Values{"mode": {"stopwatch"}})
	if _, err := a.db.Exec(ctx, ranOut, u.ID); err != nil {
		t.Fatal(err)
	}
	timer, err := a.normalizeSoloTimer(ctx, u.ID)
	if err != nil || !timer.StopwatchPaused() || timer.StopwatchBankedSeconds() != 360*60 {
		t.Fatalf("at the ceiling: %+v %v, want it paused at six hours", timer, err)
	}
	if got := activityMinutes(t, a, u); got != 360 {
		t.Errorf("credited %d minutes at the pause, want 360", got)
	}

	postAs(a, u, a.continueSoloStopwatch, "/solo/continue", nil)
	timer, err = a.normalizeSoloTimer(ctx, u.ID)
	if err != nil || timer == nil || timer.Phase != "focus" || timer.StopwatchBankedSeconds() != 360*60 {
		t.Fatalf("after continue: %+v %v, want it running on from six hours", timer, err)
	}
	// Ten minutes into the second stretch, stop: only those ten are new.
	if _, err := a.db.Exec(ctx, `UPDATE timer_runs SET phase_started_at = now() - interval '10 minutes' WHERE id = $1`, timer.ID); err != nil {
		t.Fatal(err)
	}
	postAs(a, u, a.stopSoloStopwatch, "/solo/stop", nil)
	if got := activityMinutes(t, a, u); got != 370 {
		t.Errorf("credited %d minutes after stopping, want 370", got)
	}
	timer, err = a.normalizeSoloTimer(ctx, u.ID)
	if err != nil || timer == nil || timer.StopwatchPaused() || !soloBreakPending(timer) {
		t.Fatalf("after stop: %+v %v, want an ordinary pending break", timer, err)
	}
	postAs(a, u, a.skipSoloBreak, "/solo/break/skip", nil)

	// "Back to it" started a fresh stopwatch. Run it out, stop from the pause.
	if _, err := a.db.Exec(ctx, ranOut, u.ID); err != nil {
		t.Fatal(err)
	}
	if timer, err = a.normalizeSoloTimer(ctx, u.ID); err != nil || !timer.StopwatchPaused() {
		t.Fatalf("second run at the ceiling: %+v %v, want it paused", timer, err)
	}
	postAs(a, u, a.stopSoloStopwatch, "/solo/stop", nil)
	if got := activityMinutes(t, a, u); got != 730 {
		t.Errorf("credited %d minutes, want 730: stopping a paused stopwatch banks nothing more", got)
	}
	if timer, err = a.normalizeSoloTimer(ctx, u.ID); err != nil || timer == nil || timer.StopwatchPaused() || !soloBreakPending(timer) {
		t.Fatalf("stopped from the pause: %+v %v, want an ordinary pending break", timer, err)
	}
	postAs(a, u, a.skipSoloBreak, "/solo/break/skip", nil)

	// A third run, paused and then left alone for over an hour: gone.
	if _, err := a.db.Exec(ctx, `UPDATE timer_runs SET phase_started_at = now() - interval '425 minutes', phase_ends_at = now() - interval '65 minutes'
		WHERE user_id = $1 AND room_id IS NULL AND ended_at IS NULL`, u.ID); err != nil {
		t.Fatal(err)
	}
	if timer, err = a.normalizeSoloTimer(ctx, u.ID); err != nil || timer != nil {
		t.Fatalf("paused for over an hour: %+v %v, want the run ended", timer, err)
	}
	if got := activityMinutes(t, a, u); got != 1090 {
		t.Errorf("credited %d minutes, want 1090: the abandoned stretch is still saved", got)
	}
	// And a stale tab's Continue can't bring it back.
	postAs(a, u, a.continueSoloStopwatch, "/solo/continue", nil)
	if timer, _ = a.activeSoloTimer(ctx, u.ID); timer != nil {
		t.Errorf("continue revived an ended run: %+v", timer)
	}
}

// A temporary room opened at the ordinary room address is sent on to its own
// screen, which is the only one that knows what a stopwatch is.
func TestRoomPageSendsTemporaryRoomsToTheirOwnScreen(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")
	rm := makeTempRoom(t, a, host)
	a.asUser(host)
	rec := httptest.NewRecorder()
	a.roomPage(rec, httptest.NewRequest(http.MethodGet, "/r/"+rm.Code+"?error=nope", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/f/"+rm.Code+"?error=nope" {
		t.Errorf("got %d to %q, want a redirect to /f/%s with the query kept", rec.Code, rec.Header().Get("Location"), rm.Code)
	}
}
