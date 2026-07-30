<script setup>
// Wraps a timer column so it can be popped out into the always-on-top
// picture-in-picture window (composables/pipWindow.js).
//
// The card is *moved*, not copied: <Teleport> relocates the very same
// component instances, so there is one timer, one subscription, one source of
// state. Anything that would have updated the card in the page — a socket
// signal, the server's own phase wakeup, someone pressing a button in the
// Discord bot — updates it in the pop-out with no extra wiring, which is what
// makes an untouched auto-roll run walk through focus/break/focus on its own.
import { usePipWindow } from '@/composables/pipWindow';

const { supported, open, mount, openPip, closePip } = usePipWindow();
</script>

<template>
  <div class="pip-slot">
    <button
      v-if="supported && !open"
      type="button"
      class="pip-pop"
      title="Pop the timer out into a small window that stays on top"
      aria-label="Pop out timer"
      @click="openPip()"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
        <rect x="2" y="4" width="20" height="16" rx="2"/>
        <rect x="12" y="12" width="8" height="6" rx="1" fill="currentColor" stroke="none"/>
      </svg>
    </button>

    <!-- `to` falls back to body purely to keep Teleport quiet while disabled;
         placement is the in-page slot whenever the pop-out is closed. -->
    <Teleport :to="mount || 'body'" :disabled="!open">
      <div class="pip-stage" :data-pip="open ? 'out' : 'in'">
        <slot />
      </div>
    </Teleport>

    <article v-if="open" class="panel pip-parked">
      <p class="label label-accent">Popped out</p>
      <p class="muted">The timer is running in its own window, on top of your other apps.</p>
      <button type="button" class="btn-ghost btn-compact" @click="closePip()">Bring it back</button>
    </article>
  </div>
</template>
