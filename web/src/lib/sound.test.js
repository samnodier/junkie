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
      this.pause = vi.fn();
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

// A temporary room's chime is the block's, not the viewer's: it plays for
// everyone who joined, whatever their own switch says. Sam's call -- you
// asked for it by joining.
describe('a temporary room plays for everyone', () => {
  beforeEach(() => localStorage.clear());

  it('plays with the switch off, when forced', async () => {
    const { mod, played } = await loadSound();
    expect(mod.soundEnabled()).toBe(false);
    mod.playPhaseSound({ forced: true });
    await settle();
    await settle();
    expect(played).toEqual(['/assets/sounds/chime.wav']);
  });

  it('still stays silent with the switch off when not forced', async () => {
    const { mod, played } = await loadSound();
    mod.playPhaseSound();
    mod.playPhaseSound({ forced: false });
    await settle();
    expect(played).toEqual([]);
  });

  it("prefers the room's own sound, then the default, over this browser's pick", async () => {
    const { mod, played } = await loadSound({
      manifest: [
        { id: 'chime', name: 'Soft chime', file: 'chime.wav' },
        { id: 'bell', name: 'Bell', file: 'bell.wav' },
        { id: 'room:sound', name: 'Room', url: '/r/ABC/sound' },
      ],
    });
    // This browser chose the bell for its private timer. A temporary room
    // does not care: it plays what the room uploaded.
    mod.setSoundChoice('bell');
    mod.playPhaseSound({ forced: true });
    await settle();
    await settle();
    expect(played).toEqual(['/r/ABC/sound']);
  });

  it('falls back to the default when the room has no sound of its own', async () => {
    const { mod, played } = await loadSound({
      manifest: [
        { id: 'chime', name: 'Soft chime', file: 'chime.wav' },
        { id: 'bell', name: 'Bell', file: 'bell.wav' },
      ],
    });
    mod.setSoundChoice('bell');
    mod.playPhaseSound({ forced: true });
    await settle();
    await settle();
    expect(played).toEqual(['/assets/sounds/chime.wav']);
  });

  it('spends the first tap on a silent play, so the real chime is allowed later', async () => {
    const { mod, built, played } = await loadSound();
    const stop = mod.unlockSoundOnFirstGesture();
    expect(built).toHaveLength(0);
    document.dispatchEvent(new Event('pointerdown'));
    await settle();
    await settle();
    // one element, played once while muted, then rewound and unmuted
    expect(played).toEqual(['/assets/sounds/chime.wav']);
    expect(built[0].muted).toBe(false);
    expect(built[0].currentTime).toBe(0);
    // and the listener is gone: a second tap does not play again
    document.dispatchEvent(new Event('pointerdown'));
    await settle();
    expect(played).toHaveLength(1);
    stop();
  });
});
