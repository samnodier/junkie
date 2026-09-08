<script setup>
// The "chime when a phase ends" control. Drops into the room's timer settings,
// the temporary-room creation popup, and the profile page — all three edit the
// same per-browser setting, so there is one of these rather than three forms.
//
// Nothing here posts anywhere: the setting is localStorage only (see
// lib/sound.js), which is why it sits outside the surrounding <form> logic and
// its fields carry no name attributes.
import { computed, onMounted, ref, watch } from 'vue';
import {
  loadSounds,
  previewSound,
  primeSound,
  setSoundChoice,
  setSoundEnabled,
  setSoundSource,
  soundChoice,
  soundEnabled,
} from '@/lib/sound';
import { postForm } from '@/lib/postForm';

// The two hosts style their preference rows differently: the room settings and
// the temporary-room popup use .settings-auto-roll, the profile page uses
// .profile-preference. Same control, so take the row class from the caller.
const props = defineProps({
  rowClass: { type: String, default: 'settings-auto-roll' },
  // A room's own catalogue when set, the built-in list otherwise.
  roomCode: { type: String, default: '' },
  // Uploading is a room-admin power: this is audio that plays in other
  // people's rooms, and no automated check catches an unpleasant one.
  canUpload: { type: Boolean, default: false },
});

const enabled = ref(soundEnabled());
const choice = ref(soundChoice());
const sounds = ref([]);
const uploading = ref(false);
const fileInput = ref(null);

// The file actually behind the current choice, so the picker says what is set
// rather than leaving you to guess which "custom" this is.
const chosenFileName = computed(
  () => sounds.value.find((s) => s?.id === choice.value)?.fileName || '',
);
const roomSound = computed(() => sounds.value.find((s) => s?.fileName));

async function refresh() {
  sounds.value = await loadSounds();
  // A stored id whose sound has since left the manifest would otherwise leave
  // the picker blank and play nothing; fall back to what's actually on offer.
  if (sounds.value.length && !sounds.value.some((s) => s?.id === choice.value)) {
    choice.value = sounds.value[0].id;
    setSoundChoice(choice.value);
  }
  primeSound();
}

watch(() => props.roomCode, (code) => { setSoundSource(code); refresh(); });

onMounted(() => {
  setSoundSource(props.roomCode);
  refresh();
});

// Duration is checked here because this is the one place it's cheap to know.
// The server enforces size and file type; it deliberately does not decode
// audio, so this is the friendly check rather than the security one.
async function tooLong(file) {
  if (typeof AudioContext === 'undefined') return 0;
  try {
    const ctx = new AudioContext();
    const buffer = await ctx.decodeAudioData(await file.arrayBuffer());
    ctx.close();
    return buffer.duration;
  } catch {
    return 0; // undecodable here; let the server have the final say
  }
}

async function upload(event) {
  const file = event.target.files?.[0];
  if (!file) return;
  uploading.value = true;
  try {
    const seconds = await tooLong(file);
    if (seconds > 15) {
      alert(`That clip is ${Math.round(seconds)} seconds — the limit is 15.`);
      return;
    }
    if (file.size > 512 * 1024) {
      alert(
        `That file is ${Math.round(file.size / 1024)} KB — the limit is 512 KB. ` +
          'Saving it as an MP3 or OGG instead of a WAV usually makes it far smaller.',
      );
      return;
    }
    const body = new FormData();
    body.append('sound', file);
    const res = await fetch(`/r/${encodeURIComponent(props.roomCode)}/sound`, {
      method: 'POST',
      credentials: 'same-origin',
      body,
    });
    const dest = res.redirected ? new URL(res.url, location.origin) : null;
    const failed = dest?.searchParams.get('error');
    if (failed) {
      alert(failed);
      return;
    }
    setSoundChoice('room:sound');
    choice.value = 'room:sound';
    await refresh();
  } finally {
    uploading.value = false;
    if (fileInput.value) fileInput.value.value = '';
  }
}

async function removeSound() {
  if (!confirm('Remove this room\u2019s sound? Everyone falls back to the built-in chime.')) return;
  if (await postForm(`/r/${encodeURIComponent(props.roomCode)}/sound`, { remove: '1' })) {
    await refresh();
  }
}

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
    <!-- The file behind the choice, so you can see what's already set before
         deciding whether to replace it. -->
    <p v-if="enabled && chosenFileName" class="muted sound-pref-filename mono">{{ chosenFileName }}</p>
    <div v-if="canUpload && roomCode" class="sound-pref-row">
      <label class="btn btn-ghost btn-compact sound-pref-upload">
        {{ uploading ? 'Uploading…' : roomSound ? 'Replace room sound' : 'Upload a room sound' }}
        <input
          ref="fileInput"
          type="file"
          accept="audio/mpeg,audio/ogg,audio/wav,.mp3,.ogg,.wav"
          class="visually-hidden"
          :disabled="uploading"
          @change="upload"
        >
      </label>
      <button v-if="roomSound" type="button" class="btn-ghost btn-compact" @click="removeSound">Remove</button>
      <span class="muted">Up to 15 seconds, 512 KB. MP3 or OGG is much smaller than WAV.</span>
    </div>
  </div>
</template>
