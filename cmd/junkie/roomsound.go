package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// A room's own end-of-block chime.
//
// The setting people already had is per-browser and stays that way: this
// changes *which* sound a room offers, never whether anyone's device makes a
// noise. A room's sound appears in the picker as one more entry, so nobody
// who opened a shared link is ambushed by audio someone else chose.
const (
	// maxRoomSoundBytes is the real enforcement boundary. Fifteen seconds is
	// about 240 KB as a 128 kbps MP3 and about 120 KB as Opus, so this is
	// generous for anything compressed while still refusing a raw WAV dump --
	// uncompressed audio is the *largest* format, not the smallest, and 15
	// seconds of CD-quality WAV is around 2.6 MB.
	maxRoomSoundBytes = 512 << 10
	// maxRoomSoundSeconds is checked in the browser, where the duration is
	// cheaply knowable. It is not re-checked here: decoding attacker-supplied
	// audio server-side would mean shipping a decoder, and audio decoders fed
	// hostile bytes are a well-known source of memory-safety bugs. The size
	// cap bounds the damage instead, and only a room's admins can upload.
	maxRoomSoundSeconds = 15
	// soundChangeCooldown keeps a room's sound from being swapped repeatedly:
	// every change invalidates a cached file for everyone in the room.
	soundChangeCooldown  = 30 * time.Second
	maxSoundChangesPerHr = 10
	maxSoundNameLen      = 64
)

// allowedSoundTypes are what a sniffed upload may be. MP3 and Ogg are the
// formats worth encouraging; WAV is accepted because a short one is perfectly
// reasonable, and the size cap is what stops a long one.
var allowedSoundTypes = map[string]string{
	"audio/mpeg":      "mp3",
	"application/ogg": "ogg",
	"audio/wave":      "wav",
	"audio/wav":       "wav",
	"audio/x-wav":     "wav",
}

// sanitizeSoundName makes an uploaded filename safe to store and to show.
//
// This string reaches Discord, where markdown would be interpreted, and the
// terminal client, where escape sequences would be executed -- so it is
// cleaned on the way in, once, rather than at each place that renders it.
func sanitizeSoundName(name string) string {
	// Windows and POSIX separators both, so a path can't survive as a name.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsControl(r), r == '`', r == '*', r == '_', r == '~', r == '|', r == '#':
			// Control characters carry terminal escapes; the rest are
			// Discord's markdown.
			continue
		case unicode.IsLetter(r), unicode.IsDigit(r), strings.ContainsRune(" .-()[]+", r):
			b.WriteRune(r)
		default:
			continue
		}
	}
	out := strings.TrimSpace(b.String())
	out = limitRunes(out, maxSoundNameLen)
	if out == "" {
		return "sound"
	}
	return out
}

// roomSoundID is the id a room's own sound carries in the picker. It cannot
// collide with a catalogue id, which are plain words in sounds.json.
const roomSoundID = "room:sound"

