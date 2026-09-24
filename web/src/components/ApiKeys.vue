<script setup>
// Profile card for API keys: make one aimed at the private list or a room,
// see it once, revoke it. The keys themselves only add todos -- see
// cmd/junkie/apikeys.go.
import { computed, onMounted, ref } from 'vue';
import { askConfirm } from '@/composables/confirm';

const keys = ref([]);
const max = ref(10);
const rooms = ref([]);
const name = ref('');
const room = ref('');
const error = ref('');
const busy = ref(false);
// The plaintext of the key just made. Held only until the page goes away:
// the server never hands it out again.
const fresh = ref(null);
const copied = ref(false);

const atMax = computed(() => keys.value.length >= max.value);
const exampleCurl = computed(() =>
  fresh.value
    ? `curl -X POST ${location.origin}/api/todos \\\n  -H "Authorization: Bearer ${fresh.value.key}" \\\n  -H "Content-Type: application/json" \\\n  -d '{"todos":[{"text":"Send revised copy - Sep 26"}]}'`
    : ''
);

async function load() {
  try {
    const [k, r] = await Promise.all([
      fetch('/api/api-keys', { credentials: 'same-origin' }),
      fetch('/api/rooms', { credentials: 'same-origin' }),
    ]);
    if (k.ok) {
      const data = await k.json();
      keys.value = data.keys;
      max.value = data.max;
    }
    if (r.ok) rooms.value = (await r.json()).rooms;
  } catch {
    /* the card still renders; creating will report the failure */
  }
}

async function create() {
  error.value = '';
  busy.value = true;
  try {
    const res = await fetch('/api/api-keys', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ name: name.value, room: room.value }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      error.value = data.error || "That didn't go through — try again.";
      return;
    }
    fresh.value = data;
    copied.value = false;
    name.value = '';
    await load();
  } catch {
    error.value = 'Could not reach the server. Check your connection and try again.';
  } finally {
    busy.value = false;
  }
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(fresh.value.key);
    copied.value = true;
  } catch {
    /* the key is selectable on screen */
  }
}

async function revoke(k) {
  const ok = await askConfirm({
    title: `Revoke "${k.name}"?`,
    body: 'Anything using this key stops being able to add todos. This cannot be undone.',
    confirmLabel: 'Revoke key',
    danger: true,
  });
  if (!ok) return;
  const res = await fetch(`/api/api-keys/${encodeURIComponent(k.id)}/revoke`, { method: 'POST', credentials: 'same-origin' });
  if (!res.ok) {
    error.value = 'Could not revoke that key — try again.';
    return;
  }
  if (fresh.value?.id === k.id) fresh.value = null;
  await load();
}

function destination(k) {
  return k.roomCode ? `${k.roomName} (${k.roomCode})` : 'Private todos';
}

function when(iso) {
  return iso ? new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' }) : 'never';
}

onMounted(load);
</script>

<template>
  <article class="panel profile-card">
    <p class="eyebrow">API keys</p>
    <div class="profile-preference">
      <div>
        <strong>Add todos from other apps</strong>
        <p class="muted">A key lets an automation — meeting notes, a script — add todos to one list. It can't read, complete, or delete anything. <a href="/docs#api">How to use it</a>.</p>
      </div>
    </div>

    <p v-if="error" class="notice-error" role="alert">{{ error }}</p>

    <div v-if="fresh" class="api-key-fresh" role="status">
      <p><strong>Copy this key now — it won't be shown again.</strong></p>
      <div class="api-key-reveal">
        <code class="mono">{{ fresh.key }}</code>
        <button type="button" class="btn-ghost btn-compact" @click="copyKey">{{ copied ? 'Copied' : 'Copy' }}</button>
      </div>
      <pre class="api-key-example mono">{{ exampleCurl }}</pre>
      <button type="button" class="btn-ghost btn-compact" @click="fresh = null">Done</button>
    </div>

    <ul v-if="keys.length" class="api-key-list">
      <li v-for="k in keys" :key="k.id" class="profile-preference">
        <div>
          <strong>{{ k.name }}</strong>
          <p class="muted"><span class="mono">{{ k.prefix }}…</span> · adds to {{ destination(k) }} · last used {{ when(k.lastUsedAt) }}</p>
        </div>
        <button type="button" class="btn-ghost btn-compact" @click="revoke(k)">Revoke</button>
      </li>
    </ul>

    <form v-if="!atMax" class="api-key-form" @submit.prevent="create">
      <label>Name <input v-model="name" required maxlength="60" placeholder="Zoom meeting notes"></label>
      <label>Adds todos to
        <select v-model="room">
          <option value="">Private todos</option>
          <option v-for="r in rooms" :key="r.code" :value="r.code">{{ r.name }} ({{ r.code }})</option>
        </select>
      </label>
      <button type="submit" class="btn-primary btn-compact" :disabled="busy">Create key</button>
    </form>
    <p v-else class="muted">You have the maximum of {{ max }} keys. Revoke one to make another.</p>
  </article>
</template>

<style scoped>
.api-key-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.api-key-form {
  margin-top: 0.75rem;
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  align-items: flex-end;
}
.api-key-form label {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  flex: 1 1 12rem;
}
.api-key-fresh {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  align-items: flex-start;
  margin: 0.75rem 0 1rem;
}
.api-key-reveal {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  max-width: 100%;
}
.api-key-reveal code,
.api-key-example {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  user-select: all;
}
.api-key-example {
  margin: 0;
  font-size: 0.8rem;
  max-width: 100%;
}
</style>
