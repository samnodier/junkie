import { describe, it, expect, afterEach, vi } from 'vitest';
import { createApp, h, nextTick, ref } from 'vue';

// pipWindow.js decides `supported` once, at module load, from whether the
// Document Picture-in-Picture API exists — so every case re-imports the whole
// chain (composable, clock, presence, component) against the globals it just
// installed, the same way notify.test.js does.

function fakePipWindow({ width, height }) {
  const doc = document.implementation.createHTMLDocument('pip');
  const listeners = {};
  const timers = new Map();
  let nextTimer = 1;
  const win = {
    document: doc,
    innerWidth: width,
    innerHeight: height,
    closed: false,
    addEventListener(type, fn) {
      (listeners[type] ||= []).push(fn);
    },
    removeEventListener(type, fn) {
      listeners[type] = (listeners[type] || []).filter((f) => f !== fn);
    },
    setInterval(fn) {
      const id = nextTimer++;
      timers.set(id, fn);
      return id;
    },
    clearInterval(id) {
      timers.delete(id);
    },
    close: vi.fn(() => {
      win.closed = true;
    }),
    ResizeObserver: class {
      constructor(cb) {
        this.cb = cb;
        (listeners.__ro ||= []).push(cb);
      }
      observe() {}
      disconnect() {
        listeners.__ro = (listeners.__ro || []).filter((c) => c !== this.cb);
      }
    },
    // --- test controls ---
    emit(type) {
      for (const fn of Array.from(listeners[type] || [])) fn();
    },
    tick() {
      for (const fn of Array.from(timers.values())) fn();
    },
    timerCount: () => timers.size,
    resize(w, h) {
      win.innerWidth = w;
      win.innerHeight = h;
      win.emit('resize');
      for (const cb of Array.from(listeners.__ro || [])) cb();
    },
  };
  return win;
}

async function load({ supported = true, styles = true } = {}) {
  vi.resetModules();
  vi.unstubAllGlobals();

  const opened = [];
  if (supported) {
    vi.stubGlobal('documentPictureInPicture', {
      requestWindow: vi.fn(async (options) => {
        const win = fakePipWindow(options);
        opened.push(win);
        return win;
      }),
    });
  }

  if (styles) {
    // One readable sheet and one cross-origin sheet whose rules throw, which
    // is exactly what the Google Fonts <link> in index.html does.
    const readable = { cssRules: [{ cssText: '.circle-timer { color: red; }' }], media: { mediaText: '' } };
    const blocked = {
      href: 'https://fonts.googleapis.com/css2?family=IBM+Plex+Mono',
      get cssRules() {
        throw new DOMException('cross-origin', 'SecurityError');
      },
    };
    Object.defineProperty(document, 'styleSheets', {
      configurable: true,
      get: () => [readable, blocked],
    });
  }

  const pipMod = await import('@/composables/pipWindow');
  const clockMod = await import('@/composables/clock');
  const presence = await import('@/lib/presence');
  const PipTimer = (await import('./PipTimer.vue')).default;
  return { pipMod, clockMod, presence, PipTimer, opened };
}

// A stand-in for a timer card: its markup depends on a phase ref the test
// controls, standing in for the room store being updated by a socket signal.
function harness(PipTimer, phase, now) {
  const el = document.createElement('div');
  document.body.appendChild(el);
  const app = createApp({
    setup() {
      return () =>
        h(PipTimer, null, {
          default: () => [
            h('p', { class: 'label' }, phase.value),
            h('div', { class: 'circle-timer', id: 'ring' }, [
              h('div', { class: 'circle-timer-countdown' }, String(now.value)),
            ]),
            h('button', { class: 'join' }, 'Join this block'),
          ],
        });
    },
  });
  app.mount(el);
  return { el, app };
}

