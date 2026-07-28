import { defineStore } from 'pinia';
import { recordFocus } from '@/lib/guestActivity';
import { onTimerEnd, syncTimerNotification } from '@/lib/notify';

// Guest desk state, ported from guest.js. Same localStorage keys and timer
// state machine (focus -> break_offer -> break -> idle), so existing guest
// data keeps working unchanged.
const TODOS_KEY = 'junkie:todos';
const TIMER_KEY = 'junkie:soloTimer';

const load = (key, fallback) => {
  try {
    const raw = localStorage.getItem(key);
    if (raw === null) return fallback;
    return JSON.parse(raw);
  } catch {
    return fallback;
  }
};
const save = (key, value) =>
  value === null
    ? localStorage.removeItem(key)
    : localStorage.setItem(key, JSON.stringify(value));

const uid = () => Math.random().toString(36).slice(2, 10);

const DAY_MS = 24 * 60 * 60 * 1000;

// Done/removed todos carry the moment they went inactive; active todos don't.
function stampInactive(t) {
  if (t.done || t.removed) t.inactiveAt = new Date().toISOString();
  else delete t.inactiveAt;
  return t;
}

// Same policy as the server sweep: anything done or removed for over 24
// hours is deleted. Pre-policy todos without a stamp get one now, so they
// keep a full day of grace instead of vanishing on first load.
function pruneInactive(todos) {
  let changed = false;
  const now = Date.now();
  const kept = [];
  for (const t of todos) {
    if (!(t.done || t.removed)) {
      kept.push(t);
      continue;
    }
    if (!t.inactiveAt) {
      kept.push(stampInactive({ ...t }));
      changed = true;
      continue;
    }
    if (now - new Date(t.inactiveAt).getTime() > DAY_MS) {
      changed = true;
      continue;
    }
    kept.push(t);
  }
  return { todos: kept, changed };
}

export const clampMinutes = (n) => Math.min(180, Math.max(5, n));

export const breakMinutesForFocus = (focusMinutes) => {
  if (focusMinutes < 30) return 5;
  if (focusMinutes < 120) return 10;
  if (focusMinutes < 180) return 20;
  return 30;
};

export const useGuestDeskStore = defineStore('guestDesk', {
  state: () => ({
    todos: load(TODOS_KEY, []),
    timer: load(TIMER_KEY, null),
  }),
  getters: {
    // Active first, then removed — same ordering as guest.js sortTodos.
    sorted: (s) => {
      const active = [];
      const removed = [];
      for (const t of s.todos) (t.removed ? removed : active).push(t);
      return active.concat(removed);
    },
    phase: (s) => s.timer?.phase || 'idle',
  },
  actions: {
    persistTodos() {
      save(TODOS_KEY, this.todos);
    },
    persistTimer() {
      save(TIMER_KEY, this.timer);
    },
    addTodo(text) {
      const trimmed = text.trim();
      if (!trimmed) return false;
      this.todos.unshift({ id: uid(), text: trimmed, done: false, removed: false });
      this.persistTodos();
      return true;
    },
    // Any state flip restamps inactiveAt (mirrors the server, where every
    // flip touches updated_at and restarts the 24h deletion clock).
    toggleTodo(id) {
      this.todos = this.todos.map((t) => (t.id === id ? stampInactive({ ...t, done: !t.done }) : t));
      this.persistTodos();
    },
    removeTodo(id) {
      this.todos = this.todos.map((t) => (t.id === id ? stampInactive({ ...t, removed: true }) : t));
      this.persistTodos();
    },
    restoreTodo(id) {
      this.todos = this.todos.map((t) => (t.id === id ? stampInactive({ ...t, removed: false }) : t));
      this.persistTodos();
    },
    deleteTodo(id) {
      this.todos = this.todos.filter((t) => t.id !== id);
      this.persistTodos();
    },

    startFocus(minutes) {
      const focusMinutes = clampMinutes(Number(minutes) || 50);
      this.timer = {
        phase: 'focus',
        focusMinutes,
        endsAt: new Date(Date.now() + focusMinutes * 60 * 1000).toISOString(),
      };
      this.persistTimer();
    },
    cancelFocus() {
      this.timer = null;
      this.persistTimer();
    },
    startBreak() {
      if (this.timer?.phase !== 'break_offer') return;
      this.timer = {
        ...this.timer,
        phase: 'break',
        endsAt: new Date(Date.now() + this.timer.breakMinutes * 60 * 1000).toISOString(),
      };
      this.persistTimer();
    },
    skipBreak() {
      const focusMinutes = this.timer?.focusMinutes || 50;
      this.startFocus(focusMinutes);
    },

    // The tick calls this every second; syncTimerNotification is keyed on the
    // rendered text, so it only reposts when the phase actually turns over.
    normalize(live = false) {
      this.advance(live);
      syncTimerNotification('guest', this.timer);
    },

    // Advance expired phases, exactly like guest.js normalizeTimer: an
    // elapsed focus records its minutes once and becomes a break offer; an
    // elapsed break clears the timer. `live` notifies only for transitions
    // observed by the running tick, matching legacy (no stale notification
    // when the page is opened long after a phase already ended).
    advance(live) {
      // Re-read storage first, like guest.js did on every tick: another tab
      // (or anything else) may have changed the timer or todos.
      this.timer = load(TIMER_KEY, null);
      const pruned = pruneInactive(load(TODOS_KEY, []));
      this.todos = pruned.todos;
      if (pruned.changed) this.persistTodos();
      let t = this.timer;
      if (!t) return;
      if (!t.phase) t = { ...t, phase: 'focus' };
      if (t.phase === 'focus') {
        if (Date.now() >= new Date(t.endsAt).getTime()) {
          if (!t.focusRecorded) {
            recordFocus(t.focusMinutes);
            t.focusRecorded = true;
          }
          t = { ...t, phase: 'break_offer', breakMinutes: breakMinutesForFocus(t.focusMinutes) };
          delete t.endsAt;
          this.timer = t;
          this.persistTimer();
          if (live) onTimerEnd('focus');
        }
        return;
      }
      if (t.phase === 'break_offer') return;
      if (t.phase === 'break') {
        if (Date.now() >= new Date(t.endsAt).getTime()) {
          this.timer = null;
          this.persistTimer();
          if (live) onTimerEnd('break');
        }
        return;
      }
      this.timer = null;
      this.persistTimer();
    },
  },
});
