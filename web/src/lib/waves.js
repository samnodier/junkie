// The whole vocabulary of a wave. Keys match roomWaves on the server, which
// only ever relays the key: the words shown are these, never something a
// member typed.
export const WAVES = [
  { kind: 'hi', emoji: '👋', text: 'Hi!' },
  { kind: 'luck', emoji: '🍀', text: 'Good luck' },
  { kind: 'go', emoji: '💪', text: "Let's go" },
  { kind: 'nice', emoji: '🎉', text: 'Nice work' },
  { kind: 'back', emoji: '☕', text: 'Back soon' },
];

export function waveLabel(kind) {
  const wave = WAVES.find((w) => w.kind === kind);
  return wave ? `${wave.emoji} ${wave.text}` : '';
}

// What a wave reads as in a toast, from the receiving side.
export function waveMessage(event, meId, roomName = '') {
  const label = waveLabel(event?.kind);
  if (!label) return '';
  const who = event?.actorId === meId ? 'You' : event?.actor || 'Someone';
  return roomName ? `${who} in ${roomName}: ${label}` : `${who}: ${label}`;
}
