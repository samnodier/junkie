<script setup>
// Room-invite confirmation (/join/confirm?code=...), ported from the
// "Join room" template branch. The join/cancel POST still goes to the legacy
// /join/confirm handler so redirects and membership rules stay server-owned.
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import ConfirmCard from '@/components/ConfirmCard.vue';

const route = useRoute();
const room = ref(null);

onMounted(async () => {
  const code = String(route.query.code || '');
  try {
    const res = await fetch(`/api/join-context?code=${encodeURIComponent(code)}`, {
      credentials: 'same-origin',
    });
    const data = await res.json();
    if (data.redirect) {
      location.href = data.redirect;
      return;
    }
    room.value = data.room;
  } catch {
    location.href = '/dashboard';
  }
});
</script>

<template>
  <AppShell>
    <ConfirmCard v-if="room" eyebrow="Room invite">
      <h1>Join {{ room.name }}?</h1>
      <p class="muted">You were invited to a focus room. Room code: <strong>{{ room.code }}</strong></p>
      <form class="stack room-invite-actions" method="post" action="/join/confirm">
        <input type="hidden" name="code" :value="room.code">
        <button type="submit" name="action" value="join">Join room</button>
        <button type="submit" name="action" value="cancel" class="ghost">Cancel</button>
      </form>
    </ConfirmCard>
  </AppShell>
</template>
