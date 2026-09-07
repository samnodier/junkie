<script setup>
// One room's page in the admin space (/admin/rooms/{id}).
//
// Read-only about the room, with one exception: room admins can be marked and
// unmarked from here. There is deliberately no way to join -- staff access is
// for keeping the service working, not for turning up in other people's
// rooms, and that stays true however many people get promoted.
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import { postForm } from '@/lib/postForm';

const route = useRoute();
const id = String(route.params.id || '');
const data = ref(null);
const error = ref('');
const busy = ref('');

async function load() {
  try {
    const res = await fetch(`/api/admin/rooms/${encodeURIComponent(id)}`, {
      credentials: 'same-origin',
    });
    if (!res.ok) {
      error.value = res.status === 404 ? 'No such room.' : 'You can’t view this.';
      return;
    }
    data.value = await res.json();
  } catch {
    error.value = 'Could not load this room.';
  }
}

onMounted(load);

async function setRoomRole(member, role) {
  if (busy.value) return;
  busy.value = member.id;
  if (await postForm(`/admin/rooms/${encodeURIComponent(id)}/room-role`, { user_id: member.id, role })) {
    await load();
  }
  busy.value = '';
}

const initial = (name) => (name ? name[0].toUpperCase() : '?');
</script>

<template>
  <AppShell show-menu>
    <section class="panel profile-page">
      <p v-if="error" class="notice notice-error" role="alert">{{ error }}</p>
      <template v-if="data">
        <div class="panel-title">
          <h1>{{ data.room.name }}</h1>
          <span class="mono muted">{{ data.room.code }}</span>
          <span v-if="data.room.ephemeral" class="role-badge">Temporary</span>
          <span v-if="data.room.active" class="status-active">Active timer</span>
        </div>

        <div class="admin-overview">
          <article class="panel"><span class="label">Owner</span><strong>{{ data.room.creator || 'None' }}</strong></article>
          <article class="panel"><span class="label">Created</span><strong>{{ data.room.created }}</strong></article>
          <article class="panel"><span class="label">Timer</span><strong>{{ data.room.focusMinutes }}/{{ data.room.breakMinutes }}/{{ data.room.autoSessions }}</strong></article>
          <article class="panel"><span class="label">Members</span><strong>{{ data.members.length }}</strong></article>
        </div>

        <section class="admin-section">
          <div class="panel-title"><h2>Members</h2></div>
          <div class="connections-list">
            <article v-for="m in data.members" :key="m.id" class="connection-row">
              <div class="connection-head">
                <span class="todo-avatar connection-avatar" aria-hidden="true"><img v-if="m.hasAvatar" class="avatar-img" :src="`/avatar/${m.id}?v=${m.avatarVersion}`" alt=""><template v-else>{{ initial(m.displayName) }}</template></span>
                <RouterLink :to="`/admin/users/${m.id}`" class="connection-name">{{ m.displayName }}</RouterLink>
                <span class="mono muted">@{{ m.username }}</span>
                <span v-if="m.creator" class="role-badge role-owner">Owner</span>
                <span v-else-if="m.admin" class="role-badge role-admin">Room admin</span>
              </div>
              <!-- The room's creator has no role row to change: their standing
                   is the room's creator_id. -->
              <div v-if="!m.creator" class="member-actions">
                <button
                  type="button"
                  class="btn-ghost btn-compact"
                  :disabled="busy === m.id"
                  @click="setRoomRole(m, m.admin ? 'member' : 'admin')"
                >{{ m.admin ? 'Remove room admin' : 'Make room admin' }}</button>
              </div>
            </article>
          </div>
        </section>

        <p class="muted"><RouterLink to="/admin">Back to the admin space</RouterLink></p>
      </template>
    </section>
  </AppShell>
</template>
