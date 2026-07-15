<script setup>
import { computed } from 'vue';
import { useAuthStore } from '@/stores/auth';
import { useTheme } from '@/composables/theme';

defineProps({
  showMenu: { type: Boolean, default: false },
});
const emit = defineEmits(['open-menu']);

const auth = useAuthStore();
const { theme, toggle } = useTheme();
const themeLabel = computed(() =>
  theme.value === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'
);
</script>

<template>
  <header class="topbar">
    <a class="brand" href="/" aria-label="junkie home">
      <span class="brand-mark">j</span>
      <span>junkie</span>
    </a>
    <div class="topbar-actions">
      <button
        type="button"
        class="theme-toggle"
        id="theme-toggle"
        :aria-label="themeLabel"
        :title="themeLabel"
        @click="toggle"
      >
        <span class="theme-icon theme-icon-dark" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg></span>
        <span class="theme-icon theme-icon-light" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/></svg></span>
      </button>
      <a
        v-if="showMenu && auth.isAuthed"
        href="/profile"
        class="topbar-user"
        :aria-label="`Profile — ${auth.user.displayName}`"
      >
        <span class="topbar-avatar" aria-hidden="true"><img v-if="auth.avatarURL" class="avatar-img" :src="auth.avatarURL" alt=""><template v-else>{{ auth.initial }}</template></span>
      </a>
      <button
        v-if="showMenu"
        type="button"
        class="menu-drawer-trigger"
        aria-label="Open menu"
        :aria-expanded="'false'"
        @click="emit('open-menu')"
      >
        <span class="menu-bar" aria-hidden="true"></span>
        <span class="menu-bar" aria-hidden="true"></span>
        <span class="menu-bar" aria-hidden="true"></span>
      </button>
    </div>
  </header>
</template>
