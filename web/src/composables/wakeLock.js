import { onUnmounted } from 'vue';

// Keep the screen awake while a timer is on screen. The platform can take the
// lock back at any time (screen off, app switcher, Low Power Mode), so treat
// `want()` as a standing intent and keep re-arming until the view releases it.
// Call from views that show a countdown.

// Slow safety net for drops that fire no event we listen for.
const RECHECK_MS = 15000;

export function useWakeLock() {
  let lock = null;
  let requesting = false;
  let wanted = false;
  let watchdog = null;

  async function acquire() {
    if (!('wakeLock' in navigator) || !wanted || lock || requesting) return;
    // A hidden document can't hold a lock, and asking rejects; the visibility
    // handler below retries once we're back on screen.
    if (document.visibilityState !== 'visible') return;
    requesting = true;
    try {
      const sentinel = await navigator.wakeLock.request('screen');
      if (!wanted) {
        sentinel.release().catch(() => {});
        return;
      }
      lock = sentinel;
      sentinel.addEventListener('release', () => {
        // Only forget the sentinel we actually hold: a late release from a
        // previous one must not drop a live lock on the floor.
        if (lock !== sentinel) return;
        lock = null;
        // The platform took it back. If the timer is still running and we're
        // still on screen, take it straight back.
        acquire();
      });
    } catch {
      lock = null;
    } finally {
      requesting = false;
    }
  }

  function release() {
    wanted = false;
    clearInterval(watchdog);
    watchdog = null;
    const held = lock;
    lock = null;
    held?.release().catch(() => {});
  }

  function onVisibility() {
    if (document.visibilityState === 'visible') acquire();
  }
  document.addEventListener('visibilitychange', onVisibility);
  // A home-screen web app returning from the app switcher can restore from the
  // page cache without a visibilitychange.
  window.addEventListener('pageshow', onVisibility);
  window.addEventListener('focus', onVisibility);

  onUnmounted(() => {
    document.removeEventListener('visibilitychange', onVisibility);
    window.removeEventListener('pageshow', onVisibility);
    window.removeEventListener('focus', onVisibility);
    release();
  });

  return {
    want() {
      wanted = true;
      if (!watchdog) watchdog = setInterval(acquire, RECHECK_MS);
      acquire();
    },
    release,
  };
}
