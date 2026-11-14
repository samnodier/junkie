import { describe, it, expect } from 'vitest';
import { WAVES, waveLabel, waveMessage } from './waves.js';

describe('waves', () => {
  it('labels every wave and nothing else', () => {
    for (const wave of WAVES) expect(waveLabel(wave.kind)).toContain(wave.text);
    expect(waveLabel('free text')).toBe('');
  });

  it('names the sender, or You for your own', () => {
    expect(waveMessage({ kind: 'hi', actorId: 'a', actor: 'Eliz' }, 'me')).toBe('Eliz: 👋 Hi!');
    expect(waveMessage({ kind: 'hi', actorId: 'me', actor: 'Sam' }, 'me')).toBe('You: 👋 Hi!');
    expect(waveMessage({ kind: 'luck', actorId: 'a', actor: 'Eliz' }, 'me', 'Study')).toBe('Eliz in Study: 🍀 Good luck');
  });

  it('drops an unknown kind rather than showing it', () => {
    expect(waveMessage({ kind: '<b>', actorId: 'a', actor: 'x' }, 'me')).toBe('');
  });
});
