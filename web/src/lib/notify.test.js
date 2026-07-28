import { describe, it, expect, beforeEach, vi } from 'vitest';

// notify.js caches a service worker registration at module scope, so each test
// re-imports it fresh against whatever globals it just installed.
async function loadNotify({ permission = 'granted', worker = true } = {}) {
  vi.resetModules();

  const shown = [];
  const live = [];
  const registration = {
    showNotification: vi.fn(async (title, options) => {
      shown.push({ title, ...options });
      const n = { tag: options?.tag, close: vi.fn(() => {
        const i = live.indexOf(n);
        if (i >= 0) live.splice(i, 1);
      }) };
      live.push(n);
    }),
    getNotifications: vi.fn(async ({ tag } = {}) => live.filter((n) => !tag || n.tag === tag)),
  };

  const constructed = [];
  class FakeNotification {
    constructor(title, options) {
      constructed.push({ title, ...options });
      this.close = vi.fn();
    }
  }
  FakeNotification.permission = permission;
  FakeNotification.requestPermission = vi.fn(async () => permission);

  vi.stubGlobal('Notification', FakeNotification);
  if (worker) {
    vi.stubGlobal('navigator', { serviceWorker: { ready: Promise.resolve(registration) } });
  } else {
    vi.stubGlobal('navigator', {});
  }

  const mod = await import('./notify.js');
  return { mod, shown, constructed, registration, live, FakeNotification };
}

// Wait for the module's internal promise chain (registration lookup) to settle.
const settle = () => new Promise((r) => setTimeout(r, 0));

describe('delivery path', () => {
  // The bug this file exists for: `new Notification()` is an illegal
  // constructor on Android and throws, and the old code swallowed it in a
  // catch — so phones silently got nothing at all.
  it('delivers through the service worker registration, not the constructor', async () => {
    const { mod, shown, constructed } = await loadNotify();
    mod.notify('Focus session complete', 'Time for a break');
    await settle();

    expect(shown).toHaveLength(1);
    expect(shown[0].title).toBe('Focus session complete');
    expect(constructed).toHaveLength(0);
  });

  it('falls back to the page constructor when no worker is active', async () => {
    const { mod, shown, constructed } = await loadNotify({ worker: false });
    mod.notify('Focus session complete', 'Time for a break');
    await settle();

    expect(shown).toHaveLength(0);
    expect(constructed).toHaveLength(1);
    expect(constructed[0].title).toBe('Focus session complete');
  });

  it('stays silent when permission was never granted', async () => {
    const { mod, shown, constructed } = await loadNotify({ permission: 'default' });
    mod.notify('Focus session complete', 'Time for a break');
    await settle();

    expect(shown).toHaveLength(0);
    expect(constructed).toHaveLength(0);
  });

  it('carries a click target so the worker can focus the right page', async () => {
    const { mod, shown } = await loadNotify();
    mod.notify('Break time in Study Hall', 'Join now', '/r/AB-CD');
    await settle();

    expect(shown[0].data).toEqual({ url: '/r/AB-CD' });
  });
});

