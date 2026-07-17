import { defineStore } from 'pinia';
import { connectSignals } from '@/lib/ws';
import { useToastStore } from '@/stores/toasts';
import { postForm } from '@/lib/postForm';
import { onRoomInvite, onBreakInvite } from '@/lib/notify';

// Signed-in desk state, fed by /api/desk. Mutations post to the legacy
// endpoints (which own all the rules) via postForm — rejections surface as
// toasts — and then refresh; WS signals from /ws/me and each room socket
// also refresh, replacing the legacy fragment-swap/location.reload() sync
// with reactive updates.

export const useDeskStore = defineStore('desk', {
  state: () => ({
    loaded: false,
    soloTimer: null,
    todos: [],
    rooms: [],
    // transient UI state driven by WS events
    joinPrompt: null, // {code, roomName, starterName, lobbyDeadline}
    sockets: [],
    refreshTimer: null,
  }),
  actions: {
    async refresh() {
      try {
        const res = await fetch('/api/desk', { credentials: 'same-origin' });
        if (!res.ok) return;
        const data = await res.json();
        this.soloTimer = data.soloTimer;
        this.todos = data.todos || [];
        this.rooms = data.rooms || [];
        this.loaded = true;
      } catch {
        /* next signal or periodic tick retries */
      }
    },

    // --- personal todos (legacy endpoints, then refresh) ---
    async addTodo(text) {
      await postForm('/todos', { text });
      await this.refresh();
    },
    async todoAction(id, action) {
      await postForm(`/todo/${encodeURIComponent(id)}/${action}`);
      await this.refresh();
    },
    async editTodo(id, text, patch = true) {
      await postForm(`/todo/${encodeURIComponent(id)}/edit`, { text });
      if (patch) await this.refresh();
    },
    async addRoomTodo(code, text) {
      await postForm(`/r/${encodeURIComponent(code)}/todos`, { text });
      await this.refresh();
    },

    // --- solo timer ---
    async soloStart(minutes) {
      await postForm('/solo/start', { focus_minutes: String(minutes) });
      await this.refresh();
    },
    async soloCancel() {
      await postForm('/solo/cancel');
      await this.refresh();
    },
    async soloBreakStart() {
      await postForm('/solo/break/start');
      await this.refresh();
    },
    async soloBreakSkip() {
      await postForm('/solo/break/skip');
      await this.refresh();
    },

    // --- room timer actions ---
    async roomTimer(code, action, fields = {}) {
      await postForm(`/r/${encodeURIComponent(code)}/${action}`, fields);
      await this.refresh();
    },

    // --- live sync ---
    connect(meId) {
      this.disconnect();
      const toasts = useToastStore();

      this.sockets.push(
        connectSignals('/ws/me', (type) => {
          if (type === 'todos' || type === 'solo-timer') this.refresh();
        })
      );

      for (const room of this.rooms) {
        const code = room.code;
        const name = room.name;
        this.sockets.push(
          connectSignals(`/ws/r/${encodeURIComponent(code)}`, (type, event) => {
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
              if (event?.starterUserId === meId) {
                this.refresh();
                return;
              }
              this.joinPrompt = {
                code: event?.roomCode || code,
                roomName: event?.roomName || name,
                starterName: event?.starterName || 'A room member',
                lobbyDeadline: event?.lobbyDeadline || '',
              };
              onRoomInvite(this.joinPrompt.starterName, this.joinPrompt.roomName);
              this.refresh();
              return;
            }
            if (type === 'timer-break-invite') {
              const timer = this.rooms.find((r) => r.code === code)?.timer;
              // Only members who never joined the ending block get invited.
              if (!timer?.participant) onBreakInvite(event?.roomName || name);
              this.refresh();
              return;
            }
            if (type === 'timer-checkin-kick') {
              if (event?.userIds?.includes(meId)) {
                toasts.show(`Dropped from ${name} — you didn't check in during the break.`);
              }
              this.refresh();
              return;
            }
            // "todos", "timer-phase", "deleted", or anything new: re-fetch.
            this.refresh();
          })
        );
      }

      // Reconcile on the same triggers the legacy desk used, minus the
      // disruptive reload: tab shown, window focus, back online, 45s tick.
      this._onVisible = () => {
        if (document.visibilityState === 'visible') this.refresh();
      };
      document.addEventListener('visibilitychange', this._onVisible);
      window.addEventListener('focus', this._onVisible);
      window.addEventListener('online', this._onVisible);
      this.refreshTimer = setInterval(() => {
        if (document.visibilityState === 'visible') this.refresh();
      }, 45000);
    },
    disconnect() {
      for (const s of this.sockets) s.close();
      this.sockets = [];
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
