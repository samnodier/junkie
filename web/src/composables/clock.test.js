import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, h } from 'vue';
import { useClock, setClockHost, syncClock, __resetClock } from './clock';

// A stand-in for the picture-in-picture window: it owns its own timer queue,
// so a test can hold the main window's timers still (that's what a hidden tab
// does — throttles them to about once a minute) and still drive ticks from
// here, which is the whole reason the clock is re-homable.
function fakeWindow() {
  const timers = new Map();
  let next = 1;
  return {
    setInterval(fn) {
      const id = next++;
      timers.set(id, fn);
      return id;
    },
    clearInterval(id) {
      timers.delete(id);
    },
    tick() {
      for (const fn of Array.from(timers.values())) fn();
    },
    count: () => timers.size,
  };
}

function mount(setup) {
  const el = document.createElement('div');
  const app = createApp({ setup: () => (setup(), () => h('div')) });
  app.mount(el);
  return app;
}

describe('shared clock', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-30T12:00:00Z'));
    __resetClock();
  });
  afterEach(() => {
    __resetClock();
    vi.useRealTimers();
  });

  it('tracks real time off the main window', () => {
    const now = useClock();
    const start = now.value;
    vi.advanceTimersByTime(1000);
    expect(now.value).toBe(start + 1000);
  });

  it('shares one interval across every subscriber', () => {
    const a = useClock();
    const b = useClock();
    expect(a).toBe(b);
    vi.advanceTimersByTime(1000);
    expect(a.value).toBe(b.value);
  });

  it('keeps ticking from the pip window while the owning tab is throttled', () => {
    const now = useClock();
    const pip = fakeWindow();

    setClockHost(pip);
    expect(pip.count()).toBe(1);

    // The tab is hidden: its own timers no longer fire. Wall-clock time moves
    // on regardless, so a clock that only listened to the tab would freeze
    // here — this is the bug the whole indirection exists to prevent.
    vi.advanceTimersByTime(60_000);
    expect(now.value).not.toBe(Date.now());

    pip.tick();
    expect(now.value).toBe(Date.now());
  });

  it('hands the tick back to the main window when the pop-out closes', () => {
    const now = useClock();
    const pip = fakeWindow();
    setClockHost(pip);
    vi.advanceTimersByTime(30_000);

    setClockHost(null);
    expect(pip.count()).toBe(0);
    // Re-homing resyncs immediately rather than showing a stale reading until
    // the next tick.
    expect(now.value).toBe(Date.now());

    vi.advanceTimersByTime(1000);
    expect(now.value).toBe(Date.now());
  });

  it('is idempotent when handed the host it already has', () => {
    useClock();
    const pip = fakeWindow();
    setClockHost(pip);
    setClockHost(pip);
    expect(pip.count()).toBe(1);
  });

  it('syncClock catches up without waiting for a tick', () => {
    const now = useClock();
    const pip = fakeWindow();
    setClockHost(pip);
    vi.advanceTimersByTime(5000);
    expect(now.value).not.toBe(Date.now());
    syncClock();
    expect(now.value).toBe(Date.now());
  });

  it('stops the interval once the last subscriber unmounts', () => {
    const pip = fakeWindow();
    setClockHost(pip);
    const app = mount(() => useClock());
    expect(pip.count()).toBe(1);
    app.unmount();
    expect(pip.count()).toBe(0);
  });
});
