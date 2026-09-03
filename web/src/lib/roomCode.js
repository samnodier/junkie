// Formatting for the "Code or link" field.
//
// Codes are minted as two groups of six hex characters (see randomCode in
// main.go), and the dash between them is part of the code, not decoration —
// so rather than making people remember to type it, put it in for them once
// the first group is full, the way a phone-number field does.
//
// Two things this must not break. Older rooms were minted at four characters
// per group and still work, so a dash the visitor types themselves is left
// exactly where they put it. And the field accepts a pasted link as well as a
// code, so a URL is reduced to its last path segment instead of having its
// punctuation stripped into nonsense.
const GROUP = 6;

export function formatRoomCode(raw) {
  let value = String(raw || '');
  // Prefer the segment a junkie link puts the code in, so both the room link
  // and the overlay link (which has /embed after the code) reduce correctly;
  // fall back to the last segment for anything else that looks like a path.
  const marked = value.match(/\/[fr]\/([^/?#]+)/i);
  if (marked) value = marked[1];
  else if (value.includes('/')) value = value.slice(value.lastIndexOf('/') + 1);
  value = value.split(/[?#]/)[0];
  value = value.toUpperCase().replace(/[^0-9A-Z-]/g, '');

  const dash = value.indexOf('-');
  if (dash >= 0) {
    // Their grouping wins; anything after the first dash is one group.
    const tail = value.slice(dash + 1).replace(/-/g, '');
    return (value.slice(0, dash + 1) + tail).slice(0, dash + 1 + GROUP);
  }
  // Only once there's something to put after it, so backspacing back through
  // the dash doesn't hit a character that immediately reappears.
  if (value.length > GROUP) {
    value = `${value.slice(0, GROUP)}-${value.slice(GROUP)}`;
  }
  return value.slice(0, GROUP * 2 + 1);
}
