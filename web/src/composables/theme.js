import { ref } from 'vue';

const KEY = 'junkie:theme';
// index.html's boot script has already stamped data-theme before Vue mounts.
const theme = ref(document.documentElement.getAttribute('data-theme') || 'dark');

export function useTheme() {
  const toggle = () => {
    theme.value = theme.value === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', theme.value);
    localStorage.setItem(KEY, theme.value);
  };
  return { theme, toggle };
}
