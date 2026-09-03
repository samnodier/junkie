<script setup>
// The OBS overlay of a temporary focus room (/f/{code}/embed).
//
// Same live run as /f/{code} — same socket, same normalized state — with the
// page stripped to the ring and a transparent background, so a browser source
// composites it straight onto a scene. Read-only by construction: it talks to
// the unauthenticated embed endpoints (see focusembed.go), so there is nothing
// here to start, join, or leave with, and no room store behind it.
//
// It also stays quiet: no notifications and no chime, since it doesn't route
// through onTimerEnd. The chime belongs to the person at the desk, not to a
// scene in a stream, and a browser source is never interacted with, so
// browsers would refuse to play it anyway.
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { connectSignals } from '@/lib/ws';
import { useTimerDeadline, expireNudge } from '@/composables/timerDeadline';
import RingCountdown from '@/components/RingCountdown.vue';
import ParticipantStack from '@/components/ParticipantStack.vue';

const route = useRoute();
const code = String(route.params.code || '');

// The code alone, shown on the overlay because that's the whole point of
// streaming it. Just the code: where to type it is something a stream says
// once, and the address ate the line without earning it.
// ?code=0 takes it off, for a scene you'd rather not be joined in.
const showCode = computed(() => String(route.query.code ?? '') !== '0');

const room = ref(null);
const timer = ref(null);
const waiters = ref([]);
const loaded = ref(false);
// The run ending deletes the room. An overlay left in a scene then shows
// nothing at all rather than a stale clock — the tidiest thing a stream can
// do with a timer that no longer exists.
const gone = ref(false);

const phase = computed(() => timer.value?.phase || 'idle');
const heads = computed(() =>
  timer.value?.participants?.length ? timer.value.participants : waiters.value
);
const { endsAt } = useTimerDeadline(timer);
const phaseKey = () =>
  `${phase.value}:${timer.value?.paused ? 1 : 0}:${timer.value?.breakPending ? 1 : 0}`;

const totalSeconds = computed(() => {
  const t = timer.value;
  if (!t) return 1;
  if (t.phase === 'lobby') return 30;
  if (t.phase === 'focus') return t.focusMinutes * 60;
  return t.breakMinutes * 60;
});

// The heading above the ring, kept to what reads at a glance over video.
const caption = computed(() => {
  const t = timer.value;
  if (!t) return 'Ready';
  if (t.phase === 'lobby') return 'Starting';
  if (t.phase === 'focus') return `Focus · ${t.currentSession}/${t.totalSessions}`;
  if (t.breakPending || t.paused) return 'Break ready';
  return 'Break';
});

// A pending or paused break has no deadline to count down, so the ring would
// read 00:00; show the length that's waiting instead.
const stillRing = computed(() => {
  const t = timer.value;
  if (!t) return `${room.value?.focusMinutes ?? 25}:00`;
  if (t.phase === 'break' && (t.breakPending || t.paused)) return `${t.breakMinutes}:00`;
  return `${t.focusMinutes}:00`;
});
const counting = computed(
  () =>
    !!timer.value &&
    timer.value.phase !== 'idle' &&
    !timer.value.breakPending &&
    !timer.value.paused
);
const ringClass = computed(() =>
  phase.value === 'break' ? 'break-running breather room-focus-ring' : 'running room-focus-ring'
);

async function refresh() {
  try {
    const res = await fetch(`/api/room/${encodeURIComponent(code)}/embed`, {
      credentials: 'same-origin',
    });
    if (res.status === 404) {
      gone.value = true;
      return;
    }
    if (!res.ok) return;
    const data = await res.json();
    room.value = data.room;
    timer.value = data.timer;
    waiters.value = data.waiters || [];
    loaded.value = true;
  } catch {
    /* offline: keep showing the last state and try again on the next tick */
  }
}

function expired() {
  expireNudge(refresh, phaseKey);
}

let socket = null;
let poll = null;

onMounted(async () => {
  // Transparent from the html element down, and no app chrome: both are body
  // classes so they can't be undone by a parent layout this view doesn't have.
  document.body.classList.add('embed-mode', 'focus-room-open');
  // Pin the dark palette: its near-white ink is what reads over video, and a
  // browser source's storage is its own, so whatever theme the host picked in
  // their real browser is neither here nor relevant.
  document.documentElement.setAttribute('data-theme', 'dark');
  await refresh();
  socket = connectSignals(`/ws/f/${encodeURIComponent(code)}`, (type) => {
    if (type === 'deleted') {
      gone.value = true;
      return;
    }
    refresh();
  });
  // Backstop behind the socket. Unconditional, unlike the room store's
  // isWatching() gate: a browser source is never "watching" in any sense the
  // page can detect, and this poll is also what advances an expired phase
  // when the overlay is the only viewer left.
  poll = setInterval(refresh, 45000);
});

onUnmounted(() => {
  socket?.close();
  if (poll) clearInterval(poll);
  document.body.classList.remove('embed-mode', 'focus-room-open');
});
</script>

<template>
  <main v-if="loaded && !gone" class="embed-stage" :data-room-sync="code">
    <!-- Corner count, so the head count reads even at the size a browser
         source gets scaled to; the stack keeps its own +N overflow. -->
    <div v-if="heads.length" class="embed-people">
      <span class="embed-people-count">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
        {{ heads.length }}
      </span>
      <ParticipantStack :members="heads" />
    </div>
    <div class="embed-column">
      <p class="label label-accent embed-caption">{{ caption }}</p>
      <RingCountdown
        v-if="counting"
        :key="`${timer.runId}-${phase}`"
        :ends-at="endsAt"
        :total-seconds="totalSeconds"
        :ring-class="ringClass"
        :aria-label="caption"
        @expired="expired"
      />
      <div v-else class="circle-timer room-focus-ring" role="img" :aria-label="caption">
        <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
          <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
          <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
        </svg>
        <div class="circle-timer-core">
          <div class="circle-timer-countdown">{{ stillRing }}</div>
        </div>
      </div>
      <p v-if="showCode" class="embed-code mono">{{ code }}</p>
    </div>
  </main>
</template>
