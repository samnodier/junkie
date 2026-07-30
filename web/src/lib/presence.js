// "Is the user actually watching the timer right now?"
//
// The tab's own visibilityState used to be the whole answer, and the stores
// gate their safety-net poll on it. Once the timer can be popped out into a
// picture-in-picture window that stops being true: the opener tab is hidden
// *precisely* when the pop-out is doing its job, and that is the worst moment
// to stop re-fetching, because a socket that drops while the tab is hidden
// would otherwise leave a frozen countdown on screen until the user came back.

let pip = false;

export function setPipVisible(open) {
  pip = !!open;
}

export function isWatching() {
  if (pip) return true;
  return typeof document === 'undefined' || document.visibilityState === 'visible';
}
