// Reconnecting WebSocket client for junkie's signal protocol.
//
// The server sends either a bare string signal ("todos", "solo-timer",
// "deleted") or a JSON object with a `type` field ("todo-done",
// "timer-lobby", "timer-break-invite", timer phase updates). Handlers get
// (type, event) where event is null for bare signals.
export function connectSignals(path, onMessage) {
  let ws = null;
  let closed = false;
  let attempts = 0;

  const url = () => {
    const proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
    return proto + location.host + path;
  };

  const open = () => {
    if (closed) return;
    ws = new WebSocket(url());
    ws.onopen = () => {
      attempts = 0;
    };
    ws.onmessage = (event) => {
      const msg = String(event.data);
      let parsed = null;
      if (msg.startsWith('{')) {
        try {
          parsed = JSON.parse(msg);
        } catch {
          parsed = null;
        }
      }
      onMessage(parsed?.type || msg, parsed);
    };
    ws.onclose = () => {
      if (closed) return;
      attempts += 1;
      const delay = Math.min(15000, 500 * 2 ** Math.min(attempts, 5));
      setTimeout(open, delay);
    };
  };

  open();
  return {
    close() {
      closed = true;
      ws?.close();
    },
  };
}
