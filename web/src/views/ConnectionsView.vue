<script setup>
// Connections feed, ported from the "Connections" template branch: one
// heatmap card per connection, newest first.
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import HeatmapChart from '@/components/HeatmapChart.vue';
import { useConnectLink } from '@/composables/connectLink';

const route = useRoute();
const notice = String(route.query.notice || '');
const connections = ref(null);
const { label: connectLabel, copy: copyConnectLink } = useConnectLink();

onMounted(async () => {
  try {
    const res = await fetch('/api/connections', { credentials: 'same-origin' });
    if (res.ok) connections.value = (await res.json()).connections;
  } catch {
    connections.value = [];
  }
});

const initial = (name) => (name ? name[0].toUpperCase() : '?');
</script>

<template>
  <AppShell>
    <section class="panel profile-page connections-page">
      <div class="panel-title">
        <h1>Connections</h1>
        <button type="button" id="connect-link-copy" class="btn-ghost btn-compact" @click="copyConnectLink">{{ connectLabel }}</button>
      </div>
      <p v-if="notice" class="notice-ok" role="status">{{ notice }}</p>
      <div class="connections-list" v-if="connections">
        <template v-if="connections.length">
          <article v-for="c in connections" :key="c.user.id" class="connection-row">
            <div class="connection-head">
              <span class="todo-avatar connection-avatar" aria-hidden="true"><img v-if="c.user.hasAvatar" class="avatar-img" :src="`/avatar/${c.user.id}?v=${c.user.avatarVersion}`" alt=""><template v-else>{{ initial(c.user.displayName) }}</template></span>
              <a :href="`/${c.user.username}`" class="connection-name">{{ c.user.displayName }}</a>
              <span class="mono muted">@{{ c.user.username }}</span>
            </div>
            <HeatmapChart :heatmap="c.heatmap" />
          </article>
        </template>
        <p v-else class="muted">No connections yet. Copy your link and send it to one person — it works once, then you mint a new one for the next person.</p>
      </div>
      <p class="muted"><a href="/profile">Back to your profile</a></p>
    </section>
  </AppShell>
</template>
