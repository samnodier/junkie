<script setup>
// Signed-in desk, ported from the Dashboard template branch: solo + room
// timer cards in the ring column, the private/room todos panel with the
// mode switcher (same sessionStorage keys and ?todos=/&room= URL contract),
// membership pill, and the WS-driven live sync.
import { computed, inject, onMounted, onUnmounted, provide, ref, watch, watchEffect } from 'vue';
import { useAuthStore } from '@/stores/auth';
import { useDeskStore } from '@/stores/desk';
import SoloTimerCard from './SoloTimerCard.vue';
import RoomTimerCard from './RoomTimerCard.vue';
import PipTimer from './PipTimer.vue';
import TodoRow from './TodoRow.vue';
import TodoGroups from './TodoGroups.vue';
import JoinPromptModal from './JoinPromptModal.vue';

const MODE_KEY = 'junkie:deskTodosMode';
const ROOM_KEY = 'junkie:deskTodosRoom';

const auth = useAuthStore();
const desk = useDeskStore();
const shell = inject('shellApi', null);
provide('todoApi', {
  todoAction: (id, action) => desk.todoAction(id, action),
  editTodo: (id, text, patch) => desk.editTodo(id, text, patch),
});

const draft = ref('');
const roomDraft = ref('');
const todoInput = ref(null);
const menuOpen = ref(false);

// --- mode selection (same precedence as the legacy switcher) ---
const params = new URLSearchParams(location.search);
const mode = ref('room');
const room = ref(params.get('room') || sessionStorage.getItem(ROOM_KEY) || '');

function initMode() {
  const codes = desk.rooms.map((r) => r.code);
  if (params.get('todos') === 'private') mode.value = 'private';
  else if (params.get('todos') === 'room' && codes.length) mode.value = 'room';
  else {
    const saved = sessionStorage.getItem(MODE_KEY);
    if (saved === 'private') mode.value = 'private';
    else if (codes.length) mode.value = 'room';
    else mode.value = 'private';
  }
  if (mode.value === 'room' && !codes.includes(room.value)) room.value = codes[0] || '';
  if (!codes.length) mode.value = 'private';
}

const hasRooms = computed(() => desk.rooms.length > 0);
const activeRoom = computed(() => desk.rooms.find((r) => r.code === room.value) || null);
const modeLabel = computed(() =>
  mode.value === 'private' ? 'Private' : activeRoom.value?.name || 'Room todos'
);

function select(nextMode, nextRoom) {
  mode.value = nextMode;
  if (nextRoom) room.value = nextRoom;
  menuOpen.value = false;
  sessionStorage.setItem(MODE_KEY, mode.value);
  if (mode.value === 'room') sessionStorage.setItem(ROOM_KEY, room.value);
  const nextURL = new URL(location.href);
  nextURL.searchParams.set('todos', mode.value);
  if (mode.value === 'room') nextURL.searchParams.set('room', room.value);
  else nextURL.searchParams.delete('room');
  history.replaceState(null, '', nextURL);
}

function onDocClick(event) {
  if (!event.target.closest?.('.desk-todos-switch')) menuOpen.value = false;
}
function onDocKey(event) {
  if (event.key === 'Escape') menuOpen.value = false;
}

async function addPrivate() {
  const text = draft.value.trim();
  if (!text) return;
  draft.value = '';
  await desk.addTodo(text);
  todoInput.value?.focus();
}
async function addRoom() {
  const text = roomDraft.value.trim();
  if (!text || !room.value) return;
  roomDraft.value = '';
  await desk.addRoomTodo(room.value, text);
}

// body.focus-active while the solo timer runs, like the legacy shell class.
watchEffect(() => {
  document.body.classList.toggle('focus-active', !!desk.soloTimer);
});

// Re-wire room sockets when the room set changes (join/create/delete).
watch(
  () => desk.rooms.map((r) => r.code).join(','),
  () => {
    initMode();
    desk.connect(auth.user?.id);
  }
);

