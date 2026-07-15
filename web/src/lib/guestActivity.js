// Guest work-map builder, ported from guest.js. Reads the same
// junkie:activity localStorage key and produces the same heatmap shape as
// the server's /api/... responses (date labels via toLocaleDateString, like
// the legacy guest renderer).

const ACTIVITY_KEY = 'junkie:activity';

export function heatLevel(minutes) {
  if (minutes >= 180) return 4;
  if (minutes >= 90) return 3;
  if (minutes >= 30) return 2;
  if (minutes > 0) return 1;
  return 0;
}

export const dateKey = (date) => date.toISOString().slice(0, 10);

export function loadActivity() {
  try {
    const raw = localStorage.getItem(ACTIVITY_KEY);
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

export function recordFocus(minutes) {
  const activity = loadActivity();
  const key = dateKey(new Date());
  activity[key] = (activity[key] || 0) + minutes;
  localStorage.setItem(ACTIVITY_KEY, JSON.stringify(activity));
}

export function buildGuestHeatmap() {
  const activity = loadActivity();
  const today = new Date();
  today.setHours(0, 0, 0, 0);

  const days = [];
  const dates = [];
  for (let i = 364; i >= 0; i--) {
    const d = new Date(today);
    d.setDate(today.getDate() - i);
    dates.push(d);
    const minutes = activity[dateKey(d)] || 0;
    days.push({
      date: d,
      label: d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
      minutes,
      level: heatLevel(minutes),
    });
  }

  const firstDate = dates[0];
  const lastDate = dates[dates.length - 1];
  const gridStart = new Date(firstDate);
  gridStart.setDate(firstDate.getDate() - firstDate.getDay());

  let totalMinutes = 0;
  for (const day of days) totalMinutes += day.minutes;

  const dayByKey = {};
  for (const day of days) dayByKey[dateKey(day.date)] = day;

  const totalDays = Math.floor((lastDate - gridStart) / 86400000) + 1;
  const weeks = Math.ceil(totalDays / 7);
  const cells = Array.from({ length: weeks * 7 }, () => ({ empty: true }));

  for (let week = 0; week < weeks; week++) {
    for (let dow = 0; dow < 7; dow++) {
      const d = new Date(gridStart);
      d.setDate(gridStart.getDate() + week * 7 + dow);
      if (d < firstDate || d > lastDate) continue;
      const idx = week * 7 + dow;
      const key = dateKey(d);
      const day = dayByKey[key];
      cells[idx] = day
        ? { date: day.label, minutes: day.minutes, level: day.level }
        : {
            date: d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
            minutes: 0,
            level: 0,
          };
    }
  }

  const months = [];
  const labeled = {};
  for (let week = 0; week < weeks; week++) {
    for (let dow = 0; dow < 7; dow++) {
      const d = new Date(gridStart);
      d.setDate(gridStart.getDate() + week * 7 + dow);
      if (d < firstDate || d > lastDate) continue;
      if (d.getDate() === 1) {
        const key = d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0');
        if (!labeled[key]) {
          months.push({ label: d.toLocaleDateString(undefined, { month: 'short' }), col: week });
          labeled[key] = true;
        }
        break;
      }
    }
  }

  return { cells, months, weeks, totalMinutes };
}