describe('popped-out timer', () => {
  let app = null;

  afterEach(() => {
    app?.unmount();
    app = null;
    document.body.innerHTML = '';
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it('offers no pop-out control where the API is missing (Safari)', async () => {
    const { PipTimer, pipMod } = await load({ supported: false });
    expect(pipMod.supported).toBe(false);
    const h1 = harness(PipTimer, ref('Focus'), ref('25:00'));
    app = h1.app;
    await nextTick();
    expect(h1.el.querySelector('.pip-pop')).toBeNull();
    // …and the card is still right there on the page.
    expect(h1.el.querySelector('#ring')).not.toBeNull();
  });

  it('moves the very same card into the pop-out, rather than making a copy', async () => {
    const { PipTimer, pipMod, opened } = await load();
    const h1 = harness(PipTimer, ref('Focus'), ref('25:00'));
    app = h1.app;
    await nextTick();

    const ringBefore = h1.el.querySelector('#ring');
    expect(ringBefore).not.toBeNull();

    await pipMod.openPip();
    await nextTick();

    const pipDoc = opened[0].document;
    const ringAfter = pipDoc.querySelector('#ring');
    // Identity, not just presence: one timer, one set of subscriptions, one
    // source of state. A copy would tick independently and drift.
    expect(ringAfter).toBe(ringBefore);
    expect(h1.el.querySelector('#ring')).toBeNull();
    expect(pipDoc.querySelector('.pip-root')).not.toBeNull();
    expect(h1.el.querySelector('.pip-parked')).not.toBeNull();
  });

  it('carries the stylesheet across, re-linking the ones it cannot read', async () => {
    const { pipMod, opened } = await load();
    await pipMod.openPip();
    const head = opened[0].document.head;
    expect(head.querySelector('style').textContent).toContain('.circle-timer');
    expect(head.querySelector('link[rel="stylesheet"]').getAttribute('href')).toContain(
      'fonts.googleapis.com'
    );
  });

  it('carries the current theme across', async () => {
    document.documentElement.setAttribute('data-theme', 'light');
    const { pipMod, opened } = await load();
    await pipMod.openPip();
    expect(opened[0].document.documentElement.getAttribute('data-theme')).toBe('light');
    document.documentElement.setAttribute('data-theme', 'dark');
  });

  it('drives the countdown from the pop-out while the owning tab is throttled', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-30T12:00:00Z'));
    const { pipMod, clockMod, opened } = await load();
    const now = clockMod.useClock();

    await pipMod.openPip();
    const pip = opened[0];
    expect(pip.timerCount()).toBe(1);

    // Tab hidden: its own timers stop firing.
    vi.advanceTimersByTime(120_000);
    expect(now.value).not.toBe(Date.now());
    pip.tick();
    expect(now.value).toBe(Date.now());
  });

  it('keeps counting as watched, so the safety-net poll stays on with the tab hidden', async () => {
    const { pipMod, presence } = await load();
    const spy = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');

    expect(presence.isWatching()).toBe(false);
    await pipMod.openPip();
    expect(presence.isWatching()).toBe(true);

    pipMod.closePip();
    expect(presence.isWatching()).toBe(false);
    spy.mockRestore();
  });

  it('follows the run through phase changes it was never touched for', async () => {
    // Sam's case: auto-breaks on, check-in off, the window shrunk into a
    // corner. The server advances the phase and the socket updates the store;
    // nothing here is clicked. The popped-out card has to follow along.
    const { PipTimer, pipMod, opened } = await load();
    const phase = ref('Focus · session 1 of 3');
    const clock = ref('25:00');
    const h1 = harness(PipTimer, phase, clock);
    app = h1.app;
    await nextTick();

    await pipMod.openPip();
    await nextTick();
    const pipDoc = opened[0].document;
    expect(pipDoc.querySelector('.label').textContent).toBe('Focus · session 1 of 3');

    // …the focus block ends server-side and the socket pushes the break.
    phase.value = 'Break · next session in';
    clock.value = '05:00';
    await nextTick();
    expect(pipDoc.querySelector('.label').textContent).toBe('Break · next session in');
    expect(pipDoc.querySelector('.circle-timer-countdown').textContent).toBe('05:00');

    // …and on into the next focus block, still untouched.
    phase.value = 'Focus · session 2 of 3';
    clock.value = '25:00';
    await nextTick();
    expect(pipDoc.querySelector('.label').textContent).toBe('Focus · session 2 of 3');
    // Still one card, still only in the pop-out.
    expect(h1.el.querySelector('#ring')).toBeNull();
    expect(pipDoc.querySelectorAll('#ring')).toHaveLength(1);
  });

  it('survives a phase change that swaps which part of the page owns the card', async () => {
    // The room page renders a fullscreen view while you are inside a focus
    // block and a card in the timer column otherwise, so crossing the focus
    // boundary destroys the component that owns the pop-out and builds a
    // different one. The card has to reappear in the pop-out, not get
    // stranded on the page nobody is looking at.
    const { PipTimer, pipMod, opened } = await load();
    const focusing = ref(false);
    const el = document.createElement('div');
    document.body.appendChild(el);
    const local = createApp({
      setup: () => () => {
        const card = () => h('div', { class: 'circle-timer', id: 'ring' }, focusing.value ? 'focus' : 'shell');
        return focusing.value
          ? h('div', { class: 'room-focus-page' }, [h(PipTimer, null, { default: card })])
          : h('section', { class: 'room-shell' }, [h(PipTimer, null, { default: card })]);
      },
    });
    local.mount(el);
    await nextTick();

    await pipMod.openPip();
    await nextTick();
    const pipDoc = opened[0].document;
    expect(pipDoc.querySelector('#ring').textContent).toBe('shell');

    // The focus block starts: different branch, different PipTimer instance.
    focusing.value = true;
    await nextTick();
    expect(pipDoc.querySelectorAll('#ring')).toHaveLength(1);
    expect(pipDoc.querySelector('#ring').textContent).toBe('focus');
    expect(el.querySelectorAll('#ring')).toHaveLength(0);
    expect(el.querySelector('.pip-parked')).not.toBeNull();

    // …and back out to the break, still popped out.
    focusing.value = false;
    await nextTick();
    expect(pipDoc.querySelectorAll('#ring')).toHaveLength(1);
    expect(pipDoc.querySelector('#ring').textContent).toBe('shell');
    expect(el.querySelectorAll('#ring')).toHaveLength(0);

    opened[0].emit('pagehide');
    await nextTick();
    expect(el.querySelectorAll('#ring')).toHaveLength(1);
    local.unmount();
    el.remove();
  });

  it('collapses to the ring alone when shrunk, and restores the controls when grown', async () => {
    const { pipMod, opened } = await load();
    await pipMod.openPip();
    const pip = opened[0];
    const root = pip.document.querySelector('.pip-root');
    expect(root.dataset.pipSize).toBe('full');

    pip.resize(200, 200);
    expect(root.dataset.pipSize).toBe('mini');

    pip.resize(360, 380);
    expect(root.dataset.pipSize).toBe('full');
  });

  it('puts the card back on the page when the window is closed from the OS', async () => {
    const { PipTimer, pipMod, clockMod, opened } = await load();
    const h1 = harness(PipTimer, ref('Focus'), ref('25:00'));
    app = h1.app;
    await nextTick();
    const ringBefore = h1.el.querySelector('#ring');

    await pipMod.openPip();
    await nextTick();
    const pip = opened[0];
    expect(h1.el.querySelector('#ring')).toBeNull();

    // Not our close button — the window's own, or the OS closing it.
    pip.emit('pagehide');
    await nextTick();

    expect(h1.el.querySelector('#ring')).toBe(ringBefore);
    expect(h1.el.querySelector('.pip-parked')).toBeNull();
    expect(h1.el.querySelector('.pip-pop')).not.toBeNull();
    expect(pipMod.__pipState().open).toBe(false);
    // The tick is back on the main window.
    expect(pip.timerCount()).toBe(0);
    const now = clockMod.useClock();
    expect(now.value).toBeGreaterThan(0);
  });

  it('brings the card back from the in-page button', async () => {
    const { PipTimer, pipMod, opened } = await load();
    const h1 = harness(PipTimer, ref('Focus'), ref('25:00'));
    app = h1.app;
    await nextTick();

    await pipMod.openPip();
    await nextTick();
    h1.el.querySelector('.pip-parked button').click();
    await nextTick();

    expect(opened[0].close).toHaveBeenCalled();
    expect(h1.el.querySelector('#ring')).not.toBeNull();
  });

  it('survives a pop-out / return / pop-out cycle', async () => {
    const { PipTimer, pipMod, opened } = await load();
    const h1 = harness(PipTimer, ref('Focus'), ref('25:00'));
    app = h1.app;
    await nextTick();

    for (let i = 0; i < 3; i += 1) {
      await pipMod.openPip();
      await nextTick();
      expect(opened[i].document.querySelectorAll('#ring')).toHaveLength(1);
      expect(h1.el.querySelectorAll('#ring')).toHaveLength(0);

      opened[i].emit('pagehide');
      await nextTick();
      expect(h1.el.querySelectorAll('#ring')).toHaveLength(1);
    }
    expect(opened).toHaveLength(3);
  });

  it('refuses to open a second window over the first', async () => {
    const { pipMod, opened } = await load();
    expect(await pipMod.openPip()).toBe(true);
    expect(await pipMod.openPip()).toBe(false);
    expect(opened).toHaveLength(1);
  });

  it('leaves the card on the page when the browser refuses the window', async () => {
    const { PipTimer, pipMod, clockMod } = await load();
    vi.stubGlobal('documentPictureInPicture', {
      requestWindow: vi.fn(async () => {
        throw new DOMException('user gesture required', 'NotAllowedError');
      }),
    });
    const h1 = harness(PipTimer, ref('Focus'), ref('25:00'));
    app = h1.app;
    await nextTick();

    expect(await pipMod.openPip()).toBe(false);
    await nextTick();
    expect(h1.el.querySelector('#ring')).not.toBeNull();
    expect(h1.el.querySelector('.pip-parked')).toBeNull();
    expect(clockMod.useClock().value).toBeGreaterThan(0);
  });
});
