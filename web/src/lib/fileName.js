// Shortening for a chosen sound's filename.
//
// Uploads arrive named by whatever stock library they came from --
// "501417_visionear__aachen_burning-fireplace-crackling-fire-sounds.wav" is a
// real one -- and a name that long stretches the button holding it until the
// text beside it is squeezed into a single-word column. The name here is only
// ever a reminder of which file is set, so the opening characters are enough
// to tell two of them apart; the full name stays in the title attribute.
const KEEP = 10;

export function shortFileName(name) {
  const value = String(name || '');
  return value.length > KEEP ? `${value.slice(0, KEEP)}…` : value;
}