describe('ongoing timer notification', () => {
  const focus = { phase: 'focus', endsAt: '2026-07-28T15:45:00.000Z' };

  it('shows the end time, replacing in place rather than stacking', async () => {
    const { mod, shown } = await loadNotify();
    mod.syncTimerNotification('desk', focus);
    await settle();

    expect(shown).toHaveLength(1);
    expect(shown[0].title).toBe('Focusing');
    expect(shown[0].body).toMatch(/^Until /);
    expect(shown[0].tag).toBe('junkie-timer');
    expect(shown[0].silent).toBe(true);
  });

  it('labels the room when the block belongs to one', async () => {
    const { mod, shown } = await loadNotify();
    mod.syncTimerNotification('room', focus, { label: 'Study Hall', url: '/r/AB-CD' });
    await settle();

    expect(shown[0].title).toBe('Focusing · Study Hall');
    expect(shown[0].data).toEqual({ url: '/r/AB-CD' });
  });

  it.each([
    ['break', { phase: 'break', endsAt: focus.endsAt }, 'On a break', /^Until /],
    ['paused', { phase: 'focus', paused: true }, 'Focusing', /^Paused$/],
    ['break offer', { phase: 'break_offer' }, 'Break ready', /ready/],
    ['pending break', { phase: 'break', breakPending: true }, 'Break ready', /ready/],
  ])('renders the %s phase', async (_name, timer, title, body) => {
    const { mod, shown } = await loadNotify();
    mod.syncTimerNotification('desk', timer);
    await settle();

    expect(shown[0].title).toBe(title);
    expect(shown[0].body).toMatch(body);
  });

  // The guest tick calls this every second and the stores call it on every
  // poll; reposting each time would redraw the notification and flicker it.
  it('does not repost while the phase is unchanged', async () => {
    const { mod, shown } = await loadNotify();
    for (let i = 0; i < 5; i++) {
      mod.syncTimerNotification('desk', focus);
      await settle();
    }
    expect(shown).toHaveLength(1);
  });

  it('reposts when the phase actually turns over', async () => {
    const { mod, shown } = await loadNotify();
    mod.syncTimerNotification('desk', focus);
    await settle();
    mod.syncTimerNotification('desk', { phase: 'break', endsAt: focus.endsAt });
    await settle();

    expect(shown.map((s) => s.title)).toEqual(['Focusing', 'On a break']);
  });

  it('takes the notification down when the timer clears', async () => {
    const { mod, live } = await loadNotify();
    mod.syncTimerNotification('desk', focus);
    await settle();
    expect(live).toHaveLength(1);

    mod.syncTimerNotification('desk', null);
    await settle();
    expect(live).toHaveLength(0);
  });

  // The desk and room stores both run this. Without ownership, the desk
  // finding no solo timer would clear the room's notification out from under
  // it on the very next poll.
  it('lets only the owning source clear the notification', async () => {
    const { mod, live } = await loadNotify();
    mod.syncTimerNotification('room', focus, { label: 'Study Hall' });
    await settle();
    expect(live).toHaveLength(1);

    mod.syncTimerNotification('desk', null); // desk has no solo timer
    await settle();
    expect(live).toHaveLength(1);

    mod.syncTimerNotification('room', null); // the owner ends its run
    await settle();
    expect(live).toHaveLength(0);
  });

  it('ignores an idle timer', async () => {
    const { mod, shown } = await loadNotify();
    mod.syncTimerNotification('desk', { phase: 'idle' });
    await settle();
    expect(shown).toHaveLength(0);
  });
});

describe('permission timing', () => {
  // The prompt is answered well after the timer that triggered it started, so
  // a sync dropped for lack of permission has to be replayed — otherwise the
  // shade sits empty until the next 45s poll.
  it('replays a sync that was dropped before permission was granted', async () => {
    const { mod, shown, FakeNotification } = await loadNotify({ permission: 'default' });

    mod.syncTimerNotification('desk', { phase: 'focus', endsAt: '2026-07-28T15:45:00.000Z' });
    await settle();
    expect(shown).toHaveLength(0);

    // A browser only flips Notification.permission when the user answers the
    // prompt, which is after requestPermission() has already returned.
    FakeNotification.requestPermission.mockImplementation(async () => {
      FakeNotification.permission = 'granted';
      return 'granted';
    });
    mod.requestPermission();
    await settle();
    await settle();

    expect(shown).toHaveLength(1);
    expect(shown[0].title).toBe('Focusing');
  });

  it('does not re-prompt once the user has already answered', async () => {
    const { mod, FakeNotification } = await loadNotify({ permission: 'denied' });
    mod.requestPermission();
    expect(FakeNotification.requestPermission).not.toHaveBeenCalled();
  });
});

describe('room invite notifications', () => {
  it('stays quiet while the tab is focused', async () => {
    const { mod, shown } = await loadNotify();
    vi.spyOn(document, 'hasFocus').mockReturnValue(true);
    mod.onRoomInvite('Sam', 'Study Hall');
    await settle();
    expect(shown).toHaveLength(0);
  });

  it('notifies when the tab is in the background', async () => {
    const { mod, shown } = await loadNotify();
    vi.spyOn(document, 'hasFocus').mockReturnValue(false);
    mod.onRoomInvite('Sam', 'Study Hall');
    await settle();
    expect(shown).toHaveLength(1);
    expect(shown[0].title).toContain('Sam');
  });

  it('respects the room-invite opt-out', async () => {
    const { mod, shown } = await loadNotify();
    vi.spyOn(document, 'hasFocus').mockReturnValue(false);
    localStorage.setItem('junkie:roomInviteNotifications', 'disabled');
    mod.onRoomInvite('Sam', 'Study Hall');
    await settle();
    expect(shown).toHaveLength(0);
    localStorage.clear();
  });
});

describe('phase-end alerts', () => {
  beforeEach(() => localStorage.clear());

  it.each([
    ['focus', 'Focus session complete'],
    ['break', "Break's over"],
  ])('announces the end of a %s block', async (phase, title) => {
    const { mod, shown } = await loadNotify();
    mod.onTimerEnd(phase);
    await settle();
    expect(shown[0].title).toBe(title);
  });
});
