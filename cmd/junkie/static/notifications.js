(function () {
  const ICON = '/assets/icon.svg';
  const ROOM_INVITE_KEY = 'junkie:roomInviteNotifications';

  function supported() {
    return typeof window !== 'undefined' && 'Notification' in window;
  }

  function requestPermission() {
    if (!supported() || Notification.permission !== 'default') return;
    try {
      Notification.requestPermission().catch(function () {});
    } catch (_) {}
  }

  function notify(title, body, onClick) {
    if (!supported() || Notification.permission !== 'granted') return;
    try {
      const n = new Notification(title, { body: body || '', icon: ICON });
      n.onclick = function () {
        window.focus();
        if (typeof onClick === 'function') onClick();
        n.close();
      };
    } catch (_) {}
  }

  function roomInvitesEnabled() {
    return localStorage.getItem(ROOM_INVITE_KEY) !== 'disabled';
  }

  function setRoomInvitesEnabled(enabled) {
    localStorage.setItem(ROOM_INVITE_KEY, enabled ? 'enabled' : 'disabled');
    if (enabled) requestPermission();
  }

  function onRoomInvite(starterName, roomName, onClick) {
    if (!roomInvitesEnabled() || document.visibilityState === 'visible' && document.hasFocus()) return;
    notify(
      starterName + ' is starting a focus block',
      'Join ' + roomName + ' before the lobby closes.',
      onClick
    );
  }

  function onTimerEnd(phase) {
    if (phase === 'break') {
      notify("Break's over", 'Ready for your next focus block');
    } else {
      notify('Focus session complete', 'Time for a break');
    }
  }

  window.junkieNotify = {
    requestPermission,
    notify,
    onTimerEnd,
    onRoomInvite,
    roomInvitesEnabled,
    setRoomInvitesEnabled,
  };
})();
