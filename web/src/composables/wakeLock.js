import { onUnmounted } from 'vue';

// Keep the screen awake on mobile while a timer is on screen, re-acquiring
// when the tab becomes visible again (the browser drops the lock on hide).
// Ported from the legacy wireWakeLock; call from views that show a countdown.
export function useWakeLock() {
  let lock = null;
  let requesting = false;
  let wanted = false;

  async function acquire() {
    if (!('wakeLock' in navigator) || lock || requesting) return;
    requesting = true;
    try {
      lock = await navigator.wakeLock.request('screen');
      lock.addEventListener('release', () => {
        lock = null;
      });
    } catch {
      lock = null;
    } finally {
      requesting = false;
    }
  }

  function release() {
    wanted = false;
    lock?.release().catch(() => {});
    lock = null;
  }

  function onVisibility() {
    if (wanted && document.visibilityState === 'visible') acquire();
  }
  document.addEventListener('visibilitychange', onVisibility);

  onUnmounted(() => {
    document.removeEventListener('visibilitychange', onVisibility);
    release();
  });

  return {
    want() {
      wanted = true;
      acquire();
    },
    release,
  };
}
