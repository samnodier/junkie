package main

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func makeTempRoom(t *testing.T, a *app, creator user) room {
	t.Helper()
	rm := makeRoom(t, a, creator)
	if _, err := a.db.Exec(context.Background(), `UPDATE rooms SET ephemeral = true WHERE id = $1`, rm.ID); err != nil {
		t.Fatal(err)
	}
	rm, _ = a.findRoom(context.Background(), rm.Code)
	return rm
}

// A normal room's focus block is closed; a temporary room's is the whole
// point of sharing it while it runs.
func TestTemporaryRoomsCanBeJoinedMidSession(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")
	viewer := makeUser(t, a, "Viewer")
	ctx := context.Background()

	normal := makeRoom(t, a, host)
	a.addRoomMember(ctx, normal.ID, viewer.ID)
	if _, err := a.startRoomTimer(ctx, normal, host.ID, normal.FocusMinutes); err != nil {
		t.Fatal(err)
	}
	// Skip the lobby so the run is genuinely in focus.
	if _, err := a.db.Exec(ctx,
		`UPDATE timer_runs SET phase = 'focus', phase_started_at = now(), phase_ends_at = now() + interval '30 minutes'
		 WHERE room_id = $1 AND ended_at IS NULL`, normal.ID); err != nil {
		t.Fatal(err)
	}
	if _, outcome, err := a.joinTimer(ctx, normal, viewer.ID); err != nil {
		t.Fatal(err)
	} else if outcome != joinedQueuedBreak {
		t.Errorf("normal room outcome = %v, want the viewer queued for the break", outcome)
	}

	temp := makeTempRoom(t, a, host)
	a.addRoomMember(ctx, temp.ID, viewer.ID)
	if _, err := a.startRoomTimer(ctx, temp, host.ID, temp.FocusMinutes); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(ctx,
		`UPDATE timer_runs SET phase = 'focus', phase_started_at = now(), phase_ends_at = now() + interval '30 minutes'
		 WHERE room_id = $1 AND ended_at IS NULL`, temp.ID); err != nil {
		t.Fatal(err)
	}
	if _, outcome, err := a.joinTimer(ctx, temp, viewer.ID); err != nil {
		t.Fatal(err)
	} else if outcome != joinedNow {
		t.Errorf("temporary room outcome = %v, want the viewer straight in", outcome)
	}
	timer, err := a.activeTimer(ctx, temp.ID, viewer.ID)
	if err != nil || timer == nil || !timer.Participant {
		t.Fatalf("viewer is not in the run: %v %+v", err, timer)
	}
}

