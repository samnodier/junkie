<script setup>
// 404 page, served by the SPA catch-all route. The Go catch-all (GET /)
// serves the SPA shell for any path no other handler claims, so unmatched
// paths land here instead of a blank <router-view>.
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import AppShell from '@/components/AppShell.vue';

// Installed apps (iOS home-screen, the Android TWA) have no address bar or
// back button to escape a dead link, so send people home on their own.
const seconds = ref(3);
const router = useRouter();
let tick;
onMounted(() => {
  tick = setInterval(() => {
    // Show 3, 2, 1 and 0 for a full second each (4s total), then leave.
    // Never go below 0: a negative offset would wrap the arc's dash back on.
    // replace, not push: Back must not land on this 404 again.
    if (seconds.value === 0) router.replace('/');
    else seconds.value -= 1;
  }, 1000);
});
onBeforeUnmount(() => clearInterval(tick));
</script>

<template>
  <AppShell>
    <section class="notfound-card">
      <!-- The desk's focus ring, stopped at 4:04, its short arc shrinking 3-2-1 with the redirect countdown — decorative; the heading
           below carries the message for screen readers. -->
      <div class="circle-timer notfound-ring" aria-hidden="true">
        <svg class="circle-timer-svg" viewBox="0 0 200 200">
          <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
          <circle class="circle-timer-progress notfound-arc" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" :style="{ strokeDashoffset: 553 - 30 * seconds }"/>
        </svg>
        <div class="circle-timer-core notfound-core">
          <div class="circle-timer-countdown">4:04</div>
          <div class="notfound-ring-label">not found</div>
        </div>
      </div>
      <h1>This page lost focus</h1>
      <p class="muted">The link may be mistyped, or it points to a room whose run has ended.</p>
      <p class="muted" aria-live="polite">Taking you back to the desk in {{ seconds }}…</p>
      <a href="/" class="btn btn-primary">Back to the desk</a>
    </section>
  </AppShell>
</template>
