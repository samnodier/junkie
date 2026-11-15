<script setup>
// The stopwatch before it starts: the clock at zero and a start button.
// There is no ring -- a ring measures progress towards an end, and a
// stopwatch has none -- and no length to set.
defineProps({
  ringClass: { type: String, default: '' },
});
const emit = defineEmits(['start']);
</script>

<template>
  <div class="circle-timer idle ringless" :class="ringClass" role="img" aria-label="Stopwatch, ready to start">
    <div class="circle-timer-core">
      <div class="circle-timer-countdown">00:00</div>
    </div>
  </div>
  <form @submit.prevent="emit('start')">
    <button type="submit" class="btn-primary big-action">Start stopwatch</button>
  </form>
</template>
