<script setup>
// Shared page chrome: topbar, optional menu drawer, toast stack. Mirrors the
// Go "shell" template so ported pages keep identical structure and classes.
import { ref } from 'vue';
import TopBar from './TopBar.vue';
import MenuDrawer from './MenuDrawer.vue';
import ToastHolder from './ToastHolder.vue';

defineProps({
  showMenu: { type: Boolean, default: false },
  rooms: { type: Array, default: () => [] },
  currentRoomCode: { type: String, default: '' },
  next: { type: String, default: '/' },
});

const drawerOpen = ref(false);
</script>

<template>
  <TopBar :show-menu="showMenu" @open-menu="drawerOpen = true" />
  <main class="page">
    <slot />
  </main>
  <MenuDrawer
    v-if="showMenu"
    :open="drawerOpen"
    :rooms="rooms"
    :current-room-code="currentRoomCode"
    :next="next"
    @close="drawerOpen = false"
  />
  <ToastHolder />
</template>
