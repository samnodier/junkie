<script setup>
// The room-code field, in both drawer variants (guest and signed-in). Still a
// plain named input inside a native form post — the formatting happens on the
// DOM value, with no v-model, so nothing about how the form submits changes.
import { formatRoomCode } from '@/lib/roomCode';

function onInput(event) {
  const el = event.target;
  const raw = el.value;
  const formatted = formatRoomCode(raw);
  if (formatted === raw) return;
  // Rewriting the value resets the caret to the end, which is wrong for
  // anyone editing the middle of what they typed: hold the caret where it
  // was, shifted by however many characters formatting added or removed.
  const caret = el.selectionStart ?? raw.length;
  const atEnd = caret === raw.length;
  el.value = formatted;
  const next = atEnd
    ? formatted.length
    : Math.max(0, Math.min(caret + (formatted.length - raw.length), formatted.length));
  el.setSelectionRange(next, next);
}
</script>

<template>
  <!-- No maxlength: it would truncate a pasted link before the formatter ever
       saw it. The length cap lives in formatRoomCode instead. -->
  <input
    class="room-code-input"
    name="code"
    placeholder="Code or link"
    required
    aria-label="Room code"
    autocapitalize="characters"
    autocomplete="off"
    spellcheck="false"
    @input="onInput"
  >
</template>
