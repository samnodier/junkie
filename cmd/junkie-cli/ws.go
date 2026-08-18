package main

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// The server pushes over two kinds of channel: /ws/me carries this user's
// own changes ("solo-timer", "todos"), and /ws/r/{code} carries a room's
// ("timer-phase", "timer-lobby", "todo-done", …). The desk subscribes to its
// user channel and to one socket per room, which is exactly what the web
// does — see the loop in web/src/stores/desk.js.
//
// Both accept this client because coder/websocket's handshake check passes a
// request with no Origin header, the same reason the HTTP mutations get past
// CrossOriginProtection. The client sends none.

// signal is one message. The server sends either a bare string or a JSON
// object with a type field, so kind carries whichever it was and event holds
// the payload when there was one.
type signal struct {
	room  string
	kind  string
	event map[string]any
}

// sockets holds every live connection for a desk, and reconnects each one on
// its own schedule.
type sockets struct {
	events chan signal
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// openSockets subscribes to the user channel and to each room, and returns
// as soon as the goroutines are started — nothing here blocks on a
// connection, because a server that is asleep (Render's free tier) must not
// hold up the screen.
func openSockets(c *client, rooms []string) *sockets {
	ctx, cancel := context.WithCancel(context.Background())
	s := &sockets{events: make(chan signal, 32), cancel: cancel}

	s.listen(ctx, c, "/ws/me", "")
	for _, code := range rooms {
		s.listen(ctx, c, "/ws/r/"+code, code)
	}
	return s
}

func (s *sockets) listen(ctx context.Context, c *client, path, room string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(ctx, c, path, room)
	}()
}

// run keeps one channel connected for as long as the desk is open, retrying
// with the same backoff shape the browser client uses.
func (s *sockets) run(ctx context.Context, c *client, path, room string) {
	for attempt := 0; ctx.Err() == nil; {
		if err := s.pump(ctx, c, path, room); err == nil {
			attempt = 0
		} else {
			attempt++
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
	}
}

// backoff is 500ms doubling to a 15s ceiling, matching web/src/lib/ws.js so
// a flapping server sees the same load from either client.
func backoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 500 * time.Millisecond
	}
	if attempt > 5 {
		attempt = 5
	}
	d := time.Duration(500*math.Pow(2, float64(attempt))) * time.Millisecond
	if d > 15*time.Second {
		d = 15 * time.Second
	}
	return d
}

// pump holds one connection open, forwarding messages until it drops.
func (s *sockets) pump(ctx context.Context, c *client, path, room string) error {
	header := http.Header{}
	if c.token != "" {
		header.Set("Cookie", sessionCookie+"="+c.token)
	}
	conn, _, err := websocket.Dial(ctx, websocketURL(c.baseURL)+path, &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		select {
		case s.events <- parseSignal(room, data):
		case <-ctx.Done():
			return ctx.Err()
		default:
			// A desk that cannot keep up drops the signal rather than
			// blocking the reader: every one of them only means "re-read",
			// and the next will say the same thing.
		}
	}
}

// websocketURL swaps the scheme; everything else about the address is the
// same server the HTTP calls go to.
func websocketURL(baseURL string) string {
	switch {
	case strings.HasPrefix(baseURL, "https://"):
		return "wss://" + strings.TrimPrefix(baseURL, "https://")
	case strings.HasPrefix(baseURL, "http://"):
		return "ws://" + strings.TrimPrefix(baseURL, "http://")
	default:
		return baseURL
	}
}

// parseSignal reads the server's two message shapes: a bare string, or JSON
// carrying its own type.
func parseSignal(room string, data []byte) signal {
	text := string(data)
	if !strings.HasPrefix(strings.TrimSpace(text), "{") {
		return signal{room: room, kind: text}
	}
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return signal{room: room, kind: text}
	}
	kind, _ := event["type"].(string)
	if kind == "" {
		kind = text
	}
	return signal{room: room, kind: kind, event: event}
}

func (s *sockets) close() {
	if s == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
}

// eventString reads a string field out of a signal's payload.
func (s signal) str(key string) string {
	v, _ := s.event[key].(string)
	return v
}

// eventTime reads an RFC3339 field — the deadlines the server sends with a
// lobby or a break invite.
func (s signal) at(key string) (time.Time, bool) {
	raw := s.str(key)
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	return t, err == nil
}
