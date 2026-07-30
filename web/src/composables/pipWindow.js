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
// Below this the card can't show its controls legibly, so it collapses to the
// bare ring (see .pip-root[data-pip-size="mini"] in app.css). The user gets the
// controls back by making the window bigger again.
const MINI_WIDTH = 260;
const MINI_HEIGHT = 300;
const DEFAULT_SIZE = { width: 380, height: 440 };

export const supported =
  typeof window !== 'undefined' &&
  'documentPictureInPicture' in window &&
  typeof window.documentPictureInPicture?.requestWindow === 'function';

const win = shallowRef(null);
// Teleport target. Null whenever the pop-out is closed, which is also the
// signal that sends the card back to the page.
const mount = shallowRef(null);
const size = ref('full');
const open = ref(false);

export function usePipWindow() {
  return {
    supported,
    open: readonly(open),
    size: readonly(size),
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

function applySize(target) {
  const w = target.innerWidth || 0;
  const h = target.innerHeight || 0;
  const next = w < MINI_WIDTH || h < MINI_HEIGHT ? 'mini' : 'full';
  size.value = next;
  const root = mount.value;
  if (root) root.dataset.pipSize = next;
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
  applySize(pip);

  // The opener tab is hidden while the pop-out is in use, which throttles its
  // timers to about once a minute. This window is visible, so its timers run
  // at full rate — drive the countdown from here.
  setClockHost(pip);
  syncClock();
  setPipVisible(true);

  const onResize = () => {
    applySize(pip);
    rememberSize(pip);
  };
  pip.addEventListener('resize', onResize);
  if (typeof pip.ResizeObserver === 'function') {
    const observer = new pip.ResizeObserver(() => applySize(pip));
    observer.observe(doc.documentElement);
    pip.__junkieObserver = observer;
  }
  // Closing the window (its own close button, or the OS) has to put the card
  // back on the page — dropping the mount does that, since the Teleport falls
  // back to its in-page slot.
  pip.addEventListener('pagehide', () => finish(pip), { once: true });
  return true;
}

function finish(pip) {
  if (win.value !== pip) return;
  pip.__junkieObserver?.disconnect();
  delete pip.__junkieObserver;
  win.value = null;
  mount.value = null;
  open.value = false;
  size.value = 'full';
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
  return { win: win.value, mount: mount.value, open: open.value, size: size.value };
}
