<script setup>
import { ref } from 'vue';

defineProps({
  name: { type: String, default: 'password' },
  autocomplete: { type: String, default: 'current-password' },
});
const model = defineModel({ type: String, default: '' });
const visible = ref(false);
</script>

<template>
  <span class="password-field">
    <input
      :type="visible ? 'text' : 'password'"
      :name="name"
      :autocomplete="autocomplete"
      v-model="model"
      required
    >
    <button
      type="button"
      class="password-toggle"
      :class="{ 'is-visible': visible }"
      :aria-label="visible ? 'Hide password' : 'Show password'"
      :aria-pressed="String(visible)"
      @click="visible = !visible"
    >
      <svg class="icon-eye" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z"/><circle cx="12" cy="12" r="3"/></svg>
      <svg class="icon-eye-off" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 3l18 18"/><path d="M10.6 10.6a3 3 0 0 0 4.24 4.24"/><path d="M9.9 5.1A10.4 10.4 0 0 1 12 5c6.5 0 10 7 10 7a13.3 13.3 0 0 1-3.09 3.9M6.1 6.1A13.4 13.4 0 0 0 2 12s3.5 7 10 7c1.15 0 2.25-.16 3.29-.47"/></svg>
    </button>
  </span>
</template>
