import { useToastStore } from '@/stores/toasts';

// POST to a legacy form endpoint. Those endpoints were built for HTML forms:
// they redirect back to the page with any rejection (room caps, settings
// locked mid-run, check-in rules) in an ?error= query. The X-Junkie-Action
// header asks the server for that outcome as JSON instead, so a click costs
// one request rather than a post plus a download of the page it redirects
// to. Rejections surface as a toast; the return value reports success so
// callers can skip pointless refreshes.
export async function postForm(url, fields = {}) {
  try {
    const res = await fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded',
        'X-Junkie-Action': '1',
      },
      body: new URLSearchParams(fields),
    });
    let error = '';
    const type = res.headers?.get?.('Content-Type') || '';
    if (type.includes('application/json')) {
      const data = await res.json().catch(() => null);
      error = data?.error || '';
    } else if (res.redirected) {
      // A server from before the JSON reply (mid-deploy) still redirects.
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
