<script setup>
// The log, as a terminal reads: one line per event, newest first, monospace,
// nothing folded away. Sam's ask was to be able to see what happened without
// picking it out of a formatted table.
//
// The same component serves the whole service (admin) and a single room (its
// own admins). What differs is only which rows the server hands over -- there
// is no client-side filtering standing between anyone and their own data.
import { computed, ref } from 'vue';

const props = defineProps({
  events: { type: Array, default: () => [] },
  showRoom: { type: Boolean, default: true },
});

const filter = ref('');

// Filtering is a convenience over the page already loaded, not a query: the
// server decides what you may see before any of this runs.
const shown = computed(() => {
  const needle = filter.value.trim().toLowerCase();
  if (!needle) return props.events;
  return props.events.filter((e) =>
    [e.action, e.actor, e.room, e.target, JSON.stringify(e.detail || {})]
      .join(' ')
      .toLowerCase()
      .includes(needle),
  );
});

// "2026-09-07 18:22:04" — sortable, unambiguous, and the same width on every
// line so the column of actions stays readable.
function stamp(iso) {
  const d = new Date(iso);
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

// Detail is whatever the event carried; rendering it as key=value keeps the
// line scannable without needing to know each event's shape.
function detail(e) {
  const pairs = Object.entries(e.detail || {}).filter(([k]) => k !== 'room' || !props.showRoom);
  return pairs.map(([k, v]) => `${k}=${v}`).join(' ');
}
</script>

<template>
  <div class="event-log">
    <label class="event-log-filter">
      <span class="visually-hidden">Filter the log</span>
      <input v-model="filter" type="search" placeholder="Filter by action, person or room…">
    </label>
    <p class="muted event-log-count">{{ shown.length }} of {{ events.length }} {{ events.length === 1 ? 'event' : 'events' }}</p>
    <div class="event-log-lines">
      <p v-for="(e, i) in shown" :key="`${e.at}-${i}`" class="event-log-line">
        <span class="event-log-at">{{ stamp(e.at) }}</span>
        <span class="event-log-action">{{ e.action }}</span>
        <span class="event-log-who">{{ e.actor || '—' }}</span>
        <span v-if="showRoom && e.room" class="event-log-room">{{ e.room }}</span>
        <span v-if="e.target && e.target !== e.actor" class="event-log-target">→ {{ e.target }}</span>
        <span v-if="detail(e)" class="event-log-detail">{{ detail(e) }}</span>
      </p>
      <p v-if="!shown.length" class="muted">{{ events.length ? 'Nothing matches that.' : 'Nothing recorded yet.' }}</p>
    </div>
  </div>
</template>
