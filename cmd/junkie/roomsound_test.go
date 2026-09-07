package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// wavBytes builds a real, playable WAV of the requested payload size so the
// sniffer sees genuine audio rather than a magic-number stub.
func wavBytes(dataBytes int) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataBytes))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1))     // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1))     // mono
	binary.Write(&b, binary.LittleEndian, uint32(24000)) // sample rate
	binary.Write(&b, binary.LittleEndian, uint32(48000)) // byte rate
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(dataBytes))
	b.Write(bytes.Repeat([]byte{0x01, 0x00}, dataBytes/2))
	return b.Bytes()
}

// postSound uploads through the real multipart handler.
func postSound(t *testing.T, a *app, code, filename string, data []byte) (int, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("sound", filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/r/"+code+"/sound", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.SetPathValue("code", code)
	rec := httptest.NewRecorder()
	a.uploadRoomSound(rec, req)
	dest := rec.Header().Get("Location")
	errMsg := ""
	if i := strings.Index(dest, "error="); i >= 0 {
		errMsg = dest[i+6:]
	}
	return rec.Code, errMsg
}

func TestAdminCanUploadAndReplaceARoomSound(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	rm := makeRoom(t, a, owner)
	a.asUser(owner)

	if _, errMsg := postSound(t, a, rm.Code, "chime.wav", wavBytes(2000)); errMsg != "" {
		t.Fatalf("upload: %s", errMsg)
	}
	var name, ctype string
	if err := a.db.QueryRow(context.Background(),
		`SELECT sound_name, sound_type FROM rooms WHERE id = $1 AND sound IS NOT NULL`, rm.ID).
		Scan(&name, &ctype); err != nil {
		t.Fatalf("sound not stored: %v", err)
	}
	if name != "chime.wav" {
		t.Errorf("stored name = %q, want %q", name, "chime.wav")
	}
	if !strings.HasPrefix(ctype, "audio/") {
		t.Errorf("stored type = %q, want an audio type", ctype)
	}
}

func TestOnlyAdminsCanChangeTheRoomSound(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	member := makeUser(t, a, "Member")
	rm := makeRoom(t, a, owner)
	a.addRoomMember(context.Background(), rm.ID, member.ID)

	a.asUser(member)
	if _, errMsg := postSound(t, a, rm.Code, "x.wav", wavBytes(2000)); errMsg == "" {
		t.Fatal("a plain member should not be able to set the room's sound")
	}
	var has bool
	_ = a.db.QueryRow(context.Background(), `SELECT sound IS NOT NULL FROM rooms WHERE id = $1`, rm.ID).Scan(&has)
	if has {
		t.Error("a refused upload still stored a sound")
	}
}

func TestUploadRejectsOversizeAndNonAudio(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	rm := makeRoom(t, a, owner)
	a.asUser(owner)

	// Over the size cap. The message names MP3/OGG, because a WAV this big is
	// the most likely way to hit it.
	_, errMsg := postSound(t, a, rm.Code, "big.wav", wavBytes(maxRoomSoundBytes+1000))
	if errMsg == "" {
		t.Error("an oversize file should be refused")
	}

	// Not audio at all, whatever it claims by extension.
	a.limiter = newRateLimiter() // past the per-room cooldown
	if _, errMsg := postSound(t, a, rm.Code, "evil.mp3", []byte("<html>not audio</html>")); errMsg == "" {
		t.Error("a non-audio file should be refused")
	}
}

func TestValidateRoomSound(t *testing.T) {
	if _, err := validateRoomSound(nil); err == nil {
		t.Error("empty data should be refused")
	}
	if _, err := validateRoomSound(wavBytes(maxRoomSoundBytes + 100)); err == nil {
		t.Error("oversize data should be refused")
	}
	if _, err := validateRoomSound([]byte("GIF89a stuff")); err == nil {
		t.Error("a non-audio type should be refused")
	}
	ct, err := validateRoomSound(wavBytes(1000))
	if err != nil {
		t.Fatalf("valid wav refused: %v", err)
	}
	if _, ok := allowedSoundTypes[ct]; !ok {
		t.Errorf("sniffed %q, which is not in the allowlist", ct)
	}
}

// The filename reaches Discord (markdown) and the terminal client (escape
// sequences), so it is cleaned once, on the way in.
func TestSanitizeSoundName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"chime.wav", "chime.wav"},
		// Only the basename survives, so a path can't be smuggled through.
		{"../../etc/passwd", "passwd"},
		{`C:\windows\evil.mp3`, "evil.mp3"},
		// The escape byte is what makes a terminal act; the bracket and digits
		// left behind are inert text, so they are allowed to stay.
		{"be\x1b[31mll.mp3", "be[31mll.mp3"},
		{"**bold**.mp3", "bold.mp3"},
		{"`whoami`.ogg", "whoami.ogg"},
		{"", "sound"},
		{"\x00\x01\x02", "sound"},
		{strings.Repeat("a", 200) + ".mp3", strings.Repeat("a", maxSoundNameLen)},
	} {
		if got := sanitizeSoundName(tc.in); got != tc.want {
			t.Errorf("sanitizeSoundName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Whatever comes out must be free of everything we strip for.
	for _, nasty := range []string{"\x1b[2J", "`id`", "**x**", "a/b", `a\b`} {
		got := sanitizeSoundName(nasty + ".mp3")
		for _, bad := range []string{"\x1b", "`", "*", "/", `\`} {
			if strings.Contains(got, bad) {
				t.Errorf("sanitizeSoundName(%q) left %q in %q", nasty, bad, got)
			}
		}
	}
}

// A normal room's sound is for its members; a temporary room's has to reach
// an OBS browser source, which carries no cookies.
func TestSoundVisibilityFollowsTheRoomKind(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	outsider := makeUser(t, a, "Outsider")
	ctx := context.Background()
	rm := makeRoom(t, a, owner)
	a.asUser(owner)
	if _, errMsg := postSound(t, a, rm.Code, "chime.wav", wavBytes(2000)); errMsg != "" {
		t.Fatal(errMsg)
	}

	fetch := func(as user) int {
		req := httptest.NewRequest(http.MethodGet, "/r/"+rm.Code+"/sound", nil)
		req.SetPathValue("code", rm.Code)
		rec := httptest.NewRecorder()
		a.asUser(as).serveRoomSound(rec, req)
		return rec.Code
	}

	if got := fetch(owner); got != http.StatusOK {
		t.Errorf("a member got %d, want 200", got)
	}
	if got := fetch(outsider); got != http.StatusNotFound {
		t.Errorf("an outsider got %d, want 404", got)
	}
	if got := fetch(user{}); got != http.StatusNotFound {
		t.Errorf("a signed-out visitor got %d, want 404", got)
	}

	// The same room, made temporary, serves anyone holding the code.
	if _, err := a.db.Exec(ctx, `UPDATE rooms SET ephemeral = true WHERE id = $1`, rm.ID); err != nil {
		t.Fatal(err)
	}
	if got := fetch(user{}); got != http.StatusOK {
		t.Errorf("a temporary room's sound returned %d to an OBS source, want 200", got)
	}
}

// The room's sound arrives through the catalogue as one more entry, so the
// picker needs no special case -- and carries the filename the page shows.
func TestRoomManifestCarriesTheRoomSound(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	rm := makeRoom(t, a, owner)
	a.asUser(owner)
	postSound(t, a, rm.Code, "airhorn.mp3", wavBytes(2000))

	req := httptest.NewRequest(http.MethodGet, "/r/"+rm.Code+"/sounds.json", nil)
	req.SetPathValue("code", rm.Code)
	rec := httptest.NewRecorder()
	a.roomSoundsManifest(rec, req)

	var sounds []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &sounds); err != nil {
		t.Fatal(err)
	}
	if len(sounds) < 2 {
		t.Fatalf("manifest has %d entries, want the room's plus the built-ins", len(sounds))
	}
	first := sounds[0]
	if first["id"] != roomSoundID {
		t.Errorf("the room's own sound should come first, got %v", first["id"])
	}
	if first["fileName"] != "airhorn.mp3" {
		t.Errorf("fileName = %v, want the stored filename", first["fileName"])
	}
	if url, _ := first["url"].(string); !strings.HasPrefix(url, "/r/"+rm.Code+"/sound?v=") {
		t.Errorf("url = %q, want a cache-busted room sound URL", url)
	}
}

// An outsider's manifest is the built-in catalogue and nothing more.
func TestManifestHidesTheRoomSoundFromOutsiders(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	outsider := makeUser(t, a, "Outsider")
	rm := makeRoom(t, a, owner)
	a.asUser(owner)
	postSound(t, a, rm.Code, "secret.mp3", wavBytes(2000))

	req := httptest.NewRequest(http.MethodGet, "/r/"+rm.Code+"/sounds.json", nil)
	req.SetPathValue("code", rm.Code)
	rec := httptest.NewRecorder()
	a.asUser(outsider).roomSoundsManifest(rec, req)
	if strings.Contains(rec.Body.String(), "secret.mp3") {
		t.Errorf("an outsider was shown the room's sound: %s", rec.Body.String())
	}
}

func TestSoundChangesAreRateLimited(t *testing.T) {
	a := newTestApp(t)
	owner := makeUser(t, a, "Owner")
	rm := makeRoom(t, a, owner)
	a.asUser(owner)

	if _, errMsg := postSound(t, a, rm.Code, "one.wav", wavBytes(2000)); errMsg != "" {
		t.Fatalf("first upload: %s", errMsg)
	}
	// Straight back: the cooldown, not the hourly count, is what stops this.
	if _, errMsg := postSound(t, a, rm.Code, "two.wav", wavBytes(2000)); errMsg == "" {
		t.Error("a second immediate change should be refused by the cooldown")
	}
}

// Only the sound upload may exceed the global request-body cap.
func TestOnlyTheSoundUploadIsExemptFromTheBodyCap(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, "/r/ABC123/sound", true},
		{http.MethodGet, "/r/ABC123/sound", false},
		{http.MethodPost, "/r/ABC123/todos", false},
		{http.MethodPost, "/r/ABC123/sound/extra", false},
		{http.MethodPost, "/r//sound", false},
		{http.MethodPost, "/profile/avatar", false},
		{http.MethodPost, "/r/ABC123", false},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if got := isRoomSoundUpload(req); got != tc.want {
			t.Errorf("isRoomSoundUpload(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
