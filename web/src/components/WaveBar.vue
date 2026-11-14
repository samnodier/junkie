<script setup>
// One-tap greetings for the moments around a block: the waiting room, the
// lobby, the breaks. Deliberately not a chat -- a fixed handful of phrases,
// shown as a passing toast to whoever is here and kept nowhere -- so saying
// hi to someone who just arrived doesn't need a DM, and can't turn into a
// conversation during focus (the server refuses waves then, and the bar
// isn't rendered).
import { ref } from 'vue';
import { useRoomStore } from '@/stores/room';
import { WAVES } from '@/lib/waves';

const room = useRoomStore();
// A short lockout per tap: long enough that a double-tap is one wave.
const cooling = ref(false);
async function send(kind) {
  if (cooling.value) return;
  cooling.value = true;
  setTimeout(() => (cooling.value = false), 1500);
  await room.wave(kind);
}
</script>

<template>
  <div class="wave-bar" role="group" aria-label="Send a quick greeting to the room">
    <button
      v-for="wave in WAVES"
      :key="wave.kind"
      type="button"
      class="wave-chip"
      :disabled="cooling"
      @click="send(wave.kind)"
    ><span aria-hidden="true">{{ wave.emoji }}</span> {{ wave.text }}</button>
  </div>
</template>
