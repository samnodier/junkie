import { defineStore } from 'pinia';
import { connectSignals } from '@/lib/ws';
import { useToastStore } from '@/stores/toasts';
import { postForm } from '@/lib/postForm';
import { onRoomInvite, onBreakInvite, onTimerEnd, syncTimerNotification } from '@/lib/notify';
import { isWatching } from '@/lib/presence';

// Room page state, fed by /api/room/{code}. Same mutation pattern as the
// desk: post to the legacy endpoints via postForm (rejections surface as
// toasts), then re-fetch; the room socket's signals re-fetch too, replacing
// the legacy reload/fragment sync.

export const useRoomStore = defineStore('room', {
  state: () => ({
    code: '',
    loaded: false,
    deleted: false,
    invite: null, // {name, code} when the viewer isn't a member
    room: null,
    isCreator: false,
    viewerAdmin: false,
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
        this.viewerAdmin = Boolean(data.viewerAdmin);
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

        // Keep the ongoing notification in step with the phase, but only for
        // a run this viewer actually joined — watching a room you're not in
        // shouldn't put a timer in your notification shade.
        syncTimerNotification('room', this.timer?.participant ? this.timer : null, {
          label: this.room?.name || '',
          url: `/r/${encodeURIComponent(this.code)}`,
        });
      } catch {
        /* next signal or periodic tick retries */
      }
    },

    async action(name, fields = {}) {
      const ok = await postForm(`/r/${encodeURIComponent(this.code)}/${name}`, fields);
      await this.refresh();
      return ok;
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

    async open(code, meId) {
      this.close();
      this.$patch({ code, loaded: false, deleted: false, invite: null, lastPhase: '' });
      const toasts = useToastStore();

      // Fetch first: the room socket 403s non-members, so opening it before
      // membership is known leaves the invite screen in a reconnect loop.
      await this.refresh();
      if (this.invite || this.deleted) return;

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
        if (type === 'timer-checkin-kick') {
          if (event?.userIds?.includes(meId)) {
            toasts.show("You're out of this block — you didn't check in during the break. Join again to come back at the next one.");
          }
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
      // Safety net behind the socket. Gated on isWatching rather than the
      // tab's visibility: with the timer popped out the tab is hidden while
      // the user is still watching, and that's the worst moment to stop
      // re-fetching (see lib/presence.js).
      this.refreshTimer = setInterval(() => {
        if (isWatching()) this.refresh();
      }, 45000);
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
