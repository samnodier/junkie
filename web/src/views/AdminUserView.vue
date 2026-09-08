<script setup>
// One person's page in the admin space (/admin/users/{id}).
//
// This page exists so changing someone's platform role is a decision you
// arrive at rather than a button you brush past in a table: promoting grants
// this entire space -- every user, every room, every reset link.
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import { postForm } from '@/lib/postForm';

const route = useRoute();
const id = String(route.params.id || '');
const data = ref(null);
const error = ref('');

async function load() {
  try {
    const res = await fetch(`/api/admin/users/${encodeURIComponent(id)}`, {
      credentials: 'same-origin',
    });
    if (!res.ok) {
      error.value = res.status === 404 ? 'No such user.' : 'You can’t view this.';
      return;
    }
    data.value = await res.json();
  } catch {
    error.value = 'Could not load this user.';
  }
}

onMounted(load);

async function changeRole(role) {
  const name = data.value.user.username;
  const verb = role === 'admin' ? 'Promote' : 'Demote';
  if (!confirm(`${verb} ${name}? An admin can see every user and every room in this space.`)) return;
  if (await postForm(`/admin/users/${encodeURIComponent(id)}/role`, { role })) await load();
}

// Owner-issued password reset link, shown once with a copy button; the server
// audits the issuance and the link expires like any reset token.
const resetLink = ref(null);
const resetCopied = ref(false);
async function createResetLink() {
  if (!confirm(`Create a password reset link for ${data.value.user.username}? It works once and expires in 30 minutes.`)) return;
  try {
    const res = await fetch(`/admin/users/${encodeURIComponent(id)}/reset-link`, {
      method: 'POST',
      credentials: 'same-origin',
    });
    const body = await res.json();
    if (!res.ok) {
      error.value = body.error || 'Could not create a reset link.';
      return;
    }
    resetLink.value = body;
    resetCopied.value = false;
  } catch {
    error.value = 'Could not create a reset link.';
  }
}
async function copyResetLink() {
  try {
    await navigator.clipboard.writeText(resetLink.value.url);
    resetCopied.value = true;
  } catch {
    /* the link stays visible for manual copying */
  }
}

const initial = (name) => (name ? name[0].toUpperCase() : '?');
</script>

<template>
  <AppShell show-menu>
    <section class="panel profile-page">
      <p v-if="error" class="notice notice-error" role="alert">{{ error }}</p>
      <template v-if="data">
        <div class="panel-title">
          <div class="connection-head">
            <span class="todo-avatar connection-avatar" aria-hidden="true"><img v-if="data.user.hasAvatar" class="avatar-img" :src="`/avatar/${data.user.id}?v=${data.user.avatarVersion}`" alt=""><template v-else>{{ initial(data.user.displayName) }}</template></span>
            <h1>{{ data.user.displayName }}</h1>
            <span class="mono muted">@{{ data.user.username }}</span>
            <span class="role-badge" :class="`role-${data.user.role}`">{{ data.user.role }}</span>
          </div>
        </div>

        <div class="detail-stats">
          <div class="detail-stat"><span class="label">Joined</span><strong>{{ data.joined }}</strong></div>
          <div class="detail-stat"><span class="label">Total focus</span><strong>{{ data.focusTime }}</strong></div>
          <div class="detail-stat"><span class="label">Last activity</span><strong>{{ data.lastActivity || 'None' }}</strong></div>
        </div>

        <section class="admin-section">
          <div class="panel-title"><h2>Platform role</h2></div>
          <p v-if="resetLink" class="notice-ok admin-reset-link" role="status">
            Reset link (works once, expires in {{ resetLink.expiresMinutes }} min) — send it to them yourself; it won't be shown again:
            <span class="mono">{{ resetLink.url }}</span>
            <button type="button" class="btn-ghost btn-compact" @click="copyResetLink">{{ resetCopied ? 'Copied!' : 'Copy link' }}</button>
          </p>
          <div class="member-actions">
            <template v-if="data.canChangeRole">
              <button v-if="data.user.role === 'user'" type="button" class="btn-ghost btn-compact" @click="changeRole('admin')">Promote to admin</button>
              <button v-else-if="data.user.role === 'admin'" type="button" class="btn-ghost btn-compact" @click="changeRole('user')">Demote to user</button>
            </template>
            <p v-else class="muted">Only the platform owner can change roles.</p>
            <button v-if="data.user.role !== 'owner'" type="button" class="btn-ghost btn-compact" @click="createResetLink">Create reset link</button>
          </div>
        </section>

        <section class="admin-section">
          <div class="panel-title"><h2>Rooms</h2></div>
          <div class="connections-list">
            <article v-for="r in data.rooms" :key="r.id" class="connection-row">
              <div class="connection-head">
                <RouterLink :to="`/admin/rooms/${r.id}`" class="connection-name">{{ r.name }}</RouterLink>
                <span class="mono muted">{{ r.code }}</span>
                <span v-if="r.creator" class="role-badge role-owner">Owner</span>
                <span v-else-if="r.admin" class="role-badge role-admin">Room admin</span>
              </div>
            </article>
            <p v-if="!data.rooms.length" class="muted">Not in any rooms.</p>
          </div>
        </section>

        <p class="muted"><RouterLink to="/admin">Back to the admin space</RouterLink></p>
      </template>
    </section>
  </AppShell>
</template>
