<script setup>
// The desk (/ and /dashboard). The Go handler only serves the SPA here for
// guests until the logged-in desk is ported; if a signed-in session ever
// lands here early, fall back to a plain reload so the server can decide.
import { onUnmounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import AppShell from '@/components/AppShell.vue';
import GuestDesk from '@/components/GuestDesk.vue';
import DeskLoggedIn from '@/components/DeskLoggedIn.vue';
import { useDeskStore } from '@/stores/desk';

const deskStore = useDeskStore();

const route = useRoute();
const auth = useAuthStore();
const error = String(route.query.error || '');
const dismissed = ref(false);

// The banner reports something that already happened -- a room that was
// deleted, a join that didn't take -- so it has nothing to wait for. It
// clears itself rather than sitting there until someone dismisses a message
// they read ten minutes ago.
const bannerLifetime = 8000;
let bannerTimer = null;
if (error) {
  bannerTimer = setTimeout(() => {
    dismissed.value = true;
  }, bannerLifetime);
}
onUnmounted(() => clearTimeout(bannerTimer));
</script>

<template>
  <AppShell show-menu next="/" :rooms="deskStore.rooms" :current-room-code="''">
    <p v-if="error && !dismissed" class="context-banner context-banner-dismiss banner-timed" role="status">
      {{ error }} <button type="button" class="banner-dismiss" aria-label="Dismiss" @click="dismissed = true">×</button>
    </p>
    <GuestDesk v-if="!auth.isAuthed" />
    <DeskLoggedIn v-else />
  </AppShell>
</template>
