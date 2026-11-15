<script setup>
// A stopwatch holding at its ceiling: the count so far, standing still.
// The time is already saved; the screen around this offers Continue or Stop.
import { computed } from 'vue';
import { formatElapsed } from '@/lib/elapsed';

const props = defineProps({
  seconds: { type: Number, required: true },
  ringClass: { type: String, default: '' },
});
const display = computed(() => formatElapsed(props.seconds));
</script>

<template>
  <div class="circle-timer ringless" :class="ringClass" role="img" :aria-label="`Stopwatch paused at ${display}`">
    <div class="circle-timer-core">
      <div class="circle-timer-countdown stopwatch-paused-count">{{ display }}</div>
    </div>
  </div>
</template>
