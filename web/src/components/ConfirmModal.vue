<script setup>
// The app's own confirmation dialog, in place of the browser's.
//
// Rendered once by AppShell and driven by the confirm composable, so a call
// site asks a question the same way it used to and gets the app's own
// furniture instead of "localhost:8080 says".
import { nextTick, onUnmounted, ref, watch } from 'vue';
import { confirmState, settleConfirm } from '@/composables/confirm';

const confirmButton = ref(null);

function onKey(event) {
  if (!confirmState.value) return;
  if (event.key === 'Escape') settleConfirm(false);
}
window.addEventListener('keydown', onKey);
onUnmounted(() => window.removeEventListener('keydown', onKey));

// Focus the confirming action when the dialog opens, so Enter answers it and
// a keyboard is never left tabbing from wherever the page happened to be.
watch(confirmState, async (state) => {
  if (!state) return;
  await nextTick();
  confirmButton.value?.focus();
});
</script>

<template>
  <div v-if="confirmState" class="join-prompt-backdrop" @click.self="settleConfirm(false)">
    <div class="join-prompt-card panel" role="dialog" aria-modal="true" aria-labelledby="confirm-title">
      <h2 id="confirm-title">{{ confirmState.title }}</h2>
      <p v-if="confirmState.body" class="muted">{{ confirmState.body }}</p>
      <div class="join-prompt-actions">
        <button
          ref="confirmButton"
          type="button"
          :class="confirmState.danger ? 'btn-danger' : 'btn-primary'"
          @click="settleConfirm(true)"
        >{{ confirmState.confirmLabel }}</button>
        <button type="button" class="btn-ghost" @click="settleConfirm(false)">Cancel</button>
      </div>
    </div>
  </div>
</template>
