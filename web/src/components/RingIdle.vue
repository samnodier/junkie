<script setup>
// Adjustable idle ring, ported from wireIdleTimer: scroll ±1 (80ms throttle),
// step buttons ±5, arrow keys ±1 on ring or input, Enter/Space or ring tap
// submits. Ring fill tracks minutes/max.
import { computed, ref } from 'vue';

const CIRC = 2 * Math.PI * 88;

const props = defineProps({
  min: { type: Number, default: 5 },
  max: { type: Number, default: 180 },
  step: { type: Number, default: 5 },
  ariaLabel: { type: String, default: 'Set focus duration' },
  inputLabel: { type: String, default: 'Focus minutes' },
  // Extra ring classes, e.g. "break-idle" for the orange adjustable break.
  ringClass: { type: String, default: '' },
});
const minutes = defineModel({ type: Number, default: 50 });
const emit = defineEmits(['submit']);

const clamp = (n) => Math.min(props.max, Math.max(props.min, n));
const dashOffset = computed(() => CIRC * (1 - clamp(minutes.value) / props.max));

function adjust(delta) {
  minutes.value = clamp((Number(minutes.value) || props.min) + delta);
}
function onInput(event) {
  const n = Number(event.target.value);
  if (!Number.isNaN(n)) minutes.value = n;
}
function onChange() {
  minutes.value = clamp(Number(minutes.value) || props.min);
}
function onKeydown(event, fromInput) {
  if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
    event.preventDefault();
    if (fromInput) event.stopPropagation();
    adjust(event.key === 'ArrowUp' ? 1 : -1);
    return;
  }
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault();
    if (fromInput) event.stopPropagation();
    submit();
  }
}
let lastWheelAt = 0;
function onWheel(event) {
  if (event.deltaY === 0) return;
  event.preventDefault();
  const now = performance.now();
  if (now - lastWheelAt < 80) return;
  lastWheelAt = now;
  adjust(event.deltaY < 0 ? 1 : -1);
}
function submit() {
  emit('submit', clamp(Number(minutes.value) || props.min));
}
</script>

<template>
  <div
    class="circle-timer idle"
    :class="ringClass"
    role="group"
    :aria-label="ariaLabel"
    tabindex="0"
    @click="submit"
    @keydown="onKeydown($event, false)"
    @wheel.prevent="onWheel"
  >
    <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
      <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
      <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" :stroke-dashoffset="dashOffset"/>
    </svg>
    <div class="circle-timer-core">
      <button type="button" class="circle-timer-step" :data-delta="-step" :aria-label="`Decrease ${step} minutes`" @click.prevent.stop="adjust(-step)">−</button>
      <label class="circle-timer-time">
        <input
          type="number"
          name="focus_minutes"
          :min="min"
          :max="max"
          :value="minutes"
          :aria-label="inputLabel"
          @click.stop
          @input="onInput"
          @change="onChange"
          @keydown="onKeydown($event, true)"
        >
      </label>
      <button type="button" class="circle-timer-step" :data-delta="step" :aria-label="`Increase ${step} minutes`" @click.prevent.stop="adjust(step)">+</button>
    </div>
  </div>
</template>
