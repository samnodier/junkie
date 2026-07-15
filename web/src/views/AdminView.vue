<script setup>
// Admin space (/admin), ported from the "Admin" template branch. The
// server-side requireAdmin gate still fronts the page; role mutations and
// room deletion post to the legacy audited endpoints, then re-fetch.
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import AppShell from '@/components/AppShell.vue';
import ForbiddenView from './ForbiddenView.vue';

const route = useRoute();
const auth = useAuthStore();
const data = ref(null);
const forbidden = ref('');
const error = ref(String(route.query.error || ''));

async function load() {
  try {
    const res = await fetch('/api/admin', { credentials: 'same-origin' });
    const body = await res.json();
    if (res.status === 403) {
      forbidden.value = body.error;
      return;
    }
    if (res.ok) data.value = body;
  } catch {
    /* leave the page blank; a reload retries */
  }
}

async function post(url, fields = {}, confirmMsg = '') {
  if (confirmMsg && !confirm(confirmMsg)) return;
  try {
    await fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams(fields),
    });
  } catch {
    /* refresh below shows the server's verdict */
  }
  await load();
}

onMounted(load);
</script>

<template>
  <ForbiddenView v-if="forbidden" :message="forbidden" />
  <AppShell v-else>
    <div class="admin-shell" v-if="data">
      <header class="admin-header">
        <div>
          <p class="eyebrow">Platform operations</p>
          <h1>Admin space</h1>
          <p class="muted">Operational metadata only. Private todos and detailed activity are never shown here.</p>
        </div>
        <a href="/" class="btn btn-ghost btn-compact">Back to app</a>
      </header>
      <p v-if="error" class="notice notice-error" role="alert">{{ error }}</p>

      <section class="admin-stats" aria-label="Platform overview">
        <article class="panel"><span class="label">Users</span><strong>{{ data.overview.users }}</strong></article>
        <article class="panel"><span class="label">Rooms</span><strong>{{ data.overview.rooms }}</strong></article>
        <article class="panel"><span class="label">Active room timers</span><strong>{{ data.overview.activeRoomTimers }}</strong></article>
        <article class="panel"><span class="label">Total focus minutes</span><strong>{{ data.overview.totalFocusMinutes }}</strong></article>
      </section>

      <section class="panel admin-section">
        <div class="panel-title">
          <div><p class="eyebrow">Accounts</p><h2>Users</h2></div>
          <span class="role-badge" :class="`role-${auth.user.role}`">{{ auth.user.role }}</span>
        </div>
        <div class="admin-table-wrap">
          <table class="admin-table">
            <thead><tr><th scope="col">Username</th><th scope="col">Role</th><th scope="col">Joined</th><th scope="col">Rooms</th><template v-if="data.isOwner"><th scope="col">Focus summary</th><th scope="col">Role action</th></template></tr></thead>
            <tbody>
              <tr v-for="u in data.users" :key="u.id">
                <td><strong>{{ u.username }}</strong></td>
                <td><span class="role-badge" :class="`role-${u.role}`">{{ u.role }}</span></td>
                <td><time :datetime="u.joinedAt">{{ u.joined }}</time></td>
                <td>{{ u.roomsCount }}</td>
                <template v-if="data.isOwner">
                  <td>{{ u.focusMinutes }} min · {{ u.lastActivity ? `last ${u.lastActivity}` : 'no activity' }}</td>
                  <td>
                    <button v-if="u.role === 'user'" type="button" class="btn-ghost btn-compact" @click="post(`/admin/users/${u.id}/role`, { role: 'admin' }, `Promote ${u.username} to admin?`)">Promote</button>
                    <button v-else-if="u.role === 'admin'" type="button" class="btn-ghost btn-compact" @click="post(`/admin/users/${u.id}/role`, { role: 'user' }, `Demote ${u.username} to user?`)">Demote</button>
                    <span v-else class="muted">Protected</span>
                  </td>
                </template>
              </tr>
              <tr v-if="!data.users.length"><td colspan="6" class="empty">No users found.</td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="panel admin-section">
        <div class="panel-title"><div><p class="eyebrow">Collaboration</p><h2>Rooms</h2></div></div>
        <div class="admin-table-wrap">
          <table class="admin-table">
            <thead><tr><th scope="col">Room</th><th scope="col">Code</th><th scope="col">Creator</th><th scope="col">Members</th><th scope="col">Status</th><th scope="col">Created</th><th scope="col">Action</th></tr></thead>
            <tbody>
              <tr v-for="r in data.rooms" :key="r.id">
                <td><strong>{{ r.name }}</strong></td>
                <td><span class="mono">{{ r.code }}</span></td>
                <td>{{ r.creator }}</td>
                <td>{{ r.membersCount }}</td>
                <td><span v-if="r.active" class="status-active">Active timer</span><template v-else>Idle</template></td>
                <td><time :datetime="r.createdAt">{{ r.created }}</time></td>
                <td>
                  <button type="button" class="btn-danger btn-compact" @click="post(`/admin/rooms/${r.id}/delete`, {}, `Permanently delete room ${r.name} and its room data?`)">Delete</button>
                </td>
              </tr>
              <tr v-if="!data.rooms.length"><td colspan="7" class="empty">No rooms found.</td></tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppShell>
</template>
