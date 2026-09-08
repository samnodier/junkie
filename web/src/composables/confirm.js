import { ref } from 'vue';

// One confirmation dialog for the whole app.
//
// Browser confirm() is modal to the tab, unstyled, and says "localhost:8080
// says" above whatever you wrote -- which reads as the browser asking, not
// the app. This keeps the same shape at the call site: await it, and carry on
// only if it resolved true.
//
// The state lives at module scope and AppShell renders the single dialog, so
// no page has to mount its own.
export const confirmState = ref(null);

let resolvePending = null;

// askConfirm({ ... }) resolves true when the person confirms, false otherwise.
// A second call while one is open resolves the first as cancelled rather than
// stacking dialogs.
export function askConfirm({ title, body = '', confirmLabel = 'Confirm', danger = false }) {
  if (resolvePending) {
    resolvePending(false);
    resolvePending = null;
  }
  confirmState.value = { title, body, confirmLabel, danger };
  return new Promise((resolve) => {
    resolvePending = resolve;
  });
}

export function settleConfirm(answer) {
  confirmState.value = null;
  if (resolvePending) {
    resolvePending(answer);
    resolvePending = null;
  }
}
