import { defineStore } from 'pinia';
import { connectSignals } from '@/lib/ws';
import { useToastStore } from '@/stores/toasts';
import { onRoomInvite, onBreakInvite, onTimerEnd } from '@/lib/notify';

// Room page state, fed by /api/room/{code}. Same mutation pattern as the
// desk: post to the legacy endpoints, then re-fetch; the room socket's
// signals re-fetch too, replacing the legacy reload/fragment sync.
async function postForm(url, fields = {}) {
  const body = new URLSearchParams(fields);
  try {
    await fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body,
    });
  } catch {
    /* the follow-up refresh shows whatever state the server has */
  }
}

export const useRoomStore = defineStore('room', {
  state: () => ({
    code: '',
    loaded: false,
    deleted: false,
    invite: null, // {name, code} when the viewer isn't a member
    room: null,
    isCreator: false,
    memberCount: 0,
    timer: null,
    waiting: false,
    waiters: [], // ephemeral rooms: who's parked before a run exists
    mine: [],
    others: [],
    joinPrompt: null,
    socket: null,
    refreshTimer: null,
    lastPhase: '',
  }),
  actions: {
    async refresh() {
      if (!this.code) return;
      try {
        const res = await fetch(`/api/room/${encodeURIComponent(this.code)}`, {
          credentials: 'same-origin',
        });
        if (res.status === 404) {
          this.deleted = true;
          return;
        }
        if (!res.ok) return;
        const data = await res.json();
        if (data.invite) {
          this.invite = data.invite;
          this.loaded = true;
          return;
        }
        this.invite = null;
        this.room = data.room;
        this.isCreator = data.isCreator;
        this.memberCount = data.memberCount;
        this.timer = data.timer;
        this.waiting = data.waiting;
        this.waiters = data.waiters || [];
        this.mine = data.mine || [];
        this.others = data.others || [];
        this.loaded = true;

        // End-of-phase notifications on observed transitions, matching the
        // legacy sessionStorage phase tracker.
        const phase = this.timer?.phase || 'idle';
        if (this.lastPhase && this.lastPhase !== phase) {
          if (this.lastPhase === 'focus' && (phase === 'break' || phase === 'idle')) onTimerEnd('focus');
          else if (this.lastPhase === 'break' && phase === 'focus') onTimerEnd('break');
        }
        this.lastPhase = phase;
      } catch {
        /* next signal or periodic tick retries */
      }
    },

    async action(name, fields = {}) {
      await postForm(`/r/${encodeURIComponent(this.code)}/${name}`, fields);
      await this.refresh();
    },
    // enter is the click-the-link join for a temporary /f/{code} room: it makes
    // the viewer a member and queues them into the run before the first fetch.
    async enter(code) {
      await postForm(`/f/${encodeURIComponent(code)}/join`);
    },
    async addTodo(text) {
      await this.action('todos', { text });
    },
    async todoAction(id, action) {
      await postForm(`/todo/${encodeURIComponent(id)}/${action}`);
      await this.refresh();
    },
    async editTodo(id, text, patch = true) {
      await postForm(`/todo/${encodeURIComponent(id)}/edit`, { text });
      if (patch) await this.refresh();
    },

    open(code, meId) {
      this.close();
      this.$patch({ code, loaded: false, deleted: false, invite: null, lastPhase: '' });
      const toasts = useToastStore();

      this.socket = connectSignals(`/ws/r/${encodeURIComponent(code)}`, (type, event) => {
        if (type === 'deleted') {
          this.deleted = true;
          return;
        }
        if (type === 'todo-done') {
          if (event?.actorId !== meId) {
            const text = String(event?.text || '');
            const short = text.length > 60 ? text.slice(0, 57) + '…' : text;
            toasts.show(`${event?.actor || 'Someone'} completed: ${short}`);
          }
          this.refresh();
          return;
        }
        if (type === 'timer-lobby') {
          // In focus mode there is no prompt (you're already in), same as
          // the legacy room-focus-shell guard.
          const inFocus = this.timer?.phase === 'focus' && this.timer?.participant;
          if (event?.starterUserId !== meId && !inFocus) {
            this.joinPrompt = {
              code,
              roomName: event?.roomName || this.room?.name || '',
              starterName: event?.starterName || 'A room member',
              lobbyDeadline: event?.lobbyDeadline || '',
            };
            onRoomInvite(this.joinPrompt.starterName, this.joinPrompt.roomName);
          }
          this.refresh();
          return;
        }
        if (type === 'timer-break-invite') {
          if (!this.timer?.participant) onBreakInvite(event?.roomName || this.room?.name || '');
          this.refresh();
          return;
        }
        this.refresh();
      });

      this._onVisible = () => {
        if (document.visibilityState === 'visible') this.refresh();
      };
      document.addEventListener('visibilitychange', this._onVisible);
      window.addEventListener('focus', this._onVisible);
      window.addEventListener('online', this._onVisible);
      this.refreshTimer = setInterval(() => {
        if (document.visibilityState === 'visible') this.refresh();
      }, 45000);

      return this.refresh();
    },
    close() {
      this.socket?.close();
      this.socket = null;
      if (this.refreshTimer) clearInterval(this.refreshTimer);
      this.refreshTimer = null;
      if (this._onVisible) {
        document.removeEventListener('visibilitychange', this._onVisible);
        window.removeEventListener('focus', this._onVisible);
        window.removeEventListener('online', this._onVisible);
        this._onVisible = null;
      }
    },
  },
});
