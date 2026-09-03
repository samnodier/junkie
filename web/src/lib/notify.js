// Device notifications.
//
// Everything goes through the service worker registration rather than the
// `new Notification()` constructor. On Android that constructor is illegal and
// throws — the old page-side implementation swallowed the error in a catch, so
// notifications never appeared there at all. showNotification() is also the
// only form that survives the tab being backgrounded, which is what lets the
// ongoing timer notification stay put in the shade.
//
// Desktop browsers with no active worker (private windows, a first load before
// install finishes) fall back to the constructor, which works fine there.
import { playPhaseSound } from '@/lib/sound';

const ROOM_INVITE_KEY = 'junkie:roomInviteNotifications';
const ICON = '/assets/icon.svg';
const BADGE = '/assets/icon-192.png';

// One tag for the ongoing timer, so re-showing replaces the notification in
// place instead of stacking a new one on every poll.
const TIMER_TAG = 'junkie-timer';

const supported = () => typeof window !== 'undefined' && 'Notification' in window;
const granted = () => supported() && Notification.permission === 'granted';

export function requestPermission() {
  if (!supported() || Notification.permission !== 'default') return;
  try {
    // The prompt is answered long after the timer that triggered it started,
    // so replay the sync that was dropped for lack of permission instead of
    // leaving the shade empty until the next 45s poll.
    Notification.requestPermission()
      .then((result) => {
        if (result === 'granted') flushPendingSync();
      })
      .catch(() => {});
  } catch {
    /* older Safari throws on the promise form */
  }
}

// navigator.serviceWorker.ready never settles when nothing is registered, so
// race it against a timeout rather than awaiting it — otherwise a notification
// from an uncontrolled page would hang forever instead of falling back.
function registration() {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) {
    return Promise.resolve(null);
  }
  return Promise.race([
    navigator.serviceWorker.ready,
    new Promise((resolve) => setTimeout(() => resolve(null), 1500)),
  ]).catch(() => null);
}

async function show(title, options) {
  if (!granted()) return;
  const reg = await registration();
  if (reg) {
    try {
      await reg.showNotification(title, { icon: ICON, badge: BADGE, ...options });
      return;
    } catch {
      /* fall through to the page constructor */
    }
  }
  try {
    // The constructor ignores tag replacement on some browsers and throws
    // outright on Android — the worker path above covers both.
    const n = new Notification(title, { icon: ICON, ...options });
    n.onclick = () => {
      window.focus();
      const url = options?.data?.url;
      if (url && window.location.pathname !== url) window.location.href = url;
      n.close();
    };
  } catch {
    /* nothing more we can do without a worker */
  }
}

export function notify(title, body, url) {
  show(title, {
    body: body || '',
    requireInteraction: true,
    data: { url: url || '/' },
  });
}

export function onTimerEnd(phase) {
  // Every phase turn in the app funnels through here -- rooms, the focus
  // room, solo, and the guest desk -- so the optional chime hangs off it
  // rather than being wired into each of them. It's a no-op unless the
  // viewer switched it on. See lib/sound.js.
  playPhaseSound();
  if (phase === 'break') {
    notify("Break's over", 'Ready for your next focus block');
  } else {
    notify('Focus session complete', 'Time for a break');
  }
}

export function roomInvitesEnabled() {
  return localStorage.getItem(ROOM_INVITE_KEY) !== 'disabled';
}

export function onRoomInvite(starterName, roomName, url) {
  if (!roomInvitesEnabled() || (document.visibilityState === 'visible' && document.hasFocus())) return;
  notify(
    starterName + ' is starting a focus block',
    'Join ' + roomName + ' before the lobby closes.',
    url
  );
}

export function onBreakInvite(roomName, url) {
  if (!roomInvitesEnabled() || (document.visibilityState === 'visible' && document.hasFocus())) return;
  notify(
    'Break time in ' + roomName,
    'Join now to be included in the next focus block.',
    url
  );
}

// --- ongoing timer notification -------------------------------------------
//
// The web has no way to tick a countdown inside a notification the way a
// native foreground service can: Notification Triggers was abandoned, and
// nothing exposes Android's chronometer. So rather than counting down, post
// the absolute time the phase ends and rewrite it only when the phase actually
// changes. That's the part you need while the screen is off, and it costs one
// notification per transition instead of one per second.

function clockTime(iso) {
  const ms = Date.parse(iso || '');
  if (!Number.isFinite(ms)) return '';
  return new Date(ms).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}

// describe maps both timer shapes — the server's solo/room payload and the
// guest's localStorage timer — onto a title and body, or null when nothing
// should be showing.
function describe(timer, label) {
  if (!timer) return null;
  const phase = timer.phase || 'idle';
  const where = label ? ` · ${label}` : '';

  if (phase === 'break_offer' || timer.breakPending) {
    return { title: `Break ready${where}`, body: "Start it when you're ready" };
  }
  if (phase === 'focus' || phase === 'break') {
    const heading = phase === 'focus' ? 'Focusing' : 'On a break';
    if (timer.paused) return { title: `${heading}${where}`, body: 'Paused' };
    const until = clockTime(timer.endsAt);
    return { title: `${heading}${where}`, body: until ? `Until ${until}` : 'In progress' };
  }
  return null;
}

// Only one timer notification exists at a time, and only the caller that put
// it there may take it down — otherwise the desk store finding no solo timer
// would clear the room store's notification out from under it.
let owner = '';
let lastKey = '';
let pendingSync = null; // last sync dropped because permission wasn't granted yet

function flushPendingSync() {
  const args = pendingSync;
  pendingSync = null;
  if (args) syncTimerNotification(...args);
}

export async function clearTimerNotification() {
  const reg = await registration();
  if (!reg?.getNotifications) return;
  try {
    for (const n of await reg.getNotifications({ tag: TIMER_TAG })) n.close();
  } catch {
    /* unsupported on some browsers; the tag still replaces on next show */
  }
}

export function syncTimerNotification(source, timer, opts = {}) {
  const { label = '', url = '/' } = opts;
  const state = describe(timer, label);
  if (!state) {
    pendingSync = null;
    if (owner === source) {
      owner = '';
      lastKey = '';
      clearTimerNotification();
    }
    return;
  }
  if (!granted()) {
    pendingSync = [source, timer, opts];
    return;
  }

  // Refreshes land every few seconds; re-posting identical content would
  // redraw the notification each time (and flicker it on Android).
  const key = `${source}:${state.title}:${state.body}`;
  if (owner === source && key === lastKey) return;
  owner = source;
  lastKey = key;

  show(state.title, {
    body: state.body,
    tag: TIMER_TAG,
    renotify: false,
    silent: true,
    requireInteraction: true, // sticky on desktop; ignored on Android/Safari
    data: { url },
  });
}
