// The end-of-phase chime.
//
// Off by default, and a per-browser choice: it lives in localStorage, never in
// the database and never on the room, so turning it on doesn't make noise on
// anybody else's machine — a viewer who joined a shared link keeps their own
// audio to themselves.
//
// The catalogue comes from the server (/assets/sounds/sounds.json), so adding
// a sound is a file plus a line of JSON; nothing here enumerates them.
//
// A room can also carry its own uploaded sound. That arrives through the same
// manifest, as one more entry, which is why nothing below needs a special
// case for it -- and why the room decides *which* sounds are on offer while
// this file still decides, per browser, whether any noise is made at all.
const ENABLED_KEY = 'junkie:phaseSound';
const CHOICE_KEY = 'junkie:phaseSoundId';
const MANIFEST_URL = '/assets/sounds/sounds.json';

// Which sound a fresh browser gets, and the fallback when a stored id names a
// sound that has since been removed from the manifest.
const DEFAULT_ID = 'chime';

export function soundEnabled() {
  try {
    return localStorage.getItem(ENABLED_KEY) === 'on';
  } catch {
    return false; // private-mode storage denial: silence is the safe default
  }
}

export function soundChoice() {
  try {
    return localStorage.getItem(CHOICE_KEY) || DEFAULT_ID;
  } catch {
    return DEFAULT_ID;
  }
}

export function setSoundEnabled(on) {
  try {
    localStorage.setItem(ENABLED_KEY, on ? 'on' : 'off');
  } catch {
    /* nothing to persist to; the session-local setting still applies */
  }
  if (on) primeSound();
}

export function setSoundChoice(id) {
  try {
    localStorage.setItem(CHOICE_KEY, String(id || DEFAULT_ID));
  } catch {
    /* as above */
  }
  primeSound();
}

// The manifest is fetched once per page, per source. A failed fetch clears
// that entry so a later call retries rather than being stuck with an empty
// catalogue.
//
// Keyed by URL because a room page asks for its own manifest (the built-in
// sounds plus that room's) while the profile page asks for the plain one.
const manifests = new Map();

// setSoundSource points later loads at a room's catalogue. Passing no code
// goes back to the built-in list.
let manifestURL = MANIFEST_URL;
export function setSoundSource(roomCode) {
  const next = roomCode ? `/r/${encodeURIComponent(roomCode)}/sounds.json` : MANIFEST_URL;
  if (next === manifestURL) return;
  manifestURL = next;
  // The chosen sound may not exist in the new catalogue; element() already
  // falls back, but drop the prepared audio so it isn't the wrong one.
  audio = null;
  loadedFile = '';
}

export function loadSounds() {
  const url = manifestURL;
  if (!manifests.has(url)) {
    manifests.set(
      url,
      fetch(url, { credentials: 'same-origin' })
        .then((res) => (res.ok ? res.json() : []))
        .then((list) => (Array.isArray(list) ? list : []))
        .catch(() => {
          manifests.delete(url);
          return [];
        }),
    );
  }
  return manifests.get(url);
}

// soundURL resolves a catalogue entry to something playable. A room's own
// sound carries an absolute url; the built-ins carry a filename under
// /assets/sounds.
export function soundURL(pick) {
  if (!pick) return '';
  if (pick.url) return pick.url;
  return pick.file ? `/assets/sounds/${encodeURIComponent(pick.file)}` : '';
}

// One <audio> element, rebuilt only when the chosen sound changes. Holding it
// across plays is what makes the sound land at the transition instead of a
// fetch-and-decode later: preload='auto' warms it while the block runs.
let audio = null;
let loadedFile = '';

async function element() {
  const list = await loadSounds();
  const pick = list.find((s) => s?.id === soundChoice()) || list[0];
  const src = soundURL(pick);
  if (!src || typeof Audio === 'undefined') return null;
  if (!audio || loadedFile !== src) {
    audio = new Audio(src);
    audio.preload = 'auto';
    loadedFile = src;
  }
  return audio;
}

// Fetch and decode ahead of the transition. Cheap to call repeatedly.
export function primeSound() {
  if (!soundEnabled()) return;
  element().catch(() => {});
}

async function play() {
  const el = await element();
  if (!el) return;
  try {
    el.currentTime = 0;
    await el.play();
  } catch {
    // Browsers refuse audio until the page has been interacted with, and OBS
    // browser sources never are. Nothing to recover from: the visible timer
    // is the real signal, the chime is a bonus.
  }
}

// Called for every focus->break and break->focus turn, via onTimerEnd.
export function playPhaseSound() {
  if (!soundEnabled()) return;
  play();
}

// The settings "Test" button. Plays whether or not the setting is on, so you
// can audition a sound before committing to it — and the click doubles as the
// interaction that unlocks audio for the rest of the page's life.
export function previewSound() {
  play();
}
