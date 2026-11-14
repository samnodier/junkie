import { ref, watch } from 'vue';

// Which private mode this browser last used, so the tab you left it on is
// the tab you come back to. Per-browser like the chime preference: it is a
// habit of the person at this screen, not a fact about their account.
const KEY = 'junkie:soloMode';

function read() {
  try {
    return localStorage.getItem(KEY) === 'stopwatch' ? 'stopwatch' : 'timer';
  } catch {
    return 'timer';
  }
}

const mode = ref(read());
watch(mode, (value) => {
  try {
    localStorage.setItem(KEY, value);
  } catch {
    /* private window: the choice just doesn't outlive the tab */
  }
});

// One shared ref, so the desk card and the /solo screen agree.
export function useSoloMode() {
  return mode;
}
