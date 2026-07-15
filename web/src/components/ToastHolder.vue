<script setup>
import { useToastStore } from '@/stores/toasts';

const toasts = useToastStore();

function onToastClick(toast) {
  if (!toast.onClick) return;
  toast.onClick();
  toasts.dismiss(toast.id);
}
</script>

<template>
  <div id="junkie-toasts" role="status" aria-live="polite">
    <div
      v-for="toast in toasts.items"
      :key="toast.id"
      class="toast"
      :class="{ 'toast-show': toast.visible, 'toast-clickable': !!toast.onClick }"
      @click="onToastClick(toast)"
    >
      <span>{{ toast.message }}</span>
      <button
        type="button"
        class="toast-close"
        aria-label="Dismiss"
        @click.stop="toasts.dismiss(toast.id)"
      >×</button>
    </div>
  </div>
</template>
