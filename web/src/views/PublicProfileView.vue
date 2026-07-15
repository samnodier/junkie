<script setup>
// /{username}: a connection's public profile, the connect-confirm card, or —
// for anyone else — the same plain 404 body a nonexistent username produces.
// The Go handler still gates guests/invalid names with real HTTP statuses;
// this view renders whichever verdict /api/public-profile returns.
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import ConfirmCard from '@/components/ConfirmCard.vue';
import HeatmapChart from '@/components/HeatmapChart.vue';

const route = useRoute();
const notice = String(route.query.notice || '');
const state = ref({ kind: 'loading' });

onMounted(async () => {
  const username = String(route.params.username || '');
  const params = route.query.connect ? `?connect=${encodeURIComponent(route.query.connect)}` : '';
  try {
    const res = await fetch(`/api/public-profile/${encodeURIComponent(username)}${params}`, {
      credentials: 'same-origin',
    });
    const data = await res.json();
    if (data.redirect) {
      location.href = data.redirect;
      return;
    }
    if (data.confirm) {
      state.value = { kind: 'confirm', ...data.confirm };
    } else if (data.profile) {
      state.value = { kind: 'profile', ...data.profile };
    } else {
      state.value = { kind: 'notFound' };
    }
  } catch {
    state.value = { kind: 'notFound' };
  }
});

const initial = (name) => (name ? name[0].toUpperCase() : '?');
</script>

<template>
  <AppShell>
    <!-- Same body as Go's http.NotFound, so a non-connection's view stays
         indistinguishable from a username that does not exist. -->
    <pre v-if="state.kind === 'notFound'" style="font-family: monospace; margin: 2rem;">404 page not found</pre>

    <ConfirmCard v-else-if="state.kind === 'confirm'" eyebrow="Connect request">
      <h1>Connect with {{ state.displayName }}?</h1>
      <p class="muted">Connections see each other's focus heatmaps — nothing else. <span class="mono">@{{ state.username }}</span> shared this link with you.</p>
      <form class="stack room-invite-actions" method="post" :action="`/connect/${encodeURIComponent(state.username)}`">
        <input type="hidden" name="token" :value="state.token">
        <button type="submit">Connect</button>
        <a href="/" class="ghost btn">Not now</a>
      </form>
    </ConfirmCard>

    <section v-else-if="state.kind === 'profile'" class="panel profile-page public-profile-page">
      <div class="panel-title">
        <div class="connection-head">
          <span class="todo-avatar connection-avatar" aria-hidden="true"><img v-if="state.user.hasAvatar" class="avatar-img" :src="`/avatar/${state.user.id}?v=${state.user.avatarVersion}`" alt=""><template v-else>{{ initial(state.user.displayName) }}</template></span>
          <h1>{{ state.user.displayName }}</h1>
        </div>
        <span class="mono muted">@{{ state.user.username }}</span>
      </div>
      <p v-if="notice" class="notice-ok" role="status">{{ notice }}</p>
      <HeatmapChart :heatmap="state.heatmap" />
      <p class="muted"><a href="/profile">Back to your profile</a></p>
    </section>
  </AppShell>
</template>
