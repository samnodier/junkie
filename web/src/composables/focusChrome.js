import { onUnmounted, ref, watchEffect } from 'vue';

// The topbar during a focus block: gone rather than dimmed. A focus screen
// earns its keep by having nothing on it but the clock, and a 35%-opacity logo
// and menu are still something to look at.
//
// Getting it back differs by input. A pointer can hover the strip the bar
// occupies, which is plain CSS (:hover on .topbar) and needs nothing from
// here. Touch has no hover, so the ring doubles as a toggle — and there the
// hidden bar also stops taking taps, because an invisible menu sitting over
// the top of the screen would otherwise swallow a tap meant for the page.
//
// Scoped to the room focus views. The desk's solo timer sets focus-active too,
// but nothing there toggles the bar back, so it keeps the gentler 35% fade
// rather than being left with a menu no touch user can reach.
const IMMERSIVE = 'focus-immersive';
const SHOWN = 'chrome-shown';

export function useFocusChrome(active) {
  const shown = ref(false);

  watchEffect(() => {
    const on = !!active.value;
    // Leaving focus mode drops the reveal with it, so re-entering starts clean
    // rather than restoring a bar the user only ever meant to show once.
    if (!on && shown.value) shown.value = false;
    document.body.classList.toggle(IMMERSIVE, on);
    document.body.classList.toggle(SHOWN, on && shown.value);
  });

  onUnmounted(() => {
    document.body.classList.remove(IMMERSIVE, SHOWN);
  });

  return { shown, toggle: () => (shown.value = !shown.value) };
}
