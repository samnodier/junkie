<script setup>
// Overlapping avatar stack, ported from participant-avatar-stack (max 5,
// then a "+n" overflow chip).
defineProps({
  members: { type: Array, required: true },
  small: { type: Boolean, default: false },
});
const MAX = 5;
const initial = (name) => (name ? name[0].toUpperCase() : '?');
</script>

<template>
  <div class="participant-avatars participant-avatars-stack" :class="{ 'participant-avatars-sm': small }" :aria-label="`${members.length} focusing`">
    <span v-for="(m, i) in members.slice(0, MAX)" :key="m.id" class="participant-avatar" :title="m.displayName">
      <img v-if="m.hasAvatar" class="avatar-img" :src="`/avatar/${m.id}?v=${m.avatarVersion}`" :alt="m.displayName">
      <template v-else>{{ initial(m.displayName) }}</template>
    </span>
    <span v-if="members.length > MAX" class="participant-avatar participant-avatar-overflow" :title="`${members.length} focusing`">+{{ members.length - MAX }}</span>
  </div>
</template>
