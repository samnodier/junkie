<script setup>
// 403 card, ported from the "Access denied" template branch. Not routed
// directly: pages render it when their data API answers 403.
import AppShell from '@/components/AppShell.vue';

defineProps({
  message: { type: String, default: 'You do not have access to this page.' },
});
</script>

<template>
  <AppShell>
    <section class="auth-card forbidden-card">
      <p class="eyebrow">403 · Forbidden</p>
      <h1>Access denied</h1>
      <p class="muted">{{ message }}</p>
      <a href="/" class="btn btn-primary">Return to junkie</a>
    </section>
  </AppShell>
</template>
