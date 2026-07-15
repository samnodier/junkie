import { defineStore } from 'pinia';

let nextID = 1;

// Same behavior as the legacy window.junkieToast: stacked top-right, max 4,
// dismissible, self-expiring after 10s, optionally clickable.
export const useToastStore = defineStore('toasts', {
  state: () => ({ items: [] }),
  actions: {
    show(message, onClick) {
      const id = nextID++;
      this.items.push({ id, message, onClick: onClick || null, visible: false });
      while (this.items.length > 4) this.items.shift();
      // matches the legacy 20ms show / 10s expiry timings
      setTimeout(() => {
        const t = this.items.find((i) => i.id === id);
        if (t) t.visible = true;
      }, 20);
      setTimeout(() => this.dismiss(id), 10000);
    },
    dismiss(id) {
      const t = this.items.find((i) => i.id === id);
      if (!t) return;
      t.visible = false;
      setTimeout(() => {
        this.items = this.items.filter((i) => i.id !== id);
      }, 300);
    },
  },
});
