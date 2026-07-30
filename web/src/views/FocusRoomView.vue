<script setup>
// Temporary "focus room" screen (/f/{code}) — the Forest-style single screen
// for an ephemeral room. Reuses the room store, but strips everything except
// the ring: a share link at the top that hides once focus starts, the timer,
// the avatar stack of who's here, and start/leave/break controls. When the run
// completes or empties the room is deleted server-side and everyone is sent
// home.
import { computed, onMounted, onUnmounted, ref, watch, watchEffect } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import { useRoomStore } from '@/stores/room';
import { requestPermission } from '@/lib/notify';
import { useWakeLock } from '@/composables/wakeLock';
import { useTimerDeadline, expireNudge } from '@/composables/timerDeadline';
import TopBar from '@/components/TopBar.vue';
import ToastHolder from '@/components/ToastHolder.vue';
import RingCountdown from '@/components/RingCountdown.vue';
import RingIdle from '@/components/RingIdle.vue';
import ParticipantStack from '@/components/ParticipantStack.vue';
import PipTimer from '@/components/PipTimer.vue';

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const room = useRoomStore();
const wakeLock = useWakeLock();

const copied = ref(false);

const timer = computed(() => room.timer);
const phase = computed(() => timer.value?.phase || 'idle');
const focusing = computed(() => phase.value === 'focus');
// The share link is a distraction during focus; it comes back for the break
// and the pre-start waiting room so latecomers can still hop in.
const showLink = computed(() => room.loaded && !focusing.value);
const shareURL = computed(() => `${location.origin}/f/${room.code}`);

// Heads shown at the bottom: the run's participants once a run exists, else the
// people parked in the waiting room before anyone has started.
const heads = computed(() =>
  timer.value?.participants?.length ? timer.value.participants : room.waiters
);

const { endsAt, seconds } = useTimerDeadline(timer);
const phaseKey = () =>
  `${phase.value}:${timer.value?.paused ? 1 : 0}:${timer.value?.breakPending ? 1 : 0}`;
const totalSeconds = computed(() => {
  const t = timer.value;
  if (!t) return 1;
  if (t.phase === 'lobby') return 30;
  if (t.phase === 'focus') return t.focusMinutes * 60;
  return t.breakMinutes * 60;
});

let copyTimer = null;
async function copyLink() {
  try {
    await navigator.clipboard.writeText(shareURL.value);
    copied.value = true;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => (copied.value = false), 2000);
  } catch {
    /* clipboard denied: nothing to show */
  }
}

function start() {
  requestPermission();
  room.action('timer-start');
}
// The startable break ring owns its value via v-model (see BreakReadyRing
// for why :model-value plus a listener freezes it); seed it each time a
// startable break appears so it opens at the room's configured length.
const breakLength = ref(0);
watch(
  () => (timer.value?.phase === 'break' && (timer.value.breakPending || timer.value.paused) ? timer.value.runId : ''),
  (startable) => {
    if (startable) breakLength.value = timer.value.breakMinutes;
  }
);
function startBreak() {
  room.action('timer-break-length', { minutes: String(breakLength.value || timer.value.breakMinutes) });
}
async function leave() {
  // Leaving cancels a queued join or drops out of the run; either way this
  // person is done here, so head home. (If they were the last one, the room
  // deletes itself and everyone else is sent home too.)
  await room.action('timer-leave');
  goHome();
}
function goHome() {
  router.replace('/dashboard');
}
function expired() {
  expireNudge(() => room.refresh(), phaseKey);
}

watchEffect(() => {
  document.body.classList.toggle('focus-active', focusing.value);
  if (timer.value) wakeLock.want();
  else wakeLock.release();
});

// The room evaporates when the run finishes or empties: the store flips
// `deleted` (via the socket signal or a 404), and we take everyone home.
watch(
  () => room.deleted,
  (gone) => {
    if (gone) goHome();
  }
);

onMounted(async () => {
  // Grid-free the whole time this room is open, not only during focus, for the
  // seamless distraction-free look.
  document.body.classList.add('focus-room-open');
  const code = String(route.params.code || '');
  // Click-the-link join: become a member and queue into the run, then open the
  // socket + first fetch. Idempotent, so this is safe for the creator too.
  await room.enter(code);
  await room.open(code, auth.user?.id);
});
onUnmounted(() => {
  room.close();
  document.body.classList.remove('focus-active', 'focus-room-open');
  clearTimeout(copyTimer);
});
</script>

