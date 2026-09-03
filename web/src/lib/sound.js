// The end-of-phase chime.
//
// Off by default, and a per-browser choice: it lives in localStorage, never in
// the database and never on the room, so turning it on doesn't make noise on
// anybody else's machine — a viewer who joined a shared link keeps their own
// audio to themselves.
//
// The catalogue comes from the server (/assets/sounds/sounds.json), so adding
// a sound is a file plus a line of JSON; nothing here enumerates them.
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

// The manifest is fetched once per page. A failed fetch clears the cache so a
// later call retries rather than being stuck with an empty catalogue.
let manifest = null;
export function loadSounds() {
  if (!manifest) {
    manifest = fetch(MANIFEST_URL, { credentials: 'same-origin' })
      .then((res) => (res.ok ? res.json() : []))
      .then((list) => (Array.isArray(list) ? list : []))
      .catch(() => {
        manifest = null;
        return [];
      });
  }
  return manifest;
}

// One <audio> element, rebuilt only when the chosen sound changes. Holding it
// across plays is what makes the sound land at the transition instead of a
// fetch-and-decode later: preload='auto' warms it while the block runs.
let audio = null;
let loadedFile = '';

async function element() {
  const list = await loadSounds();
  const pick = list.find((s) => s?.id === soundChoice()) || list[0];
  if (!pick?.file || typeof Audio === 'undefined') return null;
  if (!audio || loadedFile !== pick.file) {
    audio = new Audio(`/assets/sounds/${encodeURIComponent(pick.file)}`);
    audio.preload = 'auto';
    loadedFile = pick.file;
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
