<script setup>
// The stopwatch before it starts: the same ring as the countdown's, at zero,
// and the whole thing is the start button -- there is no length to set.
defineProps({
  ringClass: { type: String, default: '' },
});
const emit = defineEmits(['start']);
</script>

<template>
  <div
    class="circle-timer idle stopwatch-idle"
    :class="ringClass"
    role="button"
    tabindex="0"
    aria-label="Start stopwatch"
    @click="emit('start')"
    @keydown.enter.prevent="emit('start')"
    @keydown.space.prevent="emit('start')"
  >
    <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
      <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
    </svg>
    <div class="circle-timer-core">
      <div class="circle-timer-countdown">00:00</div>
    </div>
  </div>
</template>
