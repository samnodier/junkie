<script setup>
// Throwaway page for exercising the migration scaffolding (deleted at
// cutover). Renders the real app shell so topbar, drawer, theme, and toasts
// can be verified against production without flipping any user-facing route.
import { useAuthStore } from '@/stores/auth';
import { useToastStore } from '@/stores/toasts';
import AppShell from '@/components/AppShell.vue';

const auth = useAuthStore();
const toasts = useToastStore();
let toastCount = 0;
</script>

<template>
  <AppShell show-menu>
    <section class="panel" style="max-width: 28rem; margin: 3rem auto; padding: var(--sp-6);">
      <p class="eyebrow">Migration pipeline check</p>
      <h1>Vue is serving</h1>
      <p class="muted">
        Built with Vite, embedded in the Go binary, styled by the ported
        stylesheet, talking to the JSON API.
      </p>
      <p class="mono" data-testid="me">
        /api/me → {{ auth.loaded ? (auth.user ? auth.user.username : 'guest') : 'loading…' }}
      </p>
      <button type="button" class="btn-primary" data-testid="toast-btn" @click="toasts.show(`Shell toast #${++toastCount}`)">
        Test toast
      </button>
    </section>
  </AppShell>
</template>
