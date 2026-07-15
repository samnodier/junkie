<script setup>
// "Create temporary room" config popup: focus/break/sessions plus the
// auto-run-breaks toggle. Submitting native-posts to /rooms with ephemeral=1,
// so the server creates the room and redirects to its /f/{code} screen.
import { onMounted, onUnmounted, ref } from 'vue';

const emit = defineEmits(['close']);
const firstField = ref(null);

function onKey(event) {
  if (event.key === 'Escape') emit('close');
}
onMounted(() => {
  window.addEventListener('keydown', onKey);
  firstField.value?.focus();
});
onUnmounted(() => window.removeEventListener('keydown', onKey));
</script>

<template>
  <Teleport to="body">
    <div class="join-prompt-backdrop" @click.self="emit('close')">
      <div class="join-prompt-card panel temp-room-card" role="dialog" aria-labelledby="temp-room-title">
        <h2 id="temp-room-title">Start a temporary focus room</h2>
        <p class="muted temp-room-blurb">A throwaway room for a single block. Share the link, focus together, and it disappears when you're done.</p>
        <form class="stack temp-room-form" method="post" action="/rooms">
          <input type="hidden" name="ephemeral" value="1">
          <div class="temp-room-fields">
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
          <div class="join-prompt-actions">
            <button type="submit" class="btn-primary">Create &amp; get link</button>
            <button type="button" class="btn-ghost" @click="emit('close')">Cancel</button>
          </div>
        </form>
      </div>
    </div>
  </Teleport>
</template>