onMounted(async () => {
  await desk.refresh();
  initMode();
  desk.connect(auth.user?.id);
  document.addEventListener('click', onDocClick);
  document.addEventListener('keydown', onDocKey);
});
onUnmounted(() => {
  desk.disconnect();
  document.removeEventListener('click', onDocClick);
  document.removeEventListener('keydown', onDocKey);
  document.body.classList.remove('focus-active');
});
</script>

<template>
  <div class="desk-shell" v-if="desk.loaded">
    <div v-if="mode === 'room' && activeRoom" class="desk-room-bar">
      <a class="room-membership-pill" :href="`/r/${activeRoom.code}`" :data-membership-room="activeRoom.code" :data-membership-name="activeRoom.name">
        <span class="room-membership-dot" aria-hidden="true"></span>
        <span class="room-membership-label">Room</span>
        <span class="room-membership-name">{{ activeRoom.name }}</span>
      </a>
    </div>

    <section class="grid two desk-grid">
      <div class="desk-ring-column">
        <PipTimer>
          <SoloTimerCard v-show="mode === 'private'" />
          <template v-for="r in desk.rooms" :key="r.code">
            <RoomTimerCard v-show="mode === 'room' && r.code === room" :room="r" />
          </template>
        </PipTimer>
      </div>

      <article class="panel desk-todos-panel" :data-has-rooms="hasRooms ? 'true' : undefined">
        <div class="panel-title desk-todos-head">
          <div v-if="hasRooms" class="desk-todos-switch">
            <button type="button" class="desk-todos-mode" aria-haspopup="listbox" :aria-expanded="String(menuOpen)" @click="menuOpen = !menuOpen">
              <span class="desk-todos-mode-label">{{ modeLabel }}</span>
              <svg class="desk-todos-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
            </button>
            <div class="desk-todos-menu" v-show="menuOpen" role="listbox">
              <button type="button" role="option" data-mode="private" :class="{ 'is-active': mode === 'private' }" @click="select('private')">Private</button>
              <button
                v-for="r in desk.rooms"
                :key="r.code"
                type="button"
                role="option"
                data-mode="room"
                :data-room="r.code"
                :class="{ 'is-active': mode === 'room' && room === r.code }"
                @click="select('room', r.code)"
              >{{ r.name }}</button>
            </div>
          </div>
          <template v-else>
            <h2>Private todos</h2>
            <p class="desk-join-link"><a href="#" data-open-join class="mono-link" @click.prevent="shell?.openJoin()">Have a room code?</a></p>
          </template>
          <span v-if="hasRooms && mode === 'room'" class="label desk-todos-hint label-warn">Public to the room</span>
        </div>

        <div class="desk-todos-view" data-mode="private" v-show="mode === 'private'">
          <form class="inline-form todo-add-form" @submit.prevent="addPrivate">
            <input name="text" placeholder="What do you need to do?" required v-model="draft" ref="todoInput">
            <button type="submit" class="todo-add-plus" aria-label="Add task" :disabled="draft.trim() === ''">+</button>
          </form>
          <ul class="todo-list" id="personal-todos-list">
            <TodoRow v-for="t in desk.todos" :key="t.id" :todo="t" />
            <li v-if="!desk.todos.length && !hasRooms" class="empty">Nothing yet. Add one thing worth finishing.</li>
          </ul>
        </div>

        <template v-for="r in desk.rooms" :key="`view-${r.code}`">
          <div class="desk-todos-view" data-mode="room" :data-room="r.code" v-show="mode === 'room' && room === r.code">
            <form class="inline-form todo-add-form" @submit.prevent="addRoom">
              <input name="text" placeholder="What are you working on?" required v-model="roomDraft">
              <button type="submit" class="todo-add-plus" aria-label="Add task" :disabled="roomDraft.trim() === ''">+</button>
            </form>
            <TodoGroups :room-code="r.code" :user-name="auth.user?.displayName" :mine="r.mine" :others="r.others" />
          </div>
        </template>
      </article>
    </section>

    <JoinPromptModal
      v-if="desk.joinPrompt"
      :prompt="desk.joinPrompt"
      @join="desk.roomTimer(desk.joinPrompt.code, 'timer-join')"
      @expired="desk.refresh()"
      @dismiss="desk.joinPrompt = null"
    />
  </div>
</template>