// Someone who walks in halfway is credited for the half they were there for,
// not the whole session.
//
// The participants are seated directly rather than through joinTimer, because
// joinTimer normalizes the run -- which would complete the session, and
// credit it, before the arrival times under test were set.
func TestMidSessionJoinerIsCreditedOnlyFromWhenTheyArrived(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")
	late := makeUser(t, a, "Late")
	ctx := context.Background()
	rm := makeTempRoom(t, a, host)
	if _, err := a.startRoomTimer(ctx, rm, host.ID, 60); err != nil {
		t.Fatal(err)
	}
	var runID string
	if err := a.db.QueryRow(ctx, `
		UPDATE timer_runs SET phase = 'focus', phase_started_at = now() - interval '60 minutes',
			phase_ends_at = now() - interval '1 second'
		WHERE room_id = $1 AND ended_at IS NULL RETURNING id`, rm.ID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	// The host has been there the whole hour; the latecomer, the last quarter.
	if _, err := a.db.Exec(ctx, `
		UPDATE timer_participants SET joined_at = now() - interval '60 minutes'
		WHERE timer_run_id = $1 AND user_id = $2`, runID, host.ID); err != nil {
		t.Fatal(err)
	}
	a.addRoomMember(ctx, rm.ID, late.ID)
	if _, err := a.db.Exec(ctx, `
		INSERT INTO timer_participants (timer_run_id, user_id, confirmed_session, joined_at)
		VALUES ($1, $2, 1, now() - interval '15 minutes')`, runID, late.ID); err != nil {
		t.Fatal(err)
	}

	// Completing the session is what credits it.
	if _, transitioned, err := a.normalizeTimer(ctx, rm.ID, host.ID); err != nil {
		t.Fatal(err)
	} else if !transitioned {
		t.Fatal("the session did not complete")
	}

	minutes := func(u user) int {
		var m int
		_ = a.db.QueryRow(ctx, `SELECT COALESCE(SUM(focus_minutes), 0) FROM activity WHERE user_id = $1`, u.ID).Scan(&m)
		return m
	}
	hostMinutes, lateMinutes := minutes(host), minutes(late)
	if hostMinutes < 55 || hostMinutes > 65 {
		t.Errorf("host credited %d minutes, want about 60", hostMinutes)
	}
	if lateMinutes < 10 || lateMinutes > 20 {
		t.Errorf("latecomer credited %d minutes, want about 15", lateMinutes)
	}
	if lateMinutes >= hostMinutes {
		t.Errorf("latecomer (%d) should be credited less than the host (%d)", lateMinutes, hostMinutes)
	}
}

// You set twelve, you're four in, you want ten.
func TestTemporaryRoomSessionsCanBeRetargetedMidRun(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")
	ctx := context.Background()
	rm := makeTempRoom(t, a, host)
	if _, err := a.db.Exec(ctx, `UPDATE rooms SET auto_sessions = 12 WHERE id = $1`, rm.ID); err != nil {
		t.Fatal(err)
	}
	rm, _ = a.findRoom(ctx, rm.Code)
	if _, err := a.startRoomTimer(ctx, rm, host.ID, rm.FocusMinutes); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(ctx,
		`UPDATE timer_runs SET current_session = 4, total_sessions = 12 WHERE room_id = $1 AND ended_at IS NULL`, rm.ID); err != nil {
		t.Fatal(err)
	}

	a.asUser(host)
	if _, errMsg := postRoomAction(t, a, rm.Code, "settings", url.Values{"auto_sessions": {"10"}}); errMsg != "" {
		t.Fatalf("retarget: %s", errMsg)
	}
	var total int
	_ = a.db.QueryRow(ctx, `SELECT total_sessions FROM timer_runs WHERE room_id = $1 AND ended_at IS NULL`, rm.ID).Scan(&total)
	if total != 10 {
		t.Errorf("run is aiming at %d sessions, want 10", total)
	}
	// The room's own setting follows, so the next run starts from what was
	// actually wanted.
	got, _ := a.findRoom(ctx, rm.Code)
	if got.AutoSessions != 10 {
		t.Errorf("room setting = %d, want 10", got.AutoSessions)
	}

	// Aiming below the session already running means "stop after this one".
	if _, errMsg := postRoomAction(t, a, rm.Code, "settings", url.Values{"auto_sessions": {"1"}}); errMsg != "" {
		t.Fatalf("retarget down: %s", errMsg)
	}
	_ = a.db.QueryRow(ctx, `SELECT total_sessions FROM timer_runs WHERE room_id = $1 AND ended_at IS NULL`, rm.ID).Scan(&total)
	if total != 4 {
		t.Errorf("run is aiming at %d, want the session in progress (4)", total)
	}
}

// A normal room's settings still can't move mid-run.
func TestNormalRoomSettingsStayLockedMidRun(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")
	ctx := context.Background()
	rm := makeRoom(t, a, host)
	if _, err := a.startRoomTimer(ctx, rm, host.ID, rm.FocusMinutes); err != nil {
		t.Fatal(err)
	}
	a.asUser(host)
	if _, errMsg := postRoomAction(t, a, rm.Code, "settings", url.Values{"auto_sessions": {"2"}}); errMsg == "" {
		t.Fatal("a normal room's settings should be locked while a run is active")
	}
}

// A sound chosen in the creation popup arrives with the room.
func TestTemporaryRoomCanBeCreatedWithItsOwnSound(t *testing.T) {
	a := newTestApp(t)
	host := makeUser(t, a, "Host")

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("ephemeral", "1")
	mw.WriteField("focus_minutes", "25")
	mw.WriteField("break_minutes", "5")
	mw.WriteField("auto_sessions", "4")
	part, err := mw.CreateFormFile("sound", "startup.wav")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(wavBytes(2000))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/rooms", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	a.asUser(host).createRoom(rec, req)

	dest := rec.Header().Get("Location")
	if len(dest) < 4 || dest[:3] != "/f/" {
		t.Fatalf("did not land on a temporary room: %q", dest)
	}
	code := dest[3:]
	if strings.Contains(code, "?") {
		t.Fatalf("room created with an error: %s", dest)
	}
	rm, ok := a.findRoom(context.Background(), code)
	if !ok {
		t.Fatal("room not found")
	}
	t.Cleanup(func() {
		_, _ = a.db.Exec(context.Background(), `DELETE FROM rooms WHERE id = $1`, rm.ID)
	})
	var name string
	if err := a.db.QueryRow(context.Background(),
		`SELECT sound_name FROM rooms WHERE id = $1 AND sound IS NOT NULL`, rm.ID).Scan(&name); err != nil {
		t.Fatalf("the room has no sound: %v", err)
	}
	if name != "startup.wav" {
		t.Errorf("stored name = %q, want %q", name, "startup.wav")
	}
}
