<script setup>
// Guest desk, ported from guest.js: idle ring + private todos, then the
// focus/break-offer/break screens with the slide-out todos peek panel.
import { computed, inject, onMounted, onUnmounted, ref, watch, watchEffect } from 'vue';
import { useGuestDeskStore } from '@/stores/guestDesk';
import { requestPermission } from '@/lib/notify';
import { useWakeLock } from '@/composables/wakeLock';
import RingIdle from './RingIdle.vue';
import RingCountdown from './RingCountdown.vue';

const store = useGuestDeskStore();
const shell = inject('shellApi', null);
const wakeLock = useWakeLock();

const minutes = ref(50);
const draft = ref('');
const todoInput = ref(null);

// --- focus-todos peek panel (same localStorage keys as guest.js) ---
const peekOpen = ref(false);
const pinned = ref(localStorage.getItem('junkie:todosPinned') === '1');
const pulse = ref(false);
let hideTimer = null;

function scheduleDismiss() {
  if (pinned.value) return;
  clearTimeout(hideTimer);
  hideTimer = setTimeout(() => {
    if (!pinned.value) peekOpen.value = false;
  }, 10000);
}
function togglePeek() {
  peekOpen.value = !peekOpen.value;
  if (peekOpen.value) scheduleDismiss();
}
function togglePin() {
  pinned.value = !pinned.value;
  localStorage.setItem('junkie:todosPinned', pinned.value ? '1' : '0');
  if (pinned.value) clearTimeout(hideTimer);
}

const inFocusMode = computed(() => store.phase !== 'idle');
watchEffect(() => {
  document.body.classList.toggle('focus-active', inFocusMode.value);
  if (inFocusMode.value) wakeLock.want();
  else wakeLock.release();
});

// First-time hint that the todos peek exists (one 2.4s pulse, then never again).
watch(inFocusMode, (on) => {
  if (!on || localStorage.getItem('junkie:peekSeen') === '1') return;
  localStorage.setItem('junkie:peekSeen', '1');
  pulse.value = true;
  setTimeout(() => {
    pulse.value = false;
  }, 2400);
});

const activeTodos = computed(() => store.sorted.filter((t) => !t.removed));

function startFocus(mins) {
  requestPermission();
  store.startFocus(mins);
}
function cancelFocus() {
  if (!confirm("End this focus session? It won't count toward your map.")) return;
  peekOpen.value = false;
  clearTimeout(hideTimer);
  store.cancelFocus();
}
function addTodo() {
  if (!store.addTodo(draft.value)) return;
  draft.value = '';
  todoInput.value?.focus();
}

let normalizeHandle = null;
onMounted(() => {
  store.normalize();
  normalizeHandle = setInterval(() => store.normalize(true), 1000);
});
onUnmounted(() => {
  clearInterval(normalizeHandle);
  clearTimeout(hideTimer);
  document.body.classList.remove('focus-active');
});
</script>