// uploadRoomSound replaces a room's sound. Admins only -- this is audio that
// plays in other people's rooms, and there is no automated check that catches
// an unpleasant one, so it stays with someone accountable for the room.
func (a *app) uploadRoomSound(w http.ResponseWriter, r *http.Request) {
	u, _ := a.currentUser(r)
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	fail := func(msg string) {
		http.Redirect(w, r, "/r/"+rm.Code+"?error="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if !a.canAdminRoom(r.Context(), rm, u.ID) {
		fail("Only this room's admins can change its sound.")
		return
	}
	// Two ceilings: a short cooldown so it can't be swapped on a loop, and an
	// hourly count. Every change invalidates the file for everyone in the room.
	if !a.limiter.allow("soundcooldown:"+rm.ID, 1, soundChangeCooldown) {
		fail("That was just changed — give it a moment before changing it again.")
		return
	}
	if !a.limiter.allow("sound:"+rm.ID, maxSoundChangesPerHr, time.Hour) {
		fail("This room's sound has been changed too many times. Try again later.")
		return
	}

	if r.FormValue("remove") == "1" {
		if err := a.clearRoomSound(r.Context(), rm); err != nil {
			fail("Could not remove the sound.")
			return
		}
		a.logRoomEvent(r.Context(), u.ID, eventRoomSound, rm, "", map[string]string{"change": "removed"})
		a.hub.broadcast(rm.Code, "settings")
		http.Redirect(w, r, "/r/"+rm.Code+"?notice="+url.QueryEscape("Room sound removed."), http.StatusSeeOther)
		return
	}

	if err := r.ParseMultipartForm(maxRoomSoundBytes + 4096); err != nil {
		fail("That file is too large — the limit is 512 KB.")
		return
	}
	file, header, err := r.FormFile("sound")
	if err != nil {
		fail("Choose a sound file to upload.")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRoomSoundBytes+1))
	if err != nil {
		fail("Could not read that file.")
		return
	}
	name := ""
	if header != nil {
		name = header.Filename
	}
	if err := a.setRoomSound(r.Context(), rm, data, name); err != nil {
		fail(err.Error())
		return
	}
	a.logRoomEvent(r.Context(), u.ID, eventRoomSound, rm, "", map[string]string{"file": sanitizeSoundName(name)})
	a.hub.broadcast(rm.Code, "settings")
	http.Redirect(w, r, "/r/"+rm.Code+"?notice="+url.QueryEscape("Room sound updated."), http.StatusSeeOther)
}

// validateRoomSound checks bytes the way the server must: by size, and by
// what they actually are rather than what they were labelled.
func validateRoomSound(data []byte) (contentType string, err error) {
	if len(data) == 0 {
		return "", errors.New("That file is empty.")
	}
	if len(data) > maxRoomSoundBytes {
		return "", fmt.Errorf("That file is %d KB — the limit is %d KB. An MP3 or OGG of the same clip is usually far smaller than a WAV.",
			len(data)/1024, maxRoomSoundBytes/1024)
	}
	sniffed := http.DetectContentType(data)
	// DetectContentType returns parameters on some types; compare the base.
	if i := strings.IndexByte(sniffed, ';'); i >= 0 {
		sniffed = sniffed[:i]
	}
	if _, ok := allowedSoundTypes[strings.TrimSpace(sniffed)]; !ok {
		return "", errors.New("That doesn't look like an audio file. Use an MP3, OGG or WAV.")
	}
	return strings.TrimSpace(sniffed), nil
}

// setRoomSound stores validated bytes as the room's sound.
func (a *app) setRoomSound(ctx context.Context, rm room, data []byte, name string) error {
	contentType, err := validateRoomSound(data)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(ctx, `
		UPDATE rooms SET sound = $1, sound_name = $2, sound_type = $3, sound_updated_at = now()
		WHERE id = $4`, data, sanitizeSoundName(name), contentType, rm.ID)
	if err != nil {
		return errors.New("Could not save that sound.")
	}
	return nil
}

func (a *app) clearRoomSound(ctx context.Context, rm room) error {
	_, err := a.db.Exec(ctx, `
		UPDATE rooms SET sound = NULL, sound_name = NULL, sound_type = NULL, sound_updated_at = NULL
		WHERE id = $1`, rm.ID)
	return err
}

// serveRoomSound returns a room's sound bytes.
//
// Members only for a normal room. Temporary rooms serve it to anyone with the
// code, because an OBS browser source has no cookies -- the same constraint
// that shaped the public focus embed, and temporary rooms are already
// public-read behind a 48-bit code.
func (a *app) serveRoomSound(w http.ResponseWriter, r *http.Request) {
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !rm.Ephemeral {
		u, signedIn := a.currentUser(r)
		if !signedIn || !a.isRoomMember(r.Context(), rm.ID, u.ID) {
			http.NotFound(w, r)
			return
		}
	}
	var data []byte
	var contentType *string
	if err := a.db.QueryRow(r.Context(),
		`SELECT sound, sound_type FROM rooms WHERE id = $1 AND sound IS NOT NULL`, rm.ID).Scan(&data, &contentType); err != nil {
		http.NotFound(w, r)
		return
	}
	ct := "application/octet-stream"
	if contentType != nil && *contentType != "" {
		ct = *contentType
	}
	serveStatic(w, r, ct, data)
}

// roomSoundsManifest serves the catalogue a room's page should offer: the
// built-in sounds, plus this room's own if it has one.
//
// The client already fetches its catalogue from a URL rather than
// enumerating it, so a room's sound arrives as one more entry and the picker
// needs no special case for it.
func (a *app) roomSoundsManifest(w http.ResponseWriter, r *http.Request) {
	catalogue, err := staticAssets.ReadFile("static/sounds/sounds.json")
	if err != nil {
		catalogue = []byte("[]")
	}
	var sounds []map[string]any
	if err := json.Unmarshal(catalogue, &sounds); err != nil {
		sounds = []map[string]any{}
	}
	rm, ok := a.findRoom(r.Context(), r.PathValue("code"))
	if ok {
		if !rm.Ephemeral {
			u, signedIn := a.currentUser(r)
			if !signedIn || !a.isRoomMember(r.Context(), rm.ID, u.ID) {
				ok = false
			}
		}
	}
	if ok {
		var name *string
		var updated *time.Time
		if err := a.db.QueryRow(r.Context(),
			`SELECT sound_name, sound_updated_at FROM rooms WHERE id = $1 AND sound IS NOT NULL`, rm.ID).
			Scan(&name, &updated); err == nil {
			label := "sound"
			if name != nil && *name != "" {
				label = *name
			}
			version := int64(0)
			if updated != nil {
				version = updated.Unix()
			}
			// Prepended, so a room that has chosen a sound offers it first.
			sounds = append([]map[string]any{{
				"id":   roomSoundID,
				"name": "This room: " + label,
				"file": "",
				// An absolute URL, because this entry isn't in /assets/sounds.
				"url":      fmt.Sprintf("/r/%s/sound?v=%d", rm.Code, version),
				"fileName": label,
			}}, sounds...)
		}
	}
	writeJSON(w, sounds)
}
