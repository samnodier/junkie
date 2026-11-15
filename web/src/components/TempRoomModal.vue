<script setup>
// "Create temporary room" config popup: focus/break/sessions plus the
// auto-run-breaks toggle. Submitting native-posts to /rooms with ephemeral=1,
// so the server creates the room and redirects to its /f/{code} screen.
import { onMounted, onUnmounted, ref } from 'vue';
import { shortFileName } from '@/lib/fileName';
import ModeTabs from './ModeTabs.vue';

const emit = defineEmits(['close']);
const firstField = ref(null);
// Timer is the default; the stopwatch tab swaps the settings for its own
// explanation, since there is nothing to set.
const mode = ref('timer');

function onKey(event) {
  if (event.key === 'Escape') emit('close');
}
onMounted(() => {
  window.addEventListener('keydown', onKey);
  firstField.value?.focus();
});
onUnmounted(() => window.removeEventListener('keydown', onKey));

// The room's own sound, chosen before the room exists. Duration is checked
// here because this is where it's cheap to know; the server enforces size and
// file type when the room is created.
const soundName = ref('');
async function pickSound(event) {
  const file = event.target.files?.[0];
  soundName.value = file ? file.name : '';
  if (!file) return;
  // Size first, and here rather than only on the server: an oversize body
  // fails the whole multipart parse, so the server never gets to say why.
  if (file.size > 512 * 1024) {
    alert(
      `That file is ${Math.round(file.size / 1024)} KB — the limit is 512 KB. `
        + 'Saving it as an MP3 or OGG instead of a WAV usually makes it far smaller.',
    );
    event.target.value = '';
    soundName.value = '';
    return;
  }
  if (typeof AudioContext === 'undefined') return;
  try {
    const ctx = new AudioContext();
    const buffer = await ctx.decodeAudioData(await file.arrayBuffer());
    ctx.close();
    if (buffer.duration > 15) {
      alert(`That clip is ${Math.round(buffer.duration)} seconds — the limit is 15.`);
      event.target.value = '';
      soundName.value = '';
    }
  } catch {
    /* undecodable here; the server has the final say */
  }
}
</script>

<template>
  <Teleport to="body">
    <div class="join-prompt-backdrop" @click.self="emit('close')">
      <div class="join-prompt-card panel temp-room-card" role="dialog" aria-labelledby="temp-room-title">
        <ModeTabs v-model="mode" />
        <h2 id="temp-room-title">Start a temporary focus room</h2>
        <p v-if="mode === 'timer'" class="muted temp-room-blurb">A throwaway room for a single block. Share the link, focus together, and it disappears when you're done.</p>
        <p v-else class="muted temp-room-blurb">A throwaway room with a shared stopwatch. Share the link, work for as long as it takes, and it disappears when you stop it.</p>
        <!-- multipart because the room can be created with its own sound
             already attached; the server parses either encoding. -->
        <form class="stack temp-room-form" method="post" action="/rooms" enctype="multipart/form-data">
          <input type="hidden" name="ephemeral" value="1">
          <input type="hidden" name="mode" :value="mode">
          <ul v-if="mode === 'stopwatch'" class="stopwatch-notes">
            <li><strong>Counts up from zero.</strong> No length to choose, no breaks, no sessions — it runs until you stop it.</li>
            <li><strong>Everyone's time counts.</strong> Each person is credited for the time they were in, from when they joined.</li>
            <li><strong>Leaving keeps your time.</strong> Anyone can leave early; only you can stop it for the whole room.</li>
            <li><strong>Pauses itself at 6 hours</strong> with the time saved. Continue for up to 6 more, or stop; left paused for an hour, the room closes.</li>
          </ul>
          <template v-else>
          <div class="timer-fields">
            <label>Focus<input ref="firstField" type="number" name="focus_minutes" min="5" max="180" value="25" inputmode="numeric"></label>
            <label>Break<input type="number" name="break_minutes" min="1" max="60" value="5" inputmode="numeric"></label>
            <label>Sessions<input type="number" name="auto_sessions" min="1" max="12" value="4" inputmode="numeric"></label>
          </div>
          <div class="settings-auto-roll">
            <div>
              <strong>Auto-run breaks</strong>
              <p class="muted">Off: after each focus session the break waits until someone starts it. On: breaks run automatically so the whole block is hands-free.</p>
            </div>
            <label class="toggle-control">
              <input type="checkbox" name="auto_roll" value="1" aria-label="Auto-run breaks">
              <span aria-hidden="true"></span>
            </label>
          </div>
          <div class="settings-auto-roll">
            <div>
              <strong>Session check-in</strong>
              <p class="muted">Everyone taps “Check in” during each break to stay in the next session. No-shows are dropped from the block.</p>
            </div>
            <label class="toggle-control">
              <input type="checkbox" name="require_checkin" value="1" aria-label="Session check-in">
              <span aria-hidden="true"></span>
            </label>
          </div>
          <!-- No per-device chime toggle here: a temporary room plays its
               sound for everyone who joins, so this browser's own switch has
               no bearing on the room being made -- and offering it is what
               made "I turned it on" look like it should reach the others. -->
          <div class="settings-auto-roll">
            <div>
              <strong>This room's sound</strong>
              <p class="muted">Optional. Plays for everyone in the room at the end of each block; without one, the built-in chime does. Up to 15 seconds, 512 KB — an MP3 or OGG is much smaller than a WAV.</p>
            </div>
            <label class="btn btn-ghost btn-compact sound-pref-upload">
              <span :title="soundName || null">{{ shortFileName(soundName) || 'Choose a file' }}</span>
              <input
                type="file"
                name="sound"
                accept="audio/mpeg,audio/ogg,audio/wav,.mp3,.ogg,.wav"
                class="visually-hidden"
                @change="pickSound"
              >
            </label>
          </div>
          </template>
          <div class="join-prompt-actions">
            <button type="submit" class="btn-primary">Create &amp; get link</button>
            <button type="button" class="btn-ghost" @click="emit('close')">Cancel</button>
          </div>
        </form>
      </div>
    </div>
  </Teleport>
</template>
