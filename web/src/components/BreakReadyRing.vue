<script setup>
// Shared "break ready" control for the desk card and the room page, so the
// two stay pixel-identical: adjustable orange ring (tap to start), the next
// block's length, and an explicit Start button for anyone who doesn't
// discover the ring tap. Emits 'start' with the chosen minutes.
import { ref } from 'vue';
import RingIdle from './RingIdle.vue';

const props = defineProps({
  breakMinutes: { type: Number, required: true },
  focusMinutes: { type: Number, required: true },
  roomName: { type: String, default: '' },
});
const emit = defineEmits(['start']);

const minutes = ref(0);
function start() {
  emit('start', minutes.value || props.breakMinutes);
}
</script>

<template>
  <form class="circle-timer-form" @submit.prevent="start">
    <RingIdle
      :model-value="breakMinutes"
      :min="1"
      :max="60"
      ring-class="break-idle"
      :aria-label="roomName ? `Set break length for ${roomName}` : 'Set break length'"
      input-label="Break minutes"
      @update:model-value="minutes = $event"
      @submit="start"
    />
  </form>
  <p class="label">Next block · {{ focusMinutes }}:00</p>
  <button type="button" class="btn-primary break-ready-start" @click="start">Start break</button>
</template>
