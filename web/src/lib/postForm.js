import { useToastStore } from '@/stores/toasts';

// POST to a legacy form endpoint. Those endpoints answer with a redirect
// whose ?error= query carries the human-readable rejection (room caps,
// settings locked mid-run, check-in rules), which the SPA used to swallow —
// mutations that failed looked exactly like ones that worked. Surface it as
// a toast and report success so callers can skip pointless refreshes.
export async function postForm(url, fields = {}) {
  try {
    const res = await fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams(fields),
    });
    let error = '';
    if (res.redirected) {
      error = new URL(res.url, location.origin).searchParams.get('error') || '';
    }
    if (!error && !res.ok) {
      error = "That didn't go through — try again.";
    }
    if (error) {
      useToastStore().show(error);
      return false;
    }
    return true;
  } catch {
    useToastStore().show('Could not reach the server. Check your connection and try again.');
    return false;
  }
}
