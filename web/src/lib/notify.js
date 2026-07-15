// Foreground notifications, ported from notifications.js (same keys, same
// copy). The SPA pages don't load the legacy script, so this is the one
// implementation for ported screens.
const ROOM_INVITE_KEY = 'junkie:roomInviteNotifications';
const ICON = '/assets/icon.svg';

const supported = () => typeof window !== 'undefined' && 'Notification' in window;

export function requestPermission() {
  if (!supported() || Notification.permission !== 'default') return;
  try {
    Notification.requestPermission().catch(() => {});
  } catch {
    /* older Safari throws on the promise form */
  }
}

export function notify(title, body, onClick) {
  if (!supported() || Notification.permission !== 'granted') return;
  try {
    const n = new Notification(title, { body: body || '', icon: ICON });
    n.onclick = () => {
      window.focus();
      if (typeof onClick === 'function') onClick();
      n.close();
    };
  } catch {
    /* notification constructors can throw on some mobile browsers */
  }
}

export function onTimerEnd(phase) {
  if (phase === 'break') {
    notify("Break's over", 'Ready for your next focus block');
  } else {
    notify('Focus session complete', 'Time for a break');
  }
}

export function roomInvitesEnabled() {
  return localStorage.getItem(ROOM_INVITE_KEY) !== 'disabled';
}

export function onRoomInvite(starterName, roomName, onClick) {
  if (!roomInvitesEnabled() || (document.visibilityState === 'visible' && document.hasFocus())) return;
  notify(
    starterName + ' is starting a focus block',
    'Join ' + roomName + ' before the lobby closes.',
    onClick
  );
}

export function onBreakInvite(roomName, onClick) {
  if (!roomInvitesEnabled() || (document.visibilityState === 'visible' && document.hasFocus())) return;
  notify(
    'Break time in ' + roomName,
    'Join now to be included in the next focus block.',
    onClick
  );
}
