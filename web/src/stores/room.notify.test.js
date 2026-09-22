import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';

// A phase turn used to put two notifications in the shade: the "focus
// session complete" alert, which carried no tag, and the ongoing timer
// status that replaced itself under `junkie-timer`. On a phone the pair
// reads correctly; on a desktop both stick and one turn looks like a
// duplicate. These tests pin down that the pair now shares one tag, and
// that sharing it doesn't let the status clear an alert it never posted.
vi.mock('@/lib/sound', () => ({ playPhaseSound: vi.fn() }));

import { useRoomStore } from './room.js';

const TIMER_TAG = 'junkie-timer';

let shown;
let openNotifications;

function roomPayload(timer) {
  return {
    ok: true,
    status: 200,
    json: async () => ({
      room: { code: 'ABC123', name: 'Study', ephemeral: false },
      isCreator: false,
      memberCount: 2,
      timer,
      waiting: false,
      mine: [],
      others: [],
    }),
  };
}

function stubEnvironment() {
  shown = [];
  openNotifications = [];
  vi.stubGlobal('Notification', { permission: 'granted' });
  vi.stubGlobal('navigator', {
    serviceWorker: {
      ready: Promise.resolve({
        showNotification: async (title, options) => {
          shown.push({ title, options });
        },
        getNotifications: async () => openNotifications,
      }),
    },
  });
  vi.stubGlobal('document', {
    visibilityState: 'visible',
    hasFocus: () => true,
    addEventListener() {},
    removeEventListener() {},
  });
}

// show() hops through navigator.serviceWorker.ready, so the posts land a
// microtask or two after refresh() resolves.
const settle = () => new Promise((resolve) => setTimeout(resolve, 50));

describe('one phase turn, one notification', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    stubEnvironment();
  });

  afterEach(() => vi.unstubAllGlobals());

  it('gives every notification on a focus->break turn the same tag', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => roomPayload({
      phase: 'break',
      participant: true,
      secondsLeft: 300,
      endsAt: '2026-09-22T17:00:00Z',
    })));

    const room = useRoomStore();
    room.code = 'ABC123';
    room.lastPhase = 'focus';

    await room.refresh();
    await settle();

    // Both halves still post -- the alert announces the turn, the status
    // carries the new phase -- but a shared tag means the second replaces
    // the first rather than stacking beside it.
    expect(shown.length).toBeGreaterThan(0);
    expect(new Set(shown.map((s) => s.options.tag))).toEqual(new Set([TIMER_TAG]));
  });

  it('keeps the alert up when the run ends outright', async () => {
    // focus -> idle: the status has nothing to show, so it clears the tag.
    // The alert posted moments earlier lives under that same tag now, so the
    // clear has to recognise it as somebody else's notification and leave it.
    let timer = { phase: 'focus', participant: true, secondsLeft: 300, endsAt: '2026-09-22T17:00:00Z' };
    vi.stubGlobal('fetch', vi.fn(async () => roomPayload(timer)));

    const room = useRoomStore();
    room.code = 'ABC123';
    room.lastPhase = 'focus';

    // A live phase first, so the status notification is the one holding the
    // tag when the run ends. Without this the clear has nothing to own and
    // the test would pass either way.
    await room.refresh();
    await settle();
    shown.length = 0;
    const close = vi.fn();
    openNotifications = [{ close }];

    timer = null;
    await room.refresh();
    await settle();

    expect(shown.map((s) => s.title)).toContain('Focus session complete');
    expect(close).not.toHaveBeenCalled();
  });
});
