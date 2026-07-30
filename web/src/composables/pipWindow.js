import { readonly, ref, shallowRef } from 'vue';
import { setClockHost, syncClock } from './clock';
import { setPipVisible } from '@/lib/presence';

// The popped-out timer: a small always-on-top OS window holding the real timer
// card, so the countdown stays visible while the user works in another app.
//
// This is the Document Picture-in-Picture API, not the video one — it hosts a
// live DOM subtree rather than a <video>, which is what lets the actual card
// (ring, phase label, join/pause/skip buttons) move out intact. The document
// only ever hosts a mount point; the app teleports the card into it, so there
// is exactly one copy of the timer and one source of state. A phase change
// pushed over the room socket re-renders it in place, whether it came from the
// server's own phase wakeup or from someone pressing a button in Discord.
//
// Chromium 130+ and Firefox 151+ have it. Safari does not, so `supported` is
// false there and callers hide the button rather than offering a dead control.

const SIZE_KEY = 'junkie:pip:size';
// Big enough for the ring plus the phase label and every control the busiest
// state shows (a break: three buttons, two labels, the avatar stack). Shrink
// past that and app.css drops a tier on its own — first the controls, then the
// ring itself; the layout is container-queried off the window, so nothing here
// has to measure anything.
const DEFAULT_SIZE = { width: 380, height: 460 };

export const supported =
  typeof window !== 'undefined' &&
  'documentPictureInPicture' in window &&
  typeof window.documentPictureInPicture?.requestWindow === 'function';

const win = shallowRef(null);
// Teleport target. Null whenever the pop-out is closed, which is also the
// signal that sends the card back to the page.
const mount = shallowRef(null);
const open = ref(false);

export function usePipWindow() {
  return {
    supported,
    open: readonly(open),
    mount,
    openPip,
    closePip,
    toggle: () => (open.value ? closePip() : openPip()),
  };
}

// Copy the app's CSS across. A PiP document starts with no styles at all and
// inherits nothing from its opener, so without this the card lands unstyled.
function copyStyles(doc) {
  for (const sheet of Array.from(document.styleSheets)) {
    let rules = null;
    try {
      rules = sheet.cssRules;
    } catch {
      rules = null; // cross-origin (the Google Fonts sheet) — can't be read
    }
    if (rules) {
      const style = doc.createElement('style');
      style.textContent = Array.from(rules)
        .map((rule) => rule.cssText)
        .join('\n');
      if (sheet.media?.mediaText) style.media = sheet.media.mediaText;
      doc.head.appendChild(style);
    } else if (sheet.href) {
      // Re-link it instead; same CSP allowance as index.html's own <link>.
      const link = doc.createElement('link');
      link.rel = 'stylesheet';
      link.href = sheet.href;
      doc.head.appendChild(link);
    }
  }
}

function rememberSize(target) {
  try {
    localStorage.setItem(
      SIZE_KEY,
      JSON.stringify({ width: target.innerWidth, height: target.innerHeight })
    );
  } catch {
    /* private mode / quota — the default size is fine */
  }
}

function lastSize() {
  try {
    const saved = JSON.parse(localStorage.getItem(SIZE_KEY) || 'null');
    const width = Number(saved?.width);
    const height = Number(saved?.height);
    if (width > 0 && height > 0) return { width, height };
  } catch {
    /* fall through to the default */
  }
  return DEFAULT_SIZE;
}

export async function openPip() {
  if (!supported || win.value) return false;
  let pip;
  try {
    pip = await window.documentPictureInPicture.requestWindow(lastSize());
  } catch {
    // Denied (no user gesture) or unavailable — leave the card on the page.
    return false;
  }

  const doc = pip.document;
  doc.documentElement.setAttribute(
    'data-theme',
    document.documentElement.getAttribute('data-theme') || 'dark'
  );
  doc.title = document.title;
  copyStyles(doc);

  const root = doc.createElement('div');
  root.className = 'pip-root';
  doc.body.appendChild(root);
  doc.body.classList.add('pip-body');

  win.value = pip;
  mount.value = root;
  open.value = true;

  // The opener tab is hidden while the pop-out is in use, which throttles its
  // timers to about once a minute. This window is visible, so its timers run
  // at full rate — drive the countdown from here.
  setClockHost(pip);
  syncClock();
  setPipVisible(true);

  // Reopen at whatever size it was left at.
  pip.addEventListener('resize', () => rememberSize(pip));
  // Closing the window (its own close button, or the OS) has to put the card
  // back on the page — dropping the mount does that, since the Teleport falls
  // back to its in-page slot.
  pip.addEventListener('pagehide', () => finish(pip), { once: true });
  return true;
}

function finish(pip) {
  if (win.value !== pip) return;
  win.value = null;
  mount.value = null;
  open.value = false;
  setPipVisible(false);
  // Back to the main window's timers, and resync immediately so the ring shows
  // the true remaining time rather than whatever the last throttled tick left.
  setClockHost(null);
  syncClock();
}

export function closePip() {
  const pip = win.value;
  if (!pip) return;
  finish(pip);
  pip.close();
}

// Tests only.
export function __pipState() {
  return { win: win.value, mount: mount.value, open: open.value };
}
