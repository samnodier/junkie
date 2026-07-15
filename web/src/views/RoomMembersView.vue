<script setup>
// Room members list (/r/{code}/members), ported from the "Room members"
// branch: profile links only for yourself or existing connections.
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';

const route = useRoute();
const code = String(route.params.code || '');
const data = ref(null);

onMounted(async () => {
  try {
    const res = await fetch(`/api/room/${encodeURIComponent(code)}/members`, {
      credentials: 'same-origin',
    });
    if (!res.ok) {
      location.href = `/r/${encodeURIComponent(code)}`;
      return;
    }
    data.value = await res.json();
  } catch {
    location.href = '/';
  }
});

const initial = (name) => (name ? name[0].toUpperCase() : '?');
</script>

<template>
  <AppShell show-menu :next="`/r/${code}`" :current-room-code="code">
    <section v-if="data" class="panel profile-page connections-page">
      <div class="panel-title">
        <h1>Room members</h1>
        <span class="mono muted">{{ data.room.code }}</span>
      </div>
      <div class="connections-list">
        <template v-if="data.members.length">
          <article v-for="m in data.members" :key="m.user.id" class="connection-row">
            <div class="connection-head">
              <span class="todo-avatar connection-avatar" aria-hidden="true"><img v-if="m.user.hasAvatar" class="avatar-img" :src="`/avatar/${m.user.id}?v=${m.user.avatarVersion}`" alt=""><template v-else>{{ initial(m.user.displayName) }}</template></span>
              <a v-if="m.self" href="/profile" class="connection-name">{{ m.user.displayName }}</a>
              <a v-else-if="m.connected" :href="`/${m.user.username}`" class="connection-name">{{ m.user.displayName }}</a>
              <span v-else class="connection-name room-member-locked" title="Not connected">{{ m.user.displayName }}</span>
              <span class="mono muted">@{{ m.user.username }}</span>
            </div>
          </article>
        </template>
        <p v-else class="muted">No one else has joined this room yet.</p>
      </div>
      <p class="muted"><a :href="`/r/${data.room.code}`">Back to {{ data.room.name }}</a></p>
    </section>
  </AppShell>
</template>
