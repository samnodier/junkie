(function () {
  const ICON = '/assets/icon.svg';

  function supported() {
    return typeof window !== 'undefined' && 'Notification' in window;
  }

  function requestPermission() {
    if (!supported() || Notification.permission !== 'default') return;
    try {
      Notification.requestPermission().catch(function () {});
    } catch (_) {}
  }

  function notify(title, body) {
    if (!supported() || Notification.permission !== 'granted') return;
    try {
      const n = new Notification(title, { body: body || '', icon: ICON });
      n.onclick = function () {
        window.focus();
        n.close();
      };
    } catch (_) {}
  }

  function onTimerEnd(phase) {
    if (phase === 'break') {
      notify("Break's over", 'Ready for your next focus block');
    } else {
      notify('Focus session complete', 'Time for a break');
    }
  }

  window.junkieNotify = { requestPermission, notify, onTimerEnd };
})();
