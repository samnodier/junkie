import { describe, it, expect } from 'vitest';
import { formatRoomCode } from './roomCode';

describe('room code formatting', () => {
  it('adds the dash once the first group is full', () => {
    expect(formatRoomCode('8C4E8A')).toBe('8C4E8A');
    expect(formatRoomCode('8C4E8A3')).toBe('8C4E8A-3');
    expect(formatRoomCode('8C4E8A3ACB8B')).toBe('8C4E8A-3ACB8B');
  });

  it('waits for a character to follow it, so backspacing gets past the dash', () => {
    // Deleting the last character of "8C4E8A-3" leaves the six that fit in
    // the first group, and the dash must not spring back.
    expect(formatRoomCode('8C4E8A-')).toBe('8C4E8A-');
    expect(formatRoomCode('8C4E8A')).toBe('8C4E8A');
  });

  it('leaves a dash the visitor typed where they put it', () => {
    // Rooms minted at the old four-character group still work.
    expect(formatRoomCode('AB12-CD34')).toBe('AB12-CD34');
    expect(formatRoomCode('AB12-')).toBe('AB12-');
  });

  it('uppercases and drops characters a code cannot contain', () => {
    expect(formatRoomCode('8c4e8a3acb8b')).toBe('8C4E8A-3ACB8B');
    expect(formatRoomCode('8C 4E:8A')).toBe('8C4E8A');
  });

  it('reduces a pasted link to its code', () => {
    expect(formatRoomCode('https://junkie-blin.onrender.com/f/8C4E8A-3ACB8B')).toBe('8C4E8A-3ACB8B');
    expect(formatRoomCode('https://junkie-blin.onrender.com/r/8C4E8A-3ACB8B?x=1')).toBe('8C4E8A-3ACB8B');
    // The overlay link has /embed after the code, and a viewer reading it off
    // a stream may well paste the whole thing.
    expect(formatRoomCode('junkie-blin.onrender.com/f/8C4E8A-3ACB8B/embed')).toBe('8C4E8A-3ACB8B');
    expect(formatRoomCode('http://localhost:8899/f/8C4E8A-3ACB8B')).toBe('8C4E8A-3ACB8B');
  });

  it('keeps only one dash and caps the length', () => {
    expect(formatRoomCode('8C4E8A-3ACB-8B')).toBe('8C4E8A-3ACB8B');
    expect(formatRoomCode('8C4E8A3ACB8BZZZZ')).toBe('8C4E8A-3ACB8B');
  });

  it('handles an empty field', () => {
    expect(formatRoomCode('')).toBe('');
    expect(formatRoomCode(null)).toBe('');
  });
});
