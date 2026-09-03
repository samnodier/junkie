<script setup>
// The "chime when a phase ends" control. Drops into the room's timer settings,
// the temporary-room creation popup, and the profile page — all three edit the
// same per-browser setting, so there is one of these rather than three forms.
//
// Nothing here posts anywhere: the setting is localStorage only (see
// lib/sound.js), which is why it sits outside the surrounding <form> logic and
// its fields carry no name attributes.
import { onMounted, ref } from 'vue';
import {
  loadSounds,
  previewSound,
  primeSound,
  setSoundChoice,
  setSoundEnabled,
  soundChoice,
  soundEnabled,
} from '@/lib/sound';

// The two hosts style their preference rows differently: the room settings and
// the temporary-room popup use .settings-auto-roll, the profile page uses
// .profile-preference. Same control, so take the row class from the caller.
const props = defineProps({
  rowClass: { type: String, default: 'settings-auto-roll' },
});

const enabled = ref(soundEnabled());
const choice = ref(soundChoice());
const sounds = ref([]);

onMounted(async () => {
  sounds.value = await loadSounds();
  // A stored id whose sound has since left the manifest would otherwise leave
  // the picker blank and play nothing; fall back to what's actually on offer.
  if (sounds.value.length && !sounds.value.some((s) => s?.id === choice.value)) {
    choice.value = sounds.value[0].id;
    setSoundChoice(choice.value);
  }
  primeSound();
});

function toggle(on) {
  enabled.value = on;
  setSoundEnabled(on);
}

function pick(id) {
  choice.value = id;
  setSoundChoice(id);
}
</script>

<template>
  <div class="sound-pref">
    <div :class="props.rowClass">
      <div>
        <strong>Sound when a block ends</strong>
        <p class="muted">
          A chime as focus turns into a break and back again. This device only —
          it never plays for anyone else in the room.
        </p>
      </div>
      <label class="toggle-control">
        <input
          type="checkbox"
          :checked="enabled"
          aria-label="Sound when a block ends"
          @change="toggle($event.target.checked)"
        >
        <span aria-hidden="true"></span>
      </label>
    </div>
    <div v-if="enabled" class="sound-pref-row">
      <!-- One sound needs no chooser; the picker appears by itself as soon as
           the manifest carries a second. -->
      <label v-if="sounds.length > 1" class="sound-pref-picker">
        Sound
        <select :value="choice" @change="pick($event.target.value)">
          <option v-for="s in sounds" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
      </label>
      <button type="button" class="btn-ghost btn-compact" @click="previewSound">Test sound</button>
    </div>
  </div>
</template>
