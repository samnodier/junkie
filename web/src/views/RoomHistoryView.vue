<script setup>
// A room's own history (/r/{code}/history), for the people who run it.
//
// Scoped to the room by the server, which is what keeps it consistent with
// the rest of room admin: it says what happened to this room, and nothing
// about what its members do anywhere else.
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import EventLog from '@/components/EventLog.vue';

const route = useRoute();
const code = String(route.params.code || '');
const data = ref(null);
const error = ref('');

onMounted(async () => {
  try {
    const res = await fetch(`/api/room/${encodeURIComponent(code)}/events`, {
      credentials: 'same-origin',
    });
    if (!res.ok) {
      error.value = res.status === 403
        ? 'Only this room’s admins can read its history.'
        : 'No such room.';
      return;
    }
    data.value = await res.json();
  } catch {
    error.value = 'Could not load this room’s history.';
  }
});
</script>

<template>
  <AppShell show-menu :next="`/r/${code}`" :current-room-code="code">
    <section class="panel profile-page">
      <p v-if="error" class="notice notice-error" role="alert">{{ error }}</p>
      <template v-if="data">
        <div class="panel-title">
          <h1>{{ data.room.name }} history</h1>
          <span class="mono muted">{{ data.room.code }}</span>
        </div>
        <EventLog :events="data.events" :show-room="false" />
        <p class="muted"><a :href="`/r/${data.room.code}/members`">Back to members</a></p>
      </template>
    </section>
  </AppShell>
</template>
