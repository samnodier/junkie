package main

import (
	"net/http"
	"strings"

	"github.com/coder/websocket"
)

// The OBS overlay view of a temporary focus room: /f/{code}/embed.
//
// A browser source has its own empty cookie jar, so the authenticated
// /f/{code} screen would only ever redirect it to /login. These three
// handlers are the unauthenticated read-only twin of that screen — the page
// shell, one state endpoint, and the room socket — so a stream overlay shows
// the same live run everyone else is in.
//
// What keeps that safe:
//   - Temporary rooms only. Normal rooms 404 here, so nothing with a
//     persistent membership is reachable without a session.
//   - Codes are 48 bits from crypto/rand (see randomCode), so they can't be
//     enumerated: only someone holding the link gets in, which is already
//     true of the room itself.
//   - Read-only. There is no action endpoint, the socket discards whatever a
//     client sends (as the member socket does), and the payload carries no
//     todos and no viewer-specific state.
//
// embedViewer stands in for the absent account. User ids are UUIDs, so the
// viewer argument normalizeTimer takes has to parse as one; the nil UUID
// matches no participant row, which is exactly right for a guest.
const embedViewer = "00000000-0000-0000-0000-000000000000"

// focusEmbedPage serves the SPA shell for the overlay, 404ing anything that
// isn't a live temporary room — same rule as focusRoomPage.
func (a *app) focusEmbedPage(w http.ResponseWriter, r *http.Request) {
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok || !rm.Ephemeral {
		http.NotFound(w, r)
		return
	}
	a.spaPage(w, r)
}

// apiFocusEmbed is the overlay's state feed: the room's timer and who's in it,
// with none of the todos, membership, or viewer flags /api/room/{code} carries.
func (a *app) apiFocusEmbed(w http.ResponseWriter, r *http.Request) {
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok || !rm.Ephemeral {
		writeJSONError(w, http.StatusNotFound, "room not found")
		return
	}
	// Normalizing here, rather than reading the run as-is, is what lets the
	// overlay be the last viewer standing: phases are advanced by whoever
	// polls first, so an OBS source left running after the host closed their
	// tab still rolls focus into break instead of freezing on a dead phase.
	timer, transitioned, _ := a.normalizeTimer(r.Context(), rm.ID, embedViewer)
	if transitioned {
		a.broadcastTimerPhase(rm, timer)
	}
	// The run finishing deletes the room; answer with a 404 so the overlay
	// clears itself the way the member screen heads home.
	if transitioned && timer == nil {
		writeJSONError(w, http.StatusNotFound, "room not found")
		return
	}
	payload := map[string]any{
		"room": map[string]any{
			"code":         rm.Code,
			"name":         rm.Name,
			"focusMinutes": rm.FocusMinutes,
			"breakMinutes": rm.BreakMinutes,
			"autoSessions": rm.AutoSessions,
		},
		"timer": apiRoomTimer(timer),
	}
	// Before a run exists the heads come from the waiting list, so the overlay
	// isn't blank while people gather.
	if timer == nil {
		waiters, _ := a.roomWaitingUsers(r.Context(), rm.ID)
		payload["waiters"] = apiUsers(waiters)
	}
	writeJSON(w, payload)
}

// focusEmbedWS subscribes the overlay to the room's signal channel so it
// re-fetches on the same broadcasts members get, instead of polling.
func (a *app) focusEmbedWS(w http.ResponseWriter, r *http.Request) {
	code := normalizeRoomCode(strings.TrimPrefix(r.URL.Path, "/ws/f/"))
	rm, ok := a.findRoom(r.Context(), code)
	if !ok || !rm.Ephemeral {
		http.NotFound(w, r)
		return
	}
	// Default options enforce a same-origin handshake, blocking
	// cross-site WebSocket hijacking.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	a.hub.join(rm.Code, c)
	defer a.hub.leave(rm.Code, c)
	for {
		// Read-only: incoming frames are discarded, they exist only to keep
		// the connection's error state honest.
		if _, _, err := c.Read(r.Context()); err != nil {
			return
		}
	}
}
