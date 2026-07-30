import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, h, nextTick, reactive } from 'vue';
import RingCountdown from './RingCountdown.vue';
import { setClockHost, __resetClock } from '@/composables/clock';

const MINUTE = 60_000;

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
  };
}

function mountRing({ endsAt, totalSeconds }) {
  const el = document.createElement('div');
  document.body.appendChild(el);
  const props = reactive({ endsAt, totalSeconds });
  const expired = vi.fn();
  const app = createApp({
    setup: () => () => h(RingCountdown, { ...props, onExpired: expired }),
  });
  app.mount(el);
  return {
    props,
    expired,
    text: () => el.querySelector('.circle-timer-countdown').textContent,
    dashOffset: () =>
      Number(el.querySelector('.circle-timer-progress').getAttribute('stroke-dashoffset')),
    unmount: () => {
      app.unmount();
      el.remove();
    },
  };
}

const inMinutes = (m) => new Date(Date.now() + m * MINUTE).toISOString();

describe('RingCountdown', () => {
  let ring = null;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-30T12:00:00Z'));
    __resetClock();
  });
  afterEach(() => {
    ring?.unmount();
    ring = null;
    __resetClock();
    vi.useRealTimers();
  });

  it('shows the remaining time straight away, without waiting a tick', () => {
    ring = mountRing({ endsAt: inMinutes(25), totalSeconds: 25 * 60 });
    expect(ring.text()).toBe('25:00');
  });

  it('counts down against the absolute deadline', async () => {
    ring = mountRing({ endsAt: inMinutes(25), totalSeconds: 25 * 60 });
    vi.advanceTimersByTime(90_000);
    await nextTick();
    expect(ring.text()).toBe('23:30');
  });

  it('lands on the true remaining time after a throttled gap, not a decremented one', async () => {
    // The regression this guards: a ring that subtracted one second per tick
    // would read 24:59 here, because only a single (throttled) tick fired
    // across five real minutes.
    ring = mountRing({ endsAt: inMinutes(25), totalSeconds: 25 * 60 });
    const pip = fakeWindow();
    setClockHost(pip);

    vi.advanceTimersByTime(5 * MINUTE);
    pip.tick();
    await nextTick();
    expect(ring.text()).toBe('20:00');
  });

  it('fills the ring by the fraction remaining', async () => {
    ring = mountRing({ endsAt: inMinutes(10), totalSeconds: 10 * 60 });
    expect(ring.dashOffset()).toBeCloseTo(0, 5);
    vi.advanceTimersByTime(5 * MINUTE);
    await nextTick();
    // Half gone, so half the circumference is unfilled.
    expect(ring.dashOffset()).toBeCloseTo((2 * Math.PI * 88) / 2, 1);
  });

  it('stops at 00:00 rather than going negative', async () => {
    ring = mountRing({ endsAt: inMinutes(1), totalSeconds: 60 });
    vi.advanceTimersByTime(5 * MINUTE);
    await nextTick();
    expect(ring.text()).toBe('00:00');
    expect(ring.dashOffset()).toBeCloseTo(2 * Math.PI * 88, 1);
  });

  it('emits expired exactly once, however many ticks follow', async () => {
    ring = mountRing({ endsAt: inMinutes(1), totalSeconds: 60 });
    expect(ring.expired).not.toHaveBeenCalled();

    vi.advanceTimersByTime(MINUTE + 1000);
    await nextTick();
    expect(ring.expired).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(5 * MINUTE);
    await nextTick();
    expect(ring.expired).toHaveBeenCalledTimes(1);
  });

  it('never expires on a blank deadline', async () => {
    // '' is what useTimerDeadline reports when there is no run. Firing the
    // phase-advance nudge on that would push a run forward on nothing.
    ring = mountRing({ endsAt: '', totalSeconds: 60 });
    await nextTick();
    vi.advanceTimersByTime(10 * MINUTE);
    await nextTick();
    expect(ring.expired).not.toHaveBeenCalled();
    expect(ring.text()).toBe('00:00');
  });

  it('rearms when the deadline moves to the next phase', async () => {
    ring = mountRing({ endsAt: inMinutes(1), totalSeconds: 60 });
    vi.advanceTimersByTime(MINUTE + 1000);
    await nextTick();
    expect(ring.expired).toHaveBeenCalledTimes(1);

    // Same component, new phase: the break's deadline arrives on the props.
    ring.props.endsAt = inMinutes(5);
    ring.props.totalSeconds = 300;
    await nextTick();
    expect(ring.text()).toBe('05:00');

    vi.advanceTimersByTime(5 * MINUTE + 1000);
    await nextTick();
    expect(ring.expired).toHaveBeenCalledTimes(2);
  });

  it('does not double-emit for a deadline it already passed', async () => {
    ring = mountRing({ endsAt: inMinutes(-5), totalSeconds: 60 });
    await nextTick();
    expect(ring.expired).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(MINUTE);
    await nextTick();
    expect(ring.expired).toHaveBeenCalledTimes(1);
  });
});
