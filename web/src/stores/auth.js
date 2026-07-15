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
        }
      } catch {
        this.user = null;
      } finally {
        this.loaded = true;
      }
    },
  },
});
