import { ref } from 'vue';

// Mint a fresh one-time connect link on the legacy endpoint and copy it.
// Each click replaces the previous link, so only the newest works.
export function useConnectLink() {
  const label = ref('Copy connect link');
  const flash = (text) => {
    label.value = text;
    setTimeout(() => {
      label.value = 'Copy connect link';
    }, 4000);
  };
  async function copy() {
    try {
      const resp = await fetch('/profile/connect-link', { method: 'POST', credentials: 'same-origin' });
      if (!resp.ok) throw new Error('request failed');
      const link = await resp.text();
      await navigator.clipboard.writeText(link);
      flash('Copied — works for one person');
    } catch {
      flash('Could not copy — try again');
    }
  }
  return { label, copy };
}
