import { getCurrentInstance, onUnmounted, ref } from 'vue';

// One clock for every countdown on the page.
//
// The small reason this is shared: a ring per interval meant N intervals
// drifting against each other, so two views of the same deadline could sit on
// different seconds. The load-bearing reason: a hidden tab throttles
// setInterval to roughly once a minute, and that is precisely the state the
// owning tab is in while a popped-out timer is being used — the whole point is
// to work in another app and still see the countdown. A picture-in-picture
// window is its own *visible* window with its own unthrottled timers, so when
// one opens we re-home the tick onto it (setClockHost) and the ring keeps
// ticking per second no matter what the opener tab is doing.
//
// Values stay derived from Date.now(), never accumulated, so even a fully
// throttled clock is late rather than wrong: one slow tick and the display
// jumps straight to the true remaining time.

const TICK_MS = 250;

const now = ref(Date.now());

let host = null;
let handle = null;
let users = 0;

function stop() {
  if (handle !== null && host) host.clearInterval(handle);
  handle = null;
}

function start() {
  stop();
  if (!users) return;
  host = host || (typeof window === 'undefined' ? null : window);
  if (!host) return;
  now.value = Date.now();
  handle = host.setInterval(() => {
    now.value = Date.now();
  }, TICK_MS);
}

// The reactive current time, shared by every countdown. Reading it in a
// component subscribes to the tick and releases it on unmount.
export function useClock() {
  users += 1;
  if (handle === null) start();
  if (getCurrentInstance()) {
    onUnmounted(() => {
      users -= 1;
      if (users <= 0) stop();
    });
  }
  return now;
}

// Re-home the tick onto `win` (a picture-in-picture window) so it keeps
// running at full rate while the opener tab is hidden. Pass null to hand it
// back to the main window when the PiP window closes.
export function setClockHost(win) {
  const next = win || (typeof window === 'undefined' ? null : window);
  if (next === host) return;
  stop();
  host = next;
  start();
}

// A tick is cheap, but the display should never wait one for freshness — used
// when a window opens or the page becomes visible again after throttling.
export function syncClock() {
  now.value = Date.now();
}

// Tests only: drop all subscribers and intervals so each case starts clean.
export function __resetClock() {
  stop();
  users = 0;
  host = null;
  now.value = Date.now();
}
