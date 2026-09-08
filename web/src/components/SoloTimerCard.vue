<script setup>
// Private solo timer card, ported from desk-private-timer: idle adjustable
// ring, focus countdown, pending break (the room card's set-and-start ring,
// so the control is the same one either way), running break. Server owns the
// state; countdown expiry re-fetches and the server transitions the phase.
import { computed } from 'vue';
import { useAuthStore } from '@/stores/auth';
import { useDeskStore } from '@/stores/desk';
import { requestPermission, onTimerEnd } from '@/lib/notify';
import { useTimerDeadline, expireNudge } from '@/composables/timerDeadline';
import RingIdle from './RingIdle.vue';
import RingCountdown from './RingCountdown.vue';
import BreakReadyRing from './BreakReadyRing.vue';
import { askConfirm } from '@/composables/confirm';

const auth = useAuthStore();
const desk = useDeskStore();
const timer = computed(() => desk.soloTimer);
const { endsAt } = useTimerDeadline(timer);
const phaseKey = () => `${timer.value?.phase || 'idle'}:${timer.value?.breakPending ? 1 : 0}`;

function start(minutes) {
  requestPermission();
  desk.soloStart(minutes);
}
async function cancel() {
  const ok = await askConfirm({
    title: 'End this focus session?',
    body: "It won't count toward your map.",
    confirmLabel: 'End session',
    danger: true,
  });
  if (!ok) return;
  desk.soloCancel();
}
function expired(phase) {
  onTimerEnd(phase);
  expireNudge(() => desk.refresh(), phaseKey);
}
</script>

<template>
  <div class="desk-timer-view" data-mode="private">
    <article v-if="timer" class="timer-card panel desk-timer-card solo-timer" :class="timer.phase">
      <template v-if="timer.phase === 'focus'">
        <p class="label label-accent">Private focus</p>
        <RingCountdown key="solo-focus" :ends-at="endsAt" :total-seconds="timer.focusMinutes * 60" ring-class="running" aria-label="Private focus countdown" @expired="expired('focus')" />
        <form @submit.prevent="cancel"><button type="submit" class="btn-ghost timer-cancel">End early</button></form>
      </template>
      <template v-else-if="timer.breakPending">
        <p class="label label-warn">Private break ready · set the length · tap to start</p>
        <BreakReadyRing :break-minutes="timer.breakMinutes" :room-name="auth.privateRoomName" @start="desk.soloBreakStart" />
        <form @submit.prevent="desk.soloBreakSkip()"><button type="submit" class="btn-ghost timer-cancel">Skip break</button></form>
      </template>
      <template v-else>
        <p class="label label-warn">Private break</p>
        <RingCountdown key="solo-break" :ends-at="endsAt" :total-seconds="timer.breakMinutes * 60" ring-class="break-running breather" aria-label="Private break countdown" @expired="expired('break')" />
        <form @submit.prevent="desk.soloBreakSkip()"><button type="submit" class="btn-ghost timer-cancel">Skip break</button></form>
      </template>
    </article>
    <article v-else class="circle-timer-wrap timer-card panel idle desk-timer-card">
      <form class="circle-timer-form" @submit.prevent>
        <RingIdle :model-value="50" aria-label="Set private focus duration" @submit="start" />
      </form>
      <p class="label desk-ring-hint">Scroll ±1 · buttons ±5 · tap ring to focus</p>
    </article>
  </div>
</template>
