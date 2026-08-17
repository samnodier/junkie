<script setup>
// Shared "break ready" control for the desk card, the room page and private
// focus, so they stay pixel-identical: adjustable orange ring (tap to start),
// the next block's length, and an explicit Start button for anyone who
// doesn't discover the ring tap. Emits 'start' with the chosen minutes.
import { ref, watch } from 'vue';
import RingIdle from './RingIdle.vue';

const props = defineProps({
  breakMinutes: { type: Number, required: true },
  // The "next block" line only belongs where a next block is actually coming:
  // a room's break rolls into one. A private break ends its run, so /solo
  // leaves this off and the line goes with it.
  focusMinutes: { type: Number, default: 0 },
  roomName: { type: String, default: '' },
  // Fullscreen focus screens size the ring up with "room-focus-ring".
  ringClass: { type: String, default: '' },
});
const emit = defineEmits(['start']);

// This component owns the adjustable value and hands it to RingIdle via
// v-model. Passing :model-value together with an update listener instead
// makes RingIdle's defineModel defer to a parent write-back that never
// happened, freezing the ring against every adjustment path.
const minutes = ref(props.breakMinutes);
watch(
  () => props.breakMinutes,
  (m) => {
    minutes.value = m;
  }
);
function start() {
  emit('start', minutes.value || props.breakMinutes);
}
</script>

<template>
  <form class="circle-timer-form" @submit.prevent="start">
    <RingIdle
      v-model="minutes"
      :min="1"
      :max="60"
      :ring-class="`break-idle ${ringClass}`.trim()"
      :aria-label="roomName ? `Set break length for ${roomName}` : 'Set break length'"
      input-label="Break minutes"
      @submit="start"
    />
  </form>
  <p v-if="focusMinutes" class="label">Next block · {{ focusMinutes }}:00</p>
  <button type="button" class="btn-primary break-ready-start" @click="start">Start break</button>
</template>
