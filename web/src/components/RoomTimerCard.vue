<script setup>
// Room timer card on the desk, ported from desk-room-timer: idle start ring
// (with the start-confirm dialog), lobby/focus/break countdowns, pending
// break with adjustable length, pause/resume/skip, join/leave, participant
// stack. Actions post to the legacy /r/{code}/... endpoints, then refresh.
import { computed, ref } from 'vue';
import { useDeskStore } from '@/stores/desk';
import { requestPermission } from '@/lib/notify';
import { useTimerDeadline, expireNudge } from '@/composables/timerDeadline';
import RingIdle from './RingIdle.vue';
import RingCountdown from './RingCountdown.vue';
import BreakReadyRing from './BreakReadyRing.vue';
import ParticipantStack from './ParticipantStack.vue';
import StartConfirmModal from './StartConfirmModal.vue';

const props = defineProps({
  room: { type: Object, required: true },
});
const desk = useDeskStore();

const timer = computed(() => props.room.timer);
const phase = computed(() => timer.value?.phase || 'idle');
const confirmStart = ref(0);

const phaseLabel = computed(() => {
  const t = timer.value;
  if (!t) return '';
  if (t.phase === 'lobby') return 'Starting · join now';
  if (t.phase === 'focus') return `Focus · session ${t.currentSession} of ${t.totalSessions}`;
  if (t.breakPending) return 'Break ready · set the length · tap to start';
  if (t.paused) return 'Break paused';
  return `Break · session ${t.currentSession} of ${t.totalSessions}`;
});
const totalSeconds = computed(() => {
  const t = timer.value;
  if (!t) return 1;
  if (t.phase === 'lobby') return 30;
  if (t.phase === 'focus') return t.focusMinutes * 60;
  return t.breakMinutes * 60;
});
// A check-in room hides Skip break unless you're running the block alone —
// skipping ends the window where everyone else confirms they're staying.
const canSkipBreak = computed(
  () => !props.room?.requireCheckin || (timer.value?.participants?.length ?? 0) <= 1
);

const { endsAt } = useTimerDeadline(timer);
const phaseKey = () =>
  `${phase.value}:${timer.value?.paused ? 1 : 0}:${timer.value?.breakPending ? 1 : 0}`;

function requestStart(minutes) {
  requestPermission();
  confirmStart.value = minutes;
}
async function reallyStart() {
  const minutes = confirmStart.value;
  confirmStart.value = 0;
  await desk.roomTimer(props.room.code, 'timer-start', { focus_minutes: String(minutes) });
}
function startBreak(minutes) {
  desk.roomTimer(props.room.code, 'timer-break-length', { minutes: String(minutes) });
}
function expired() {
  expireNudge(() => desk.refresh(), phaseKey);
}
</script>

<template>
  <div class="desk-timer-view" data-mode="room" :data-room="room.code" :data-room-sync="room.code">
    <article v-if="timer" class="timer-card panel desk-timer-card" :class="phase">
      <p class="label" :class="phase === 'focus' || phase === 'lobby' ? 'label-accent' : 'label-warn'">{{ phaseLabel }}</p>

      <BreakReadyRing v-if="timer.breakPending" :break-minutes="timer.breakMinutes" :focus-minutes="timer.focusMinutes" :room-name="room.name" @start="startBreak" />
      <template v-else>
        <div v-if="timer.paused" class="circle-timer break-running breather" role="timer" aria-label="break countdown">
          <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
            <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
            <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" :stroke-dashoffset="553 * (1 - timer.secondsLeft / totalSeconds)"/>
          </svg>
          <div class="circle-timer-core">
            <div class="circle-timer-countdown" aria-live="polite">{{ String(Math.floor(timer.secondsLeft / 60)).padStart(2, '0') }}:{{ String(timer.secondsLeft % 60).padStart(2, '0') }}</div>
          </div>
        </div>
        <RingCountdown
          v-else
          :key="`${timer.runId}-${timer.phase}`"
          :ends-at="endsAt"
          :total-seconds="totalSeconds"
          :ring-class="phase === 'focus' || phase === 'lobby' ? 'running' : 'break-running breather'"
          :aria-label="`${phase} countdown`"
          @expired="expired"
        />
        <p v-if="phase === 'break'" class="label">Next block · {{ timer.focusMinutes }}:00</p>
      </template>

      <p v-if="phase === 'focus' && !timer.participant" class="label label-warn">Watching · join on next break</p>

      <footer class="desk-timer-footer">
        <form v-if="(phase === 'lobby' || phase === 'break') && !timer.participant" @submit.prevent="desk.roomTimer(room.code, 'timer-join')">
          <button type="submit" class="btn-primary">Join this block</button>
        </form>
        <template v-if="phase === 'break' && room.requireCheckin && timer.participant">
          <button v-if="!timer.checkedIn" type="button" class="btn-primary" @click="desk.roomTimer(room.code, 'timer-checkin')">Check in for session {{ timer.currentSession + 1 }}</button>
          <p v-else class="label label-accent">Checked in ✓ · in for session {{ timer.currentSession + 1 }}</p>
        </template>
        <div v-if="phase === 'break' && !timer.breakPending" class="desk-timer-actions-row">
          <button type="button" :class="timer.paused ? 'btn-primary' : 'btn-ghost'" @click="desk.roomTimer(room.code, timer.paused ? 'timer-resume' : 'timer-pause')">{{ timer.paused ? 'Resume break' : 'Pause break' }}</button>
          <button v-if="canSkipBreak" type="button" class="btn-ghost timer-cancel" @click="desk.roomTimer(room.code, 'timer-skip-break')">Skip break</button>
        </div>
        <form v-if="timer.participant" @submit.prevent="desk.roomTimer(room.code, 'timer-leave')">
          <button type="submit" class="btn-ghost timer-cancel">Leave this focus block</button>
        </form>
        <ParticipantStack v-if="timer.participants?.length" :members="timer.participants" small :checkin="room.requireCheckin && phase === 'break'" />
        <p class="label">{{ timer.participant ? 'Participating' : 'Watching' }} · {{ timer.participants?.length || 0 }} joined</p>
      </footer>
    </article>

    <article v-else class="circle-timer-wrap timer-card panel idle desk-timer-card">
      <form class="circle-timer-form" :data-room-name="room.name" @submit.prevent>
        <RingIdle :model-value="room.focusMinutes" aria-label="Room focus minutes" input-label="Room focus minutes" @submit="requestStart" />
      </form>
      <p class="label desk-ring-hint">Scroll ±1 · buttons ±5 · tap ring to start room</p>
    </article>

    <StartConfirmModal
      v-if="confirmStart"
      :message="`You’re about to start a focus block for ${room.name}.`"
      @confirm="reallyStart"
      @cancel="confirmStart = 0"
    />
  </div>
</template>
