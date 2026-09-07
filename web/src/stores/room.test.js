import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { useRoomStore } from './room.js';

// Every change in a room broadcasts to every member, and each of them answers
// by refetching the whole room. In a room of a hundred that turns one
// checkbox into a hundred requests, and several people acting at once
// multiplies it -- so a burst has to collapse into one fetch.
describe('room store refresh coalescing', () => {
  let fetchMock;

  beforeEach(() => {
    setActivePinia(createPinia());
    vi.useFakeTimers();
    fetchMock = vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({
        room: { code: 'ABC123', name: 'Test' },
        isCreator: false,
        memberCount: 1,
        timer: null,
        waiting: false,
        mine: [],
        others: [],
      }),
    }));
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('turns a burst of scheduled refreshes into one fetch', async () => {
    const room = useRoomStore();
    room.code = 'ABC123';

    for (let i = 0; i < 20; i += 1) room.scheduleRefresh();
    expect(fetchMock).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(300);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('still refreshes again after the window closes', async () => {
    const room = useRoomStore();
    room.code = 'ABC123';

    room.scheduleRefresh();
    await vi.advanceTimersByTimeAsync(300);
    room.scheduleRefresh();
    await vi.advanceTimersByTimeAsync(300);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('lets an explicit refresh satisfy a queued one', async () => {
    const room = useRoomStore();
    room.code = 'ABC123';

    room.scheduleRefresh();
    await room.refresh();
    await vi.advanceTimersByTimeAsync(300);
    // The queued one must not fire a second request on top of the explicit one.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('drops a queued refresh when the room closes', async () => {
    const room = useRoomStore();
    room.code = 'ABC123';

    room.scheduleRefresh();
    room.close();
    await vi.advanceTimersByTimeAsync(300);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