<template>
  <TopBar :show-menu="false" />
  <main class="page">
    <div v-if="room.loaded" class="room-focus-page" :data-room-sync="room.code">
      <div class="desk-room-bar focus-room-bar">
        <span class="room-membership-pill">
          <span class="room-membership-dot" aria-hidden="true"></span>
          <span class="room-membership-label">Focus room</span>
          <span class="room-membership-name">{{ room.room?.name }}</span>
        </span>
        <button
          v-if="showLink"
          type="button"
          class="focus-room-share"
          :class="{ 'is-copied': copied }"
          @click="copyLink"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
          <span class="mono focus-room-share-url">{{ shareURL.replace(/^https?:\/\//, '') }}</span>
          <span class="focus-room-share-feedback">{{ copied ? 'COPIED ✓' : 'Copy invite' }}</span>
        </button>
      </div>

      <PipTimer>
        <section class="room-focus-shell">
          <!-- Waiting room: no run yet, gather and start -->
          <template v-if="phase === 'idle'">
            <p class="label label-accent">Ready · {{ room.room?.autoSessions }} × {{ room.room?.focusMinutes }}/{{ room.room?.breakMinutes }}</p>
            <p class="room-focus-name">{{ room.room?.name }}</p>
            <div class="circle-timer room-focus-ring" role="img" :aria-label="`${room.room?.focusMinutes} minute focus block, ready to start`">
              <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
                <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
                <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
              </svg>
              <div class="circle-timer-core">
                <div class="circle-timer-countdown">{{ room.room?.focusMinutes }}:00</div>
              </div>
            </div>
            <ParticipantStack v-if="heads.length" :members="heads" />
            <p class="label">{{ heads.length === 1 ? 'Just you so far' : `${heads.length} here` }} · waiting to start</p>
            <form @submit.prevent="start"><button type="submit" class="btn-primary big-action">Start focus block</button></form>
            <button type="button" class="btn-ghost timer-cancel" @click="leave">Leave</button>
          </template>

          <!-- Lobby: 30s countdown before the first focus session -->
          <template v-else-if="phase === 'lobby'">
            <p class="label label-accent">Starting · join now</p>
            <p class="room-focus-name">{{ room.room?.name }}</p>
            <RingCountdown :key="`${timer.runId}-lobby`" :ends-at="endsAt" :total-seconds="30" ring-class="running room-focus-ring" aria-label="Starting countdown" @expired="expired" />
            <ParticipantStack v-if="heads.length" :members="heads" />
            <p class="label">{{ heads.length }} in · here when it starts</p>
            <button type="button" class="btn-ghost timer-cancel" @click="leave">Leave</button>
          </template>

          <!-- Focus: the distraction-free block -->
          <template v-else-if="phase === 'focus'">
            <p class="label label-accent">Focus · session {{ timer.currentSession }} of {{ timer.totalSessions }}</p>
            <p class="room-focus-name">{{ room.room?.name }}</p>
            <RingCountdown :key="`${timer.runId}-focus`" :ends-at="endsAt" :total-seconds="totalSeconds" ring-class="running room-focus-ring" aria-label="Focus countdown" @expired="expired" />
            <ParticipantStack v-if="heads.length > 1" :members="heads" />
            <p class="label">{{ heads.length === 1 ? 'Focusing solo' : `${heads.length} focusing` }}</p>
            <template v-if="!timer.participant">
              <p class="label label-accent" v-if="room.waiting">In for the break · you'll join automatically</p>
              <button v-else type="button" class="btn-primary" @click="room.action('timer-join')">Join at the break</button>
            </template>
            <button v-else type="button" class="btn-ghost timer-cancel" @click="leave">Leave focus block</button>
          </template>

          <!-- Break: the joinable window between sessions -->
          <template v-else>
            <p class="label label-warn">
              {{ timer.breakPending || timer.paused ? 'Break ready · set the length · tap to start' : 'Break · next session in' }}
            </p>
            <p class="room-focus-name">{{ room.room?.name }}</p>
            <RingCountdown v-if="!timer.breakPending && !timer.paused" :key="`${timer.runId}-break`" :ends-at="endsAt" :total-seconds="totalSeconds" ring-class="break-running breather room-focus-ring" aria-label="Break countdown" @expired="expired" />
            <form v-else class="circle-timer-form" @submit.prevent="startBreak">
              <RingIdle v-model="breakLength" :min="1" :max="60" ring-class="break-idle room-focus-ring" aria-label="Set break length" input-label="Break minutes" @submit="startBreak" />
            </form>
            <p class="label">Next block · {{ timer.focusMinutes }}:00</p>
            <button v-if="timer.breakPending || timer.paused" type="button" class="btn-primary" @click="startBreak">Start break</button>
            <ParticipantStack v-if="heads.length" :members="heads" :checkin="room.room?.requireCheckin" />
            <p class="label">{{ heads.length === 1 ? 'Focusing solo' : `${heads.length} focusing` }}</p>
            <template v-if="room.room?.requireCheckin && timer.participant">
              <button v-if="!timer.checkedIn" type="button" class="btn-primary" @click="room.action('timer-checkin')">I'm here — check in for session {{ timer.currentSession + 1 }}</button>
              <p v-else class="label label-accent">Checked in ✓ · in for session {{ timer.currentSession + 1 }}</p>
            </template>
            <button v-if="!timer.participant" type="button" class="btn-primary" @click="room.action('timer-join')">Join this block</button>
            <button v-if="!room.room?.requireCheckin" type="button" class="btn-ghost" @click="room.action('timer-skip-break')">Skip break</button>
            <button v-if="timer.participant" type="button" class="btn-ghost timer-cancel" @click="leave">Leave</button>
          </template>
        </section>
      </PipTimer>
    </div>
  </main>
  <ToastHolder />
</template>
