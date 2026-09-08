import { describe, it, expect } from 'vitest';
import { shortFileName } from './fileName';

describe('sound filename shortening', () => {
  it('cuts a long name to its first ten characters', () => {
    expect(shortFileName('501417_visionear__aachen_burning-fireplace.wav')).toBe('501417_vis…');
  });

  it('leaves a name that already fits alone', () => {
    expect(shortFileName('chime.mp3')).toBe('chime.mp3');
  });

  it('has nothing to say about a missing name', () => {
    expect(shortFileName('')).toBe('');
    expect(shortFileName(null)).toBe('');
  });
});
