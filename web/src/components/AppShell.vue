<script setup>
// Shared page chrome: topbar, optional menu drawer, toast stack. Mirrors the
// Go "shell" template so ported pages keep identical structure and classes.
import { provide, ref } from 'vue';
import TopBar from './TopBar.vue';
import MenuDrawer from './MenuDrawer.vue';
import ConfirmModal from './ConfirmModal.vue';
import ToastHolder from './ToastHolder.vue';

defineProps({
  showMenu: { type: Boolean, default: false },
  rooms: { type: Array, default: () => [] },
  currentRoomCode: { type: String, default: '' },
  next: { type: String, default: '/' },
});

const drawerOpen = ref(false);
const joinRequest = ref(0);

// Pages inside the shell can pop the drawer open on its join-room section
// ("Have a room code?" on the desk does this).
provide('shellApi', {
  openJoin() {
    drawerOpen.value = true;
    joinRequest.value += 1;
  },
});
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
    :join-request="joinRequest"
    @close="drawerOpen = false"
  />
  <ToastHolder />
  <ConfirmModal />
</template>
