import { describe, it, expect, beforeEach, vi } from 'vitest';

// sound.js caches both the manifest fetch and the <audio> element at module
// scope, so every test re-imports it against freshly installed globals.
async function loadSound({ manifest = [{ id: 'chime', name: 'Soft chime', file: 'chime.wav' }] } = {}) {
  vi.resetModules();

  const fetched = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url) => {
      fetched.push(String(url));
      if (manifest === 'error') throw new Error('offline');
      return { ok: true, json: async () => manifest };
    })
  );

  const played = [];
  const built = [];
  class FakeAudio {
    constructor(src) {
      this.src = src;
      this.currentTime = 7; // non-zero, so rewinding is observable
      built.push(this);
      this.play = vi.fn(async () => played.push(this.src));
    }
  }
  vi.stubGlobal('Audio', FakeAudio);

  const mod = await import('./sound.js');
  return { mod, played, built, fetched };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

describe('phase-end sound', () => {
  beforeEach(() => localStorage.clear());

  it('is off until switched on', async () => {
    const { mod, played, fetched } = await loadSound();
    expect(mod.soundEnabled()).toBe(false);
    mod.playPhaseSound();
    await settle();
    expect(played).toEqual([]);
    // Nothing is even fetched while the setting is off.
    expect(fetched).toEqual([]);
  });

  it('plays the chosen sound once switched on', async () => {
    const { mod, played, built } = await loadSound();
    mod.setSoundEnabled(true);
    mod.playPhaseSound();
    await settle();
    expect(played).toEqual(['/assets/sounds/chime.wav']);
    expect(built[0].currentTime).toBe(0); // rewound, so repeats aren't silent
  });

  it('reuses one element across transitions and rewinds it', async () => {
    const { mod, played, built } = await loadSound();
    mod.setSoundEnabled(true);
    mod.playPhaseSound();
    await settle();
    mod.playPhaseSound();
    await settle();
    expect(played).toHaveLength(2);
    expect(built).toHaveLength(1);
  });

  it('remembers the choice and switches element when it changes', async () => {
    const two = [
      { id: 'chime', name: 'Soft chime', file: 'chime.wav' },
      { id: 'bell', name: 'Bell', file: 'bell.wav' },
    ];
    const { mod, played } = await loadSound({ manifest: two });
    mod.setSoundEnabled(true);
    mod.setSoundChoice('bell');
    expect(mod.soundChoice()).toBe('bell');
    mod.playPhaseSound();
    await settle();
    expect(played).toEqual(['/assets/sounds/bell.wav']);
  });

  it('falls back to the first sound when the stored one is gone', async () => {
    localStorage.setItem('junkie:phaseSoundId', 'retired');
    const { mod, played } = await loadSound();
    mod.setSoundEnabled(true);
    mod.playPhaseSound();
    await settle();
    expect(played).toEqual(['/assets/sounds/chime.wav']);
  });

  it('stays silent, and retries later, when the manifest cannot be fetched', async () => {
    const { mod, played, fetched } = await loadSound({ manifest: 'error' });
    mod.setSoundEnabled(true);
    mod.playPhaseSound();
    await settle();
    expect(played).toEqual([]);
    const before = fetched.length;
    mod.playPhaseSound();
    await settle();
    expect(fetched.length).toBeGreaterThan(before);
  });

  it('previews regardless of the setting, for the Test button', async () => {
    const { mod, played } = await loadSound();
    mod.previewSound();
    await settle();
    expect(played).toEqual(['/assets/sounds/chime.wav']);
  });

  it('survives a browser that refuses localStorage', async () => {
    const { mod } = await loadSound();
    const spy = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    expect(mod.soundEnabled()).toBe(false);
    expect(mod.soundChoice()).toBe('chime');
    spy.mockRestore();
  });
});
