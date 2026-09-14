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
//
// The one exception is a temporary room. You join one of those by following
// a link into someone's block, and the chime is part of the block: it plays
// for everyone there, the room's own sound if it has one and the default
// otherwise, and the per-browser switch does not apply. Anyone who would
// rather not hear it mutes the tab or leaves. That is Sam's call -- the
// setting exists so a shared link can't make noise on a machine that never
// asked for it, and a temporary room is the case where you did ask, by
// joining.
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

// Which entry plays. Ordinarily the browser's own choice. When the room is
// making the decision -- a temporary room -- its uploaded sound wins, then the
// default, and whatever this browser picked for its private timer is beside
// the point.
const ROOM_SOUND_ID = 'room:sound';
function pickSound(list, forced) {
  if (forced) {
    return list.find((s) => s?.id === ROOM_SOUND_ID) || list.find((s) => s?.id === DEFAULT_ID) || list[0];
  }
  return list.find((s) => s?.id === soundChoice()) || list[0];
}

async function element(forced = false) {
  const list = await loadSounds();
  const pick = pickSound(list, forced);
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
export function primeSound({ forced = false } = {}) {
  if (!forced && !soundEnabled()) return;
  element(forced).catch(() => {});
}

async function play(forced = false) {
  const el = await element(forced);
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
// forced is the temporary-room case: play whatever this browser's switch
// says. See the note at the top.
export function playPhaseSound({ forced = false } = {}) {
  if (!forced && !soundEnabled()) return;
  play(forced);
}

// Browsers only allow audio once the page has been touched, and they remember
// that touch for the rest of the page's life. A temporary room's chime plays
// without anyone reaching for a setting, so nothing else on the page is
// guaranteed to have been clicked first -- this takes the first tap or key,
// whatever it was for, and spends it on a silent play so the real chime is
// allowed later. Returns a function that stops listening.
export function unlockSoundOnFirstGesture() {
  if (typeof document === 'undefined') return () => {};
  const events = ['pointerdown', 'keydown', 'touchend'];
  const stop = () => events.forEach((e) => document.removeEventListener(e, unlock, true));
  const unlock = async () => {
    stop();
    try {
      const el = await element(true);
      if (!el) return;
      el.muted = true;
      await el.play();
      el.pause();
      el.currentTime = 0;
      el.muted = false;
    } catch {
      /* not allowed even now -- nothing more this page can do */
    }
  };
  events.forEach((e) => document.addEventListener(e, unlock, { capture: true, once: false }));
  return stop;
}

// The settings "Test" button. Plays whether or not the setting is on, so you
// can audition a sound before committing to it — and the click doubles as the
// interaction that unlocks audio for the rest of the page's life.
export function previewSound() {
  play();
}
