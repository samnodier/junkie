<script setup>
// "X is starting a focus block — join?" dialog with the lobby countdown,
// ported from showFocusJoinPrompt. Auto-dismisses when the lobby closes.
import { computed, onMounted, onUnmounted, ref } from 'vue';

const props = defineProps({
  prompt: { type: Object, required: true }, // {code, roomName, starterName, lobbyDeadline}
});
// The parent owns the join action and refresh; 'expired' fires when the
// lobby closes without a decision.
const emit = defineEmits(['dismiss', 'join', 'expired']);

const deadline = Date.parse(props.prompt.lobbyDeadline || '') || Date.now() + 30000;
const left = ref(10);
let interval = null;

const display = computed(() => '00:' + String(Math.max(0, left.value)).padStart(2, '0'));

function join() {
  emit('join');
  emit('dismiss');
}

onMounted(() => {
  const paint = () => {
    left.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000));
    if (left.value === 0) {
      clearInterval(interval);
      emit('expired');
      emit('dismiss');
    }
  };
  interval = setInterval(paint, 250);
  paint();
});
onUnmounted(() => clearInterval(interval));
</script>

<template>
  <div class="join-prompt-backdrop" id="focus-join-prompt" @click.self="emit('dismiss')">
    <div class="join-prompt-card panel" role="dialog" aria-labelledby="join-prompt-title">
      <p class="label label-accent">Starting · join now</p>
      <h2 id="join-prompt-title">{{ prompt.starterName }} is starting a focus block in {{ prompt.roomName }}. Join?</h2>
      <p class="join-prompt-countdown mono" aria-live="polite">{{ display }}</p>
      <div class="join-prompt-actions">
        <form @submit.prevent="join"><button type="submit" class="btn-primary">Join</button></form>
        <button type="button" class="btn-ghost" data-dismiss @click="emit('dismiss')">Not now</button>
      </div>
    </div>
  </div>
</template>
