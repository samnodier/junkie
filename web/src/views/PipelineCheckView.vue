<script setup>
import { onMounted, ref } from 'vue';

const me = ref(null);
const error = ref('');

onMounted(async () => {
  try {
    const res = await fetch('/api/me', { credentials: 'same-origin' });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    me.value = await res.json();
  } catch (e) {
    error.value = String(e);
  }
});
</script>

<template>
  <main class="page">
    <section class="panel" style="max-width: 28rem; margin: 3rem auto; padding: var(--sp-6);">
      <p class="eyebrow">Migration pipeline check</p>
      <h1>Vue is serving</h1>
      <p class="muted">
        Built with Vite, embedded in the Go binary, styled by the ported
        stylesheet, talking to the JSON API.
      </p>
      <p v-if="me" class="mono" data-testid="me">
        /api/me → {{ me.user ? me.user.username : 'guest' }}
      </p>
      <p v-else-if="error" class="mono" data-testid="me-error">{{ error }}</p>
      <p v-else class="mono">loading…</p>
    </section>
  </main>
</template>