<template>
  <!-- Idle desk: adjustable ring + private todos -->
  <div v-if="store.phase === 'idle'" class="desk-shell">
    <section class="grid two desk-grid">
      <div class="desk-ring-column">
        <article class="circle-timer-wrap timer-card panel idle desk-timer-card">
          <form class="circle-timer-form" @submit.prevent="startFocus(minutes)">
            <RingIdle v-model="minutes" @submit="startFocus" />
          </form>
          <p class="label desk-ring-hint">Scroll ±1 · buttons ±5 · tap ring to focus</p>
        </article>
      </div>
      <article class="panel desk-todos-panel">
        <div class="panel-title desk-todos-head">
          <h2>Private todos</h2>
          <p class="desk-join-link"><a href="#" data-open-join class="mono-link" @click.prevent="shell?.openJoin()">Have a room code?</a></p>
        </div>
        <form class="inline-form todo-add-form" @submit.prevent="addTodo">
          <input name="text" placeholder="What do you need to do?" required v-model="draft" ref="todoInput" @keydown.enter.prevent="addTodo">
          <!-- Gray while empty, accent once there's text — same affordance as
               the legacy syncTodoAddButton. -->
          <button type="submit" class="todo-add-plus" aria-label="Add task" :disabled="draft.trim() === ''">+</button>
        </form>
        <ul class="todo-list" id="guest-todos">
          <template v-if="store.todos.length">
            <li v-for="t in store.sorted" :key="t.id" :class="t.removed ? 'removed' : t.done ? 'done' : ''" :data-id="t.id">
              <button type="button" class="check guest-toggle" :aria-label="t.done ? 'Mark incomplete' : 'Mark complete'" @click="store.toggleTodo(t.id)">{{ t.done ? '✓' : '○' }}</button>
              <span>{{ t.text }}</span>
              <div v-if="t.removed" class="todo-actions">
                <button type="button" class="todo-action todo-restore guest-restore" title="Bring back" aria-label="Bring back" @click="store.restoreTodo(t.id)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 14 4 9l5-5"/><path d="M4 9h11a5 5 0 0 1 0 10h-3"/></svg></button>
                <button type="button" class="todo-action todo-delete guest-delete" title="Delete permanently" aria-label="Delete permanently" @click="store.deleteTodo(t.id)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg></button>
              </div>
              <button v-else type="button" class="todo-action todo-remove guest-remove" title="Remove" aria-label="Remove" @click="store.removeTodo(t.id)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg></button>
            </li>
          </template>
          <li v-else class="empty">Nothing yet. Add one thing worth finishing.</li>
        </ul>
      </article>
    </section>
  </div>

  <!-- Focus / break screens with the todos peek -->
  <div v-else class="desk-shell desk-shell-focus">
    <section class="focus-desk">
      <div class="focus-desk-main">
        <template v-if="store.phase === 'focus'">
          <p class="label label-accent">{{ store.timer.focusMinutes }} min focus</p>
          <article class="circle-timer-wrap">
            <RingCountdown
              :ends-at="store.timer.endsAt"
              :total-seconds="store.timer.focusMinutes * 60"
              ring-class="running"
              aria-label="Focus countdown"
            />
            <button type="button" class="btn-ghost timer-cancel" @click="cancelFocus">End early</button>
          </article>
        </template>

        <template v-else-if="store.phase === 'break_offer'">
          <p class="label label-warn">{{ store.timer.breakMinutes }} min break</p>
          <article class="circle-timer-wrap">
            <div class="circle-timer break-offer breather" role="timer" aria-label="Break ready">
              <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
                <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
                <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
              </svg>
              <div class="circle-timer-core">
                <div class="circle-timer-countdown" aria-live="polite">{{ String(store.timer.breakMinutes).padStart(2, '0') }}:00</div>
              </div>
            </div>
            <button type="button" class="btn-primary timer-cancel" @click="store.startBreak()">Start break</button>
            <button type="button" class="btn-ghost timer-cancel" @click="store.skipBreak()">Skip break &amp; continue</button>
          </article>
        </template>

        <template v-else-if="store.phase === 'break'">
          <p class="label label-warn">{{ store.timer.breakMinutes }} min break</p>
          <article class="circle-timer-wrap">
            <RingCountdown
              :ends-at="store.timer.endsAt"
              :total-seconds="store.timer.breakMinutes * 60"
              ring-class="break-running breather"
              aria-label="Break countdown"
            />
            <button type="button" class="btn-ghost timer-cancel" @click="store.skipBreak()">Skip break &amp; continue</button>
          </article>
        </template>
      </div>

      <div class="focus-todos-backdrop" v-show="peekOpen && !pinned" @click="peekOpen = false"></div>
      <button
        type="button"
        class="focus-todos-toggle"
        :class="{ 'is-open': peekOpen, 'peek-pulse': pulse }"
        :aria-expanded="String(peekOpen)"
        aria-controls="focus-todos-panel"
        @click="togglePeek"
      >Todos</button>
      <aside class="focus-todos-panel" :class="{ 'is-open': peekOpen }" id="focus-todos-panel" :aria-hidden="String(!peekOpen)">
        <button type="button" class="focus-todos-pin" :class="{ 'is-pinned': pinned }" aria-label="Pin todos panel" title="Pin panel" @click.stop="togglePin"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M12 17v5M9 3h6l1 7h4l-5 6v5H9v-5L4 10h4z"/></svg></button>
        <div class="panel-title"><h2>Todos</h2></div>
        <ul class="todo-list" id="guest-focus-todos">
          <template v-if="activeTodos.length">
            <li v-for="t in activeTodos" :key="t.id" :class="t.done ? 'done' : ''" :data-id="t.id">
              <button type="button" class="check guest-toggle" :aria-label="t.done ? 'Mark incomplete' : 'Mark complete'" @click="store.toggleTodo(t.id); scheduleDismiss()">{{ t.done ? '✓' : '○' }}</button>
              <span>{{ t.text }}</span>
            </li>
          </template>
          <li v-else class="empty">Nothing yet. Add one thing worth finishing.</li>
        </ul>
      </aside>
    </section>
  </div>
</template>
