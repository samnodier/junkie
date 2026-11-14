<script setup>
// Timer | Stopwatch switch. Two tabs rather than a toggle because the two
// modes ask for different things underneath -- a countdown wants a length, a
// stopwatch wants nothing -- so each tab owns its own panel of instructions.
const mode = defineModel({ type: String, default: 'timer' });
const tabs = [
  { id: 'timer', label: 'Timer' },
  { id: 'stopwatch', label: 'Stopwatch' },
];
function onKey(event) {
  if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
  event.preventDefault();
  mode.value = mode.value === 'timer' ? 'stopwatch' : 'timer';
}
</script>

<template>
  <div class="mode-tabs" role="tablist" aria-label="Timer or stopwatch" @keydown="onKey">
    <button
      v-for="tab in tabs"
      :key="tab.id"
      type="button"
      role="tab"
      class="mode-tab"
      :class="{ 'is-active': mode === tab.id }"
      :aria-selected="String(mode === tab.id)"
      :tabindex="mode === tab.id ? 0 : -1"
      @click="mode = tab.id"
    >{{ tab.label }}</button>
  </div>
</template>
