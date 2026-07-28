import { defineStore } from 'pinia';

// Session state for the SPA, fed by /api/me. `loaded` distinguishes "guest"
// from "not fetched yet" so guards and the shell never flash the wrong UI.
export const useAuthStore = defineStore('auth', {
  state: () => ({ user: null, loaded: false }),
  getters: {
    isAuthed: (s) => !!s.user,
    initial: (s) => {
      const name = s.user?.displayName || '';
      return name ? name[0].toUpperCase() : '?';
    },
    avatarURL: (s) =>
      s.user?.hasAvatar ? `/avatar/${s.user.id}?v=${s.user.avatarVersion}` : '',
  },
  actions: {
    async load() {
      try {
        const res = await fetch('/api/me', { credentials: 'same-origin' });
        if (res.ok) {
          const data = await res.json();
          this.user = data.user;
          if (this.user) this.reportTimezone(data.timezone || '');
        }
      } catch {
        this.user = null;
      } finally {
        this.loaded = true;
      }
    },

    // Focus minutes are credited to a calendar day, and the server has no way
    // to know which day that is for you — it runs in UTC. The browser already
    // knows its own IANA zone, so report it rather than asking anyone: no
    // prompt, no stored personal detail beyond a region name. Only posts when
    // the server's copy actually differs, so travelling updates it and a
    // normal page load costs nothing.
    reportTimezone(known) {
      let tz = '';
      try {
        tz = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
      } catch {
        return; // no Intl support: the server keeps reading this account as UTC
      }
      if (!tz || tz === known) return;
      fetch('/api/timezone', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: `timezone=${encodeURIComponent(tz)}`,
      }).catch(() => {});
    },
  },
});
