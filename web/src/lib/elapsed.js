// How a stopwatch's count reads: mm:ss, and h:mm:ss once it passes the hour.
// Minutes alone would read 61:00, which is harder to take in than 1:01:00.
export function formatElapsed(seconds) {
  const value = Math.max(0, Math.floor(seconds || 0));
  const s = String(value % 60).padStart(2, '0');
  if (value >= 3600) {
    const m = String(Math.floor((value % 3600) / 60)).padStart(2, '0');
    return `${Math.floor(value / 3600)}:${m}:${s}`;
  }
  return `${String(Math.floor(value / 60)).padStart(2, '0')}:${s}`;
}

// "6 hours", "1 hour", "90 minutes": a stretch length in words.
export function stretchWords(minutes) {
  if (minutes % 60 !== 0) return `${minutes} minutes`;
  const h = minutes / 60;
  return h === 1 ? '1 hour' : `${h} hours`;
}
