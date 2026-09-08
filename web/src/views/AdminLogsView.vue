<script setup>
// The whole service's log (/admin/logs).
//
// Its own page rather than a section of the admin space: it is the longest
// thing in there by far, and having it sit between the user and room tables
// pushed them apart for no reason. You come here when you want to read what
// happened, and not otherwise.
import { onMounted, ref } from 'vue';
import AppShell from '@/components/AppShell.vue';
import EventLog from '@/components/EventLog.vue';

const data = ref(null);
const error = ref('');

onMounted(async () => {
  try {
    const res = await fetch('/api/admin/events', { credentials: 'same-origin' });
    if (!res.ok) {
      error.value = 'This space is limited to platform administrators.';
      return;
    }
    data.value = await res.json();
  } catch {
    error.value = 'Could not load the log.';
  }
});
</script>

<template>
  <AppShell show-menu>
    <section class="panel profile-page">
      <p v-if="error" class="notice notice-error" role="alert">{{ error }}</p>
      <template v-if="data">
        <div class="panel-title">
          <div><p class="eyebrow">Everything that happened</p><h1>Log</h1></div>
          <span class="muted">kept {{ data.retentionDays }} days</span>
        </div>
        <EventLog :events="data.events" />
        <p class="muted"><RouterLink to="/admin">Back to the admin space</RouterLink></p>
      </template>
    </section>
  </AppShell>
</template>
