<script setup>
// Running countdown ring: reads the shared clock (see composables/clock.js —
// one tick for every ring, re-homed onto the picture-in-picture window so a
// hidden opener tab can't throttle it), fills the ring by remaining/total, and
// emits 'expired' once when it hits zero. Remaining time is always derived
// from the absolute deadline, never decremented, so a late tick is late rather
// than wrong.
import { computed, watch } from 'vue';
import { useClock } from '@/composables/clock';
import { formatElapsed } from '@/lib/elapsed';

const CIRC = 2 * Math.PI * 88;

const props = defineProps({
  endsAt: { type: String, required: true },
  totalSeconds: { type: Number, required: true },
  ringClass: { type: String, default: 'running' },
  ariaLabel: { type: String, default: 'Focus countdown' },
  // Stopwatch: show the time elapsed instead of the time left. The deadline
  // is then the stopwatch's ceiling and totalSeconds the length of that
  // ceiling, so elapsed is still derived from the same anchored deadline --
  // and it still expires, once, if the ceiling is ever reached. A stopwatch
  // draws no ring: a ring measures progress towards an end, and it has none.
  countUp: { type: Boolean, default: false },
  // What a stopwatch counted in earlier stretches (it pauses at its ceiling
  // and can be continued): the clock carries on from there, not from zero.
  baseSeconds: { type: Number, default: 0 },
});
const emit = defineEmits(['expired']);

const now = useClock();
const deadline = computed(() => new Date(props.endsAt).getTime());
// An unparseable deadline reads 00:00 but must never *expire*: a blank endsAt
// means "no timer yet", and firing the phase-advance nudge on it would kick
// the run forward on nothing.
const dated = computed(() => Number.isFinite(deadline.value));
const left = computed(() => {
  if (!dated.value) return 0;
  return Math.max(0, Math.floor((deadline.value - now.value) / 1000));
});

const elapsed = computed(() => props.baseSeconds + Math.max(0, props.totalSeconds - left.value));

const display = computed(() => {
  // Only a stopwatch can pass the hour mark in one phase.
  if (props.countUp) return formatElapsed(elapsed.value);
  const value = left.value;
  return `${String(Math.floor(value / 60)).padStart(2, '0')}:${String(value % 60).padStart(2, '0')}`;
});
// A countdown drains the ring.
const dashOffset = computed(
  () => CIRC * (1 - Math.max(0, Math.min(1, left.value / props.totalSeconds)))
);

// Fire once per deadline. Callers key this component per phase so it normally
// remounts, but a moved-forward deadline (same key, new run) rearms it too.
let expired = false;
watch(deadline, () => {
  expired = false;
});
watch(
  left,
  (value) => {
    if (value <= 0 && dated.value && !expired) {
      expired = true;
      emit('expired');
    }
  },
  { immediate: true, flush: 'post' }
);
</script>

<template>
  <div class="circle-timer" :class="[ringClass, { ringless: countUp }]" role="timer" :aria-label="ariaLabel">
    <svg v-if="!countUp" class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
      <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
      <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" :stroke-dashoffset="dashOffset"/>
    </svg>
    <div class="circle-timer-core">
      <div class="circle-timer-countdown" aria-live="polite" :data-total="totalSeconds">{{ display }}</div>
    </div>
  </div>
</template>
