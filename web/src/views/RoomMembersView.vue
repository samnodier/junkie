<script setup>
// Room members list (/r/{code}/members), ported from the "Room members"
// branch: profile links only for yourself or existing connections.
//
// Role controls follow the group-chat model people already know: a member
// sees a plain roster, and only someone who can already act on the room sees
// buttons. Transfer ownership is deliberately not offered next to every name
// -- it appears only once someone is an admin, so it can't be the stray click
// on a row you meant to promote.
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import { useClock } from '@/composables/clock';
import { postForm } from '@/lib/postForm';
import { askConfirm } from '@/composables/confirm';

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
const canTransfer = computed(() => Boolean(data.value?.canTransfer));

// The pending transfer is only ever sent to the owner who started it, so
// there is nothing here to hide from anyone else -- if it's in the payload,
// it's theirs.
const now = useClock();
const pending = computed(() => data.value?.pendingTransfer || null);
const pendingFor = (member) => pending.value?.userId === member.user.id;

// Remaining time on the window, as m:ss. Derived from the deadline rather
// than counted down, so a throttled tab is late and never wrong.
const remaining = computed(() => {
  if (!pending.value) return '';
  const left = Math.max(0, new Date(pending.value.settlesAt).getTime() - now.value);
  const total = Math.round(left / 1000);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`;
});

// Once the window runs out the server hands the room over on its next read,
// so refresh to pick up who the owner now is instead of sitting at 0:00.
watch(
  () => Boolean(pending.value) && remaining.value === '0:00',
  (due) => {
    if (due) load();
  },
);

async function transfer(member) {
  if (busy.value) return;
  busy.value = member.user.id;
  if (await postForm(`/r/${encodeURIComponent(code)}/transfer-ownership`, { user_id: member.user.id })) {
    await load();
  }
  busy.value = '';
}

async function remove(member) {
  if (busy.value) return;
  // Removing someone is the one control here that acts on another person, so
  // it asks first -- the role buttons are all trivially reversible, this one
  // takes them out of a run in progress.
  const ok = await askConfirm({
    title: `Remove ${member.user.displayName} from this room?`,
    body: 'They lose their place in the room. Their todos and history are untouched, and they can rejoin with the room code.',
    confirmLabel: 'Remove',
    danger: true,
  });
  if (!ok) return;
  busy.value = member.user.id;
  if (await postForm(`/r/${encodeURIComponent(code)}/remove-member`, { user_id: member.user.id })) {
    await load();
  }
  busy.value = '';
}

async function cancelTransfer() {
  if (busy.value) return;
  busy.value = 'transfer';
  if (await postForm(`/r/${encodeURIComponent(code)}/cancel-transfer`, {})) {
    await load();
  }
  busy.value = '';
}

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
      <div class="connections-list room-members-list">
        <template v-if="data.members.length">
          <article v-for="m in data.members" :key="m.user.id" class="connection-row">
            <div class="connection-head member-row">
              <span class="todo-avatar connection-avatar" aria-hidden="true"><img v-if="m.user.hasAvatar" class="avatar-img" :src="`/avatar/${m.user.id}?v=${m.user.avatarVersion}`" alt=""><template v-else>{{ initial(m.user.displayName) }}</template></span>
              <a v-if="m.self" href="/profile" class="connection-name">{{ m.user.displayName }}</a>
              <a v-else-if="m.connected" :href="`/${m.user.username}`" class="connection-name">{{ m.user.displayName }}</a>
              <span v-else class="connection-name room-member-locked" title="Not connected">{{ m.user.displayName }}</span>
              <span class="mono muted member-username">@{{ m.user.username }}</span>
              <span v-if="m.creator" class="role-badge role-owner">Owner</span>
              <span v-else-if="m.admin" class="role-badge role-admin">Admin</span>

              <!-- Actions sit on the same line as the name rather than under
                   it: one row per person, so a long roster stays short.
                   The creator's row carries none -- their authority is the
                   room's creator_id, not a role this page could revoke. -->
              <div v-if="canAdmin && !m.creator" class="member-actions">
                <!-- The countdown IS the undo control, rather than a second
                     button beside it: it reads as the time left, and turning
                     red under the cursor is what says clicking takes it back. -->
                <button
                  v-if="pendingFor(m)"
                  type="button"
                  class="transfer-countdown"
                  :disabled="busy === 'transfer'"
                  :title="`Ownership passes to ${m.user.displayName} — click to take it back`"
                  @click="cancelTransfer"
                >
                  <span class="transfer-countdown-time">Owner in {{ remaining }}</span>
                  <span class="transfer-countdown-undo" aria-hidden="true">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7v6h6"/><path d="M3.5 13a9 9 0 1 0 2.1-7.4L3 8"/></svg>
                    Undo
                  </span>
                </button>
                <!-- Icons rather than words: the actions repeat on every row,
                     so the labels were most of the roster's width. Each keeps
                     its name in title + aria-label, which is what a screen
                     reader and a hover both read. -->
                <button
                  v-if="!m.admin"
                  type="button"
                  class="icon-btn"
                  :disabled="busy === m.user.id"
                  :title="`Make ${m.user.displayName} an admin`"
                  :aria-label="`Make ${m.user.displayName} an admin`"
                  @click="setRole(m, 'make-admin')"
                >
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M12 8v6M9 11h6"/></svg>
                </button>
                <template v-else>
                  <button
                    type="button"
                    class="icon-btn"
                    :disabled="busy === m.user.id || Boolean(pending)"
                    :title="`Remove ${m.user.displayName} as an admin`"
                    :aria-label="`Remove ${m.user.displayName} as an admin`"
                    @click="setRole(m, 'remove-admin')"
                  >
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 11h6"/></svg>
                  </button>
                  <!-- Only offered on someone who is already an admin, so it
                       can never be the stray click on a row you meant to
                       promote. -->
                  <button
                    v-if="canTransfer && !pending"
                    type="button"
                    class="icon-btn"
                    :disabled="busy === m.user.id"
                    :title="`Hand this room to ${m.user.displayName}`"
                    :aria-label="`Hand this room to ${m.user.displayName}`"
                    @click="transfer(m)"
                  >
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="8" cy="7" r="4"/><path d="M16 11h6M19 8l3 3-3 3"/></svg>
                  </button>
                </template>
                <button
                  v-if="!m.self && !pendingFor(m)"
                  type="button"
                  class="icon-btn icon-btn-danger"
                  :disabled="busy === m.user.id"
                  :title="`Remove ${m.user.displayName} from this room`"
                  :aria-label="`Remove ${m.user.displayName} from this room`"
                  @click="remove(m)"
                >
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/></svg>
                </button>
              </div>
            </div>
          </article>
        </template>
        <p v-else class="muted">No one else has joined this room yet.</p>
      </div>
      <p class="muted">
        <a :href="`/r/${data.room.code}`">Back to {{ data.room.name }}</a>
        <!-- Only this room's admins can read it, so only they are shown it. -->
        <template v-if="canAdmin"> · <a :href="`/r/${data.room.code}/history`">Room history</a></template>
      </p>
    </section>
  </AppShell>
</template>
