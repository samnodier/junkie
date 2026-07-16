import { computed, onUnmounted, ref } from 'vue';

// The API reports whole `secondsLeft` at fetch time. Deriving a deadline from
// it on every refresh shifts the anchor by the server's truncation plus
// network latency, so the ring skips seconds whenever a signal or poll lands
// (the "23 → 21" flicker). Anchor once per run/phase instead: the deadline is
// fixed the first time a phase is seen and only re-derived when the phase
// actually changes (or the fresh value disagrees by seconds, which means a
// different run reused the same key). Staying relative to secondsLeft also
// keeps the countdown immune to client/server clock skew, unlike trusting the
// server's absolute endsAt.
export function useTimerDeadline(timer) {
  const now = ref(Date.now());
  const tick = setInterval(() => {
    now.value = Date.now();
  }, 500);
  onUnmounted(() => clearInterval(tick));

  let anchor = { key: '', ms: 0 };
  const anchorMs = () => {
    const t = timer.value;
    if (!t) {
      anchor = { key: '', ms: 0 };
      return 0;
    }
    const key = `${t.runId || 'solo'}:${t.phase}:${t.paused ? 1 : 0}:${t.breakPending ? 1 : 0}`;
    // secondsLeft is truncated server-side; +1s restores the ceiling so a
    // fresh 25-minute block reads 25:00, and expiry lands at or after the
    // server's own deadline.
    const fresh = Date.now() + ((t.secondsLeft || 0) + 1) * 1000;
    if (key !== anchor.key || Math.abs(fresh - anchor.ms) > 2500) {
      anchor = { key, ms: fresh };
    }
    return anchor.ms;
  };

  const endsAt = computed(() => (timer.value ? new Date(anchorMs()).toISOString() : ''));
  // Live seconds for text countdowns; mirrors the server snapshot while the
  // timer is paused (nothing is running, so nothing should tick).
  const seconds = computed(() => {
    const t = timer.value;
    if (!t) return 0;
    if (t.paused || t.breakPending) return t.secondsLeft || 0;
    void now.value;
    return Math.max(0, Math.floor((anchorMs() - Date.now()) / 1000));
  });

  return { endsAt, seconds };
}

// The server owns phase transitions; our anchored deadline can run a hair
// ahead of or behind it. After the ring expires, nudge refreshes until the
// observed phase actually changes so the UI never sits on 00:00 waiting for
// the 45s poll.
export function expireNudge(refresh, phaseKey) {
  const before = phaseKey();
  let tries = 0;
  const nudge = async () => {
    await refresh();
    if (phaseKey() === before && ++tries < 5) setTimeout(nudge, 800);
  };
  setTimeout(nudge, 400);
}
