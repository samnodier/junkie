<script setup>
// Overlapping avatar stack, ported from participant-avatar-stack (max 5,
// then a "+n" overflow chip). With `checkin` on (a check-in room's break),
// members carry their check-in state: confirmed heads get a ✓ badge, the
// rest dim until they check in or get dropped at the next session.
defineProps({
  members: { type: Array, required: true },
  small: { type: Boolean, default: false },
  checkin: { type: Boolean, default: false },
});
const MAX = 5;
const initial = (name) => (name ? name[0].toUpperCase() : '?');
</script>

<template>
  <div class="participant-avatars participant-avatars-stack" :class="{ 'participant-avatars-sm': small }" :aria-label="`${members.length} focusing`">
    <span
      v-for="m in members.slice(0, MAX)"
      :key="m.id"
      class="participant-avatar"
      :class="{ 'participant-awaiting-checkin': checkin && !m.checkedIn }"
      :title="checkin ? `${m.displayName} · ${m.checkedIn ? 'checked in' : 'not checked in yet'}` : m.displayName"
    >
      <img v-if="m.hasAvatar" class="avatar-img" :src="`/avatar/${m.id}?v=${m.avatarVersion}`" :alt="m.displayName">
      <template v-else>{{ initial(m.displayName) }}</template>
      <span v-if="checkin && m.checkedIn" class="participant-checkin-badge" aria-hidden="true">✓</span>
    </span>
    <span v-if="members.length > MAX" class="participant-avatar participant-avatar-overflow" :title="`${members.length} focusing`">+{{ members.length - MAX }}</span>
  </div>
</template>
