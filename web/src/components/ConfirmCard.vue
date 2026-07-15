<script setup>
// Shared confirm-screen card (join room, Discord link, connect request):
// same auth-card/room-invite-card markup as the Go template branches.
defineProps({
  eyebrow: { type: String, required: true },
});
</script>

<template>
  <section class="auth-card room-invite-card">
    <div class="auth-card-top">
      <a class="auth-back" href="/" aria-label="Back to home">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 18l-6-6 6-6"/></svg>
      </a>
      <div class="auth-brand"><span class="brand-mark">j</span></div>
    </div>
    <p class="eyebrow">{{ eyebrow }}</p>
    <slot />
  </section>
</template>
