<script setup>
// Running countdown ring: ticks against an absolute deadline (background
// tabs throttle intervals, so decrementing per tick drifts), fills the ring
// by remaining/total, emits 'expired' once when it hits zero.
import { computed, onMounted, onUnmounted, ref } from 'vue';

const CIRC = 2 * Math.PI * 88;

const props = defineProps({
  endsAt: { type: String, required: true },
  totalSeconds: { type: Number, required: true },
  ringClass: { type: String, default: 'running' },
  ariaLabel: { type: String, default: 'Focus countdown' },
});
const emit = defineEmits(['expired']);

const left = ref(0);
const compute = () =>
  Math.max(0, Math.floor((new Date(props.endsAt).getTime() - Date.now()) / 1000));

const display = computed(() => {
  const m = String(Math.floor(left.value / 60)).padStart(2, '0');
  const s = String(left.value % 60).padStart(2, '0');
  return `${m}:${s}`;
});
const dashOffset = computed(() =>
  CIRC * (1 - Math.max(0, Math.min(1, left.value / props.totalSeconds)))
);

let handle = null;
let expired = false;
onMounted(() => {
  left.value = compute();
  handle = setInterval(() => {
    left.value = compute();
    if (left.value <= 0 && !expired) {
      expired = true;
      emit('expired');
    }
  }, 1000);
});
onUnmounted(() => clearInterval(handle));
</script>

<template>
  <div class="circle-timer" :class="ringClass" role="timer" :aria-label="ariaLabel">
    <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
      <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
      <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" :stroke-dashoffset="dashOffset"/>
    </svg>
    <div class="circle-timer-core">
      <div class="circle-timer-countdown" aria-live="polite" :data-total="totalSeconds">{{ display }}</div>
    </div>
  </div>
</template>
