<script setup>
// Room members list (/r/{code}/members), ported from the "Room members"
// branch: profile links only for yourself or existing connections.
//
// Role controls follow the group-chat model people already know: a member
// sees a plain roster, and only someone who can already act on the room sees
// buttons. Transfer ownership is deliberately not offered next to every name
// -- it appears only once someone is an admin, so it can't be the stray click
// on a row you meant to promote.
import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import { postForm } from '@/lib/postForm';

const route = useRoute();
const code = String(route.params.code || '');
const data = ref(null);
const busy = ref('');

async function load() {
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
}

onMounted(load);

const canAdmin = computed(() => Boolean(data.value?.viewerAdmin));

async function setRole(member, action) {
  if (busy.value) return;
  busy.value = member.user.id;
  if (await postForm(`/r/${encodeURIComponent(code)}/${action}`, { user_id: member.user.id })) {
    await load();
  }
  busy.value = '';
}

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
              <span v-if="m.creator" class="role-badge role-owner">Owner</span>
              <span v-else-if="m.admin" class="role-badge role-admin">Room admin</span>
            </div>
            <!-- The creator's row carries no role buttons: their authority is
                 the room's creator_id, not a role the page could revoke. -->
            <div v-if="canAdmin && !m.creator" class="member-actions">
              <button
                v-if="!m.admin"
                type="button"
                class="btn-ghost btn-compact"
                :disabled="busy === m.user.id"
                @click="setRole(m, 'make-admin')"
              >Make admin</button>
              <button
                v-else
                type="button"
                class="btn-ghost btn-compact"
                :disabled="busy === m.user.id"
                @click="setRole(m, 'remove-admin')"
              >Remove admin</button>
            </div>
          </article>
        </template>
        <p v-else class="muted">No one else has joined this room yet.</p>
      </div>
      <p class="muted"><a :href="`/r/${data.room.code}`">Back to {{ data.room.name }}</a></p>
    </section>
  </AppShell>
</template>
