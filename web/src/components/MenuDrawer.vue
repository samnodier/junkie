<script setup>
// Guest and user drawer, same markup as the Go templates' menu-drawer-guest /
// menu-drawer-user blocks. Room create/join/logout still post to the legacy
// endpoints (native form submits + server redirects) until those flows are
// ported; that keeps behavior identical during the transition.
import { computed, nextTick, onUnmounted, ref, watch } from 'vue';
import { useAuthStore } from '@/stores/auth';
import TempRoomModal from './TempRoomModal.vue';
import RoomCodeInput from './RoomCodeInput.vue';

const props = defineProps({
  open: { type: Boolean, default: false },
  rooms: { type: Array, default: () => [] },
  currentRoomCode: { type: String, default: '' },
  next: { type: String, default: '/' },
  // Incremented by the shell when a page asks for the join-room section to
  // be opened and focused (the desk's "Have a room code?" link).
  joinRequest: { type: Number, default: 0 },
});
const emit = defineEmits(['close']);

const auth = useAuthStore();
const drawerEl = ref(null);
const tempRoomOpen = ref(false);

// Opening the config popup slides the drawer away first, so the modal sits over
// the page (not stacked on top of the still-open, greyed-out panel).
function openTempRoom() {
  tempRoomOpen.value = true;
  emit('close');
}

// Pages that don't already hold the room list (profile, connections, …) get
// it fetched on first open, so the drawer shows your rooms everywhere — same
// as the server-rendered drawer did.
const fetchedRooms = ref(null);
const roomList = computed(() =>
  props.rooms.length ? props.rooms : fetchedRooms.value || []
);
watch(
  () => props.open,
  async (open) => {
    if (!open || props.rooms.length || fetchedRooms.value || !auth.isAuthed) return;
    try {
      const res = await fetch('/api/rooms', { credentials: 'same-origin' });
      if (res.ok) fetchedRooms.value = (await res.json()).rooms;
    } catch {
      fetchedRooms.value = [];
    }
  }
);

watch(
  () => props.open,
  async (open) => {
    document.body.classList.toggle('menu-drawer-open', open);
    if (open) {
      await nextTick();
      drawerEl.value?.querySelector('a, button, input, summary')?.focus();
    }
  }
);

watch(
  () => props.joinRequest,
  async () => {
    await nextTick();
    const join = drawerEl.value?.querySelector('#drawer-join-section');
    if (join) {
      join.open = true;
      join.querySelector('input')?.focus();
    }
  }
);

function onKeydown(event) {
  if (event.key === 'Escape') emit('close');
}
window.addEventListener('keydown', onKeydown);
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown);
  document.body.classList.remove('menu-drawer-open');
});
</script>

<template>
  <div class="menu-drawer-backdrop" v-show="open" @click="emit('close')"></div>
  <aside class="menu-drawer" :class="{ open }" :aria-hidden="String(!open)" ref="drawerEl">
    <div class="menu-drawer-head">
      <button type="button" class="menu-drawer-close" aria-label="Close" @click="emit('close')">×</button>
    </div>

    <template v-if="auth.isAuthed">
      <div class="drawer-identity drawer-identity-user">
        <a href="/profile" class="drawer-profile-row">
          <span class="drawer-avatar"><img v-if="auth.avatarURL" class="avatar-img" :src="auth.avatarURL" alt=""><template v-else>{{ auth.initial }}</template></span>
          <span class="drawer-name">{{ auth.user.displayName }}</span>
        </a>
      </div>
      <p class="label drawer-section-label">Your rooms</p>
      <div class="room-list">
        <template v-if="roomList.length">
          <a
            v-for="room in roomList"
            :key="room.code"
            class="room-row"
            :class="{ 'is-here': room.code === currentRoomCode }"
            :href="`/r/${room.code}`"
          >
            <div class="room-row-main">
              <strong>{{ room.name }}</strong>
              <span class="mono room-row-code">{{ room.code }} · {{ room.autoSessions }}×{{ room.focusMinutes }}/{{ room.breakMinutes }}</span>
            </div>
            <span v-if="room.code === currentRoomCode" class="label here-tag">Here</span>
          </a>
        </template>
        <p v-else class="empty">No rooms yet.</p>
      </div>
      <details class="drawer-details">
        <summary><span class="label">Create room</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
        <form class="room-create" method="post" action="/rooms">
          <input name="name" placeholder="Room name (optional)">
          <button type="submit" class="btn-primary btn-compact">Create</button>
        </form>
      </details>
      <button type="button" class="drawer-temp-room" @click="openTempRoom">
        <span class="label">Create temporary room</span>
        <span class="drawer-temp-room-hint">One-off block, joined by link</span>
      </button>
      <details class="drawer-details" id="drawer-join-section">
        <summary><span class="label">Join room</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
        <form class="room-join" method="post" action="/rooms/join">
          <input type="hidden" name="next" :value="next">
          <RoomCodeInput />
          <button type="submit" class="btn-primary btn-compact">Join</button>
        </form>
      </details>
      <form class="menu-drawer-logout" method="post" action="/logout">
        <button type="submit" class="btn-ghost">Log out</button>
      </form>
      <TempRoomModal v-if="tempRoomOpen" @close="tempRoomOpen = false" />
    </template>

    <template v-else>
      <div class="drawer-identity">
        <a href="/profile" class="drawer-profile-row">
          <span class="drawer-avatar"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg></span>
          <span class="drawer-name">Profile</span>
        </a>
        <a :href="`/login${next && next !== '/' ? `?next=${encodeURIComponent(next)}` : ''}`" class="btn-primary drawer-signin">Sign in</a>
        <p class="label drawer-guest-hint">Rooms need an account — sign in to study together.</p>
      </div>
      <details class="drawer-details" id="drawer-join-section">
        <summary><span class="label">Join room</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
        <form class="room-join" method="post" action="/rooms/join-intent">
          <input type="hidden" name="next" :value="next">
          <RoomCodeInput />
          <button type="submit" class="btn-primary btn-compact">Join</button>
        </form>
      </details>
    </template>
  </aside>
</template>
