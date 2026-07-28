import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { useAuthStore } from './auth.js';

// The server runs UTC and has no other way to know which calendar day a user
// is having, so the browser reports its own zone. Nothing is ever asked of the
// user, and a normal page load must not cost an extra request.
function stubZone(timeZone) {
  vi.spyOn(Intl, 'DateTimeFormat').mockReturnValue({
    resolvedOptions: () => ({ timeZone }),
  });
}

function stubMe(body) {
  const fetchMock = vi.fn(async (url) => {
    if (url === '/api/me') return { ok: true, json: async () => body };
    return { ok: true, json: async () => ({}) };
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

const posts = (fetchMock) =>
  fetchMock.mock.calls.filter(([url, opts]) => url === '/api/timezone' && opts?.method === 'POST');

describe('timezone reporting', () => {
  beforeEach(() => setActivePinia(createPinia()));

  it('reports the browser zone when the server has none yet', async () => {
    stubZone('Africa/Lagos');
    const fetchMock = stubMe({ user: { id: 'u1', displayName: 'Sam' } });

    await useAuthStore().load();

    const sent = posts(fetchMock);
    expect(sent).toHaveLength(1);
    expect(sent[0][1].body).toBe('timezone=Africa%2FLagos');
  });

  it('reports again when the stored zone is stale, so travelling corrects it', async () => {
    stubZone('Asia/Tokyo');
    const fetchMock = stubMe({ user: { id: 'u1' }, timezone: 'Africa/Lagos' });

    await useAuthStore().load();

    expect(posts(fetchMock)).toHaveLength(1);
    expect(posts(fetchMock)[0][1].body).toBe('timezone=Asia%2FTokyo');
  });

  it('stays quiet when the server already agrees', async () => {
    stubZone('Africa/Lagos');
    const fetchMock = stubMe({ user: { id: 'u1' }, timezone: 'Africa/Lagos' });

    await useAuthStore().load();

    expect(posts(fetchMock)).toHaveLength(0);
  });

  it('reports nothing for a guest', async () => {
    stubZone('Africa/Lagos');
    const fetchMock = stubMe({ user: null });

    await useAuthStore().load();

    expect(posts(fetchMock)).toHaveLength(0);
  });

  it('gives up quietly when the browser exposes no zone', async () => {
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(() => {
      throw new Error('no Intl');
    });
    const fetchMock = stubMe({ user: { id: 'u1' } });

    const store = useAuthStore();
    await expect(store.load()).resolves.toBeUndefined();

    expect(posts(fetchMock)).toHaveLength(0);
    expect(store.user).toEqual({ id: 'u1' }); // sign-in still succeeded
  });

  it('does not fail the session load when reporting fails', async () => {
    stubZone('Africa/Lagos');
    vi.stubGlobal('fetch', vi.fn(async (url) => {
      if (url === '/api/me') return { ok: true, json: async () => ({ user: { id: 'u1' } }) };
      throw new Error('network down');
    }));

    const store = useAuthStore();
    await store.load();

    expect(store.user).toEqual({ id: 'u1' });
    expect(store.loaded).toBe(true);
  });
});

describe('session state', () => {
  beforeEach(() => setActivePinia(createPinia()));

  it('marks itself loaded even when /api/me fails, so guards do not hang', async () => {
    stubZone('UTC');
    vi.stubGlobal('fetch', vi.fn(async () => { throw new Error('offline'); }));

    const store = useAuthStore();
    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.isAuthed).toBe(false);
  });

  it('derives the avatar URL only when one was uploaded', async () => {
    stubZone('UTC');
    stubMe({ user: { id: 'u1', hasAvatar: true, avatarVersion: 42 } });
    const store = useAuthStore();
    await store.load();

    expect(store.avatarURL).toBe('/avatar/u1?v=42');

    store.user = { id: 'u2', hasAvatar: false };
    expect(store.avatarURL).toBe('');
  });
});
