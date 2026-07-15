<script setup>
// "You're about to start a focus block for <room>" dialog, ported from the
// legacy showStartConfirm modal (same join-prompt-card markup).
defineProps({
  message: { type: String, required: true },
});
const emit = defineEmits(['confirm', 'cancel']);

function onKey(event) {
  if (event.key === 'Escape') emit('cancel');
}
window.addEventListener('keydown', onKey);
import { onUnmounted } from 'vue';
onUnmounted(() => window.removeEventListener('keydown', onKey));
</script>

<template>
  <div class="join-prompt-backdrop" @click.self="emit('cancel')">
    <div class="join-prompt-card panel" role="dialog" aria-labelledby="start-confirm-title">
      <h2 id="start-confirm-title">{{ message }}</h2>
      <div class="join-prompt-actions">
        <button type="button" class="btn-primary" @click="emit('confirm')">Start</button>
        <button type="button" class="btn-ghost" @click="emit('cancel')">Cancel</button>
      </div>
    </div>
  </div>
</template>
