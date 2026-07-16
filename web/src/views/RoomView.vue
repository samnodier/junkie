<script setup>
// The room page (/r/{code}), ported from the "room" template: header with
// copyable code/invite link and inline rename, the timer column
// (ready/lobby/focus/break with join/leave/pause/skip and the focus-mode
// fullscreen), settings/share/controls sections, and the room todos board.
import { computed, onMounted, onUnmounted, provide, ref, watch, watchEffect } from 'vue';
import { useRoute } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import { useRoomStore } from '@/stores/room';
import { useToastStore } from '@/stores/toasts';
import { requestPermission } from '@/lib/notify';
import { useWakeLock } from '@/composables/wakeLock';
import { useTimerDeadline, expireNudge } from '@/composables/timerDeadline';
import AppShell from '@/components/AppShell.vue';
import ConfirmCard from '@/components/ConfirmCard.vue';
import RingCountdown from '@/components/RingCountdown.vue';
import ParticipantStack from '@/components/ParticipantStack.vue';
import TodoGroups from '@/components/TodoGroups.vue';
import JoinPromptModal from '@/components/JoinPromptModal.vue';
import StartConfirmModal from '@/components/StartConfirmModal.vue';

const route = useRoute();
const auth = useAuthStore();
const room = useRoomStore();
const wakeLock = useWakeLock();
provide('todoApi', {
  todoAction: (id, action) => room.todoAction(id, action),
  editTodo: (id, text, patch) => room.editTodo(id, text, patch),
});

const draft = ref('');
const renaming = ref(false);
const renameDraft = ref('');
const confirmStart = ref(false);
const copied = ref('');
const breakLength = ref(0);

const timer = computed(() => room.timer);
const phase = computed(() => timer.value?.phase || 'idle');
const focusMode = computed(() => phase.value === 'focus' && timer.value?.participant);
const { endsAt, seconds } = useTimerDeadline(timer);
const phaseKey = () =>
  `${phase.value}:${timer.value?.paused ? 1 : 0}:${timer.value?.breakPending ? 1 : 0}`;
const totalSeconds = computed(() => {
  const t = timer.value;
  if (!t) return 1;
  if (t.phase === 'lobby') return 30;
  if (t.phase === 'focus') return t.focusMinutes * 60;
  return t.breakMinutes * 60;
});

// Collapsible sections persist per room, same keys as the legacy page.
const sections = { settings: ref(false), share: ref(false), controls: ref(false) };
function sectionKey(name) {
  return `junkie:room:${room.code}:open:${name}`;
}
function toggleSection(name, open) {
  sections[name].value = open;
  localStorage.setItem(sectionKey(name), open ? '1' : '0');
}

let copyTimer = null;
async function copy(text, which) {
  try {
    await navigator.clipboard.writeText(text);
    copied.value = which;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copied.value = '';
    }, 2000);
  } catch {
    /* clipboard denied: nothing to show */
  }
}
const inviteMessage = () =>
  `Join my focus room on junkie: ${location.origin}/r/${room.code}`;

function startRename() {
  renameDraft.value = room.room?.name || '';
  renaming.value = true;
}
async function saveRename() {
  renaming.value = false;
  const name = renameDraft.value.trim();
  if (name && name !== room.room?.name) await room.action('rename', { name });
}

async function saveSettings(event) {
  const f = new FormData(event.target);
  await room.action('settings', {
    focus_minutes: String(f.get('focus_minutes')),
    break_minutes: String(f.get('break_minutes')),
    auto_sessions: String(f.get('auto_sessions')),
    auto_roll: f.get('auto_roll') ? '1' : '0',
    require_checkin: f.get('require_checkin') ? '1' : '0',
  });
  // action() re-fetched the room, so compare what came back to what was
  // submitted: a mismatch means the server refused (a run started while the
  // form was open) rather than saved. Native min/max validation keeps the
  // inputs inside the server's clamp range, so equality is a fair check.
  const toasts = useToastStore();
  const r = room.room;
  const saved =
    r &&
    r.focusMinutes === Number(f.get('focus_minutes')) &&
    r.breakMinutes === Number(f.get('break_minutes')) &&
    r.autoSessions === Number(f.get('auto_sessions')) &&
    r.autoRoll === !!f.get('auto_roll') &&
    r.requireCheckin === !!f.get('require_checkin');
  if (saved) {
    toasts.show(`Timer settings saved · ${r.autoSessions}×${r.focusMinutes}/${r.breakMinutes}`);
  } else {
    toasts.show("Couldn't save — timer settings can't change while a run is active.");
  }
}
async function deleteRoom() {
  if (!confirm(`Delete '${room.room.name}'? This removes it for all ${room.memberCount} members.`)) return;
  await room.action('delete');
  location.href = '/';
}
async function addTodo() {
  const text = draft.value.trim();
  if (!text) return;
  draft.value = '';
  await room.addTodo(text);
}
function reallyStart() {
  confirmStart.value = false;
  requestPermission();
  room.action('timer-start');
}
function expired() {
  expireNudge(() => room.refresh(), phaseKey);
}

watchEffect(() => {
  document.body.classList.toggle('focus-active', !!focusMode.value);
  if (timer.value) wakeLock.want();
  else wakeLock.release();
});

// Deleted room: land back on the desk with the same error the legacy page
// produced via redirect.
watch(
  () => room.deleted,
  (gone) => {
    if (gone) location.href = '/?error=' + encodeURIComponent('That room no longer exists.');
  }
);

onMounted(async () => {
  await room.open(String(route.params.code || ''), auth.user?.id);
  for (const name of Object.keys(sections)) {
    sections[name].value = localStorage.getItem(sectionKey(name)) === '1';
  }
});
onUnmounted(() => {
  room.close();
  document.body.classList.remove('focus-active');
});
</script>

<template>
  <AppShell show-menu :next="`/r/${room.code}`" :current-room-code="room.code">
    <!-- Non-member invite card (same as /join/confirm) -->
    <ConfirmCard v-if="room.invite" eyebrow="Room invite">
      <h1>Join {{ room.invite.name }}?</h1>
      <p class="muted">You were invited to a focus room. Room code: <strong>{{ room.invite.code }}</strong></p>
      <form class="stack room-invite-actions" method="post" action="/join/confirm">
        <input type="hidden" name="code" :value="room.invite.code">
        <button type="submit" name="action" value="join">Join room</button>
        <button type="submit" name="action" value="cancel" class="ghost">Cancel</button>
      </form>
    </ConfirmCard>

    <template v-else-if="room.loaded">
      <!-- Focus mode: fullscreen ring, todos hidden -->
      <div v-if="focusMode" class="room-focus-page" data-room-page :data-room-sync="room.code">
        <div class="desk-room-bar">
          <a class="room-membership-pill" :href="`/r/${room.code}`">
            <span class="room-membership-dot" aria-hidden="true"></span>
            <span class="room-membership-label">Room</span>
            <span class="room-membership-name">{{ room.room.name }}</span>
          </a>
        </div>
        <section class="room-focus-shell">
          <p class="label label-accent">Focus · session {{ timer.currentSession }} of {{ timer.totalSessions }}</p>
          <p class="room-focus-name">{{ room.room.name }}</p>
          <RingCountdown :key="`${timer.runId}-focus-mode`" :ends-at="endsAt" :total-seconds="totalSeconds" ring-class="running room-focus-ring" aria-label="Focus countdown" @expired="expired" />
          <ParticipantStack v-if="timer.participants?.length > 1" :members="timer.participants" />
          <p class="label">{{ timer.participants?.length === 1 ? 'Focusing solo' : `${timer.participants.length} focusing` }}</p>
          <form @submit.prevent="room.action('timer-leave')">
            <button type="submit" class="btn-ghost timer-cancel">Leave focus block</button>
          </form>
        </section>
      </div>

      <!-- Normal room shell -->
      <section v-else class="room-shell" data-room-page :data-room-sync="room.code">
        <div class="room-header-new">
          <div class="room-header-main">
            <div class="label label-accent room-eyebrow">
              <span class="room-eyebrow-label">Room ·</span>
              <span class="room-share" :class="{ 'is-copied': copied === 'header' }">
                <button type="button" class="copy-chip mono room-share-code" :aria-label="`Copy room code ${room.code}`" title="Copy room code" @click="copy(room.code, 'header')">{{ room.code }}</button>
                <button type="button" class="room-share-copy room-share-copy-icon" aria-label="Copy invite link" title="Copy invite link" @click="copy(inviteMessage(), 'header')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg></button>
                <span class="room-share-feedback" aria-live="polite">{{ copied === 'header' ? 'COPIED ✓' : '' }}</span>
              </span>
            </div>
            <div class="room-title-row">
              <h1 class="room-name-display" v-show="!renaming">{{ room.room.name }}</h1>
              <button type="button" class="room-rename-trigger" aria-label="Rename room" v-show="!renaming" @click="startRename"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M12 20h9M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg></button>
              <a class="label room-members-link room-members-inline" :href="`/r/${room.code}/members`" v-show="!renaming">{{ room.memberCount || 0 }} members</a>
            </div>
            <form class="room-rename-form" v-show="renaming" @submit.prevent="saveRename">
              <input name="name" v-model="renameDraft" aria-label="Room name" required @keydown.esc="renaming = false">
              <button type="submit" class="btn-primary btn-compact">Save</button>
              <button type="button" class="btn-ghost btn-compact room-rename-cancel" @click="renaming = false">Cancel</button>
            </form>
          </div>
          <div class="room-members-meta">
            <ParticipantStack v-if="timer && timer.participants?.length > 1" :members="timer.participants" small />
            <a class="label room-members-link" :href="`/r/${room.code}/members`">{{ room.memberCount || 0 }} members</a>
          </div>
        </div>

        <section class="grid two room-desk-grid">
          <div class="room-timer-column">
            <article v-if="timer" class="timer-card panel" :class="phase">
              <!-- lobby -->
              <template v-if="phase === 'lobby'">
                <p class="label label-accent">Starting · join now</p>
                <RingCountdown :key="`${timer.runId}-lobby`" :ends-at="endsAt" :total-seconds="30" ring-class="running room-active-ring" aria-label="Focus lobby countdown" @expired="expired" />
                <ParticipantStack v-if="timer.participants?.length" :members="timer.participants" small />
                <p class="label label-accent">{{ timer.participants?.length || 0 }} joined · in when it starts</p>
                <form v-if="!timer.participant" @submit.prevent="room.action('timer-join')"><button type="submit" class="btn-primary">Join this block</button></form>
                <form v-else @submit.prevent="room.action('timer-leave')"><button type="submit" class="btn-ghost timer-cancel">Leave this focus block</button></form>
              </template>

              <!-- focus (not participating, else focus mode above) -->
              <template v-else-if="phase === 'focus'">
                <p class="label label-accent">Focus · session {{ timer.currentSession }} of {{ timer.totalSessions }}</p>
                <RingCountdown :key="`${timer.runId}-focus`" :ends-at="endsAt" :total-seconds="totalSeconds" ring-class="running room-active-ring" @expired="expired" />
                <template v-if="!timer.participant">
                  <template v-if="room.waiting">
                    <p class="label label-accent">In for the break · you'll join automatically</p>
                    <form @submit.prevent="room.action('timer-leave')"><button type="submit" class="btn-ghost timer-cancel">Cancel</button></form>
                  </template>
                  <form v-else @submit.prevent="room.action('timer-join')"><button type="submit" class="btn-primary">Join at the break</button></form>
                </template>
              </template>

              <!-- break -->
              <template v-else>
                <p class="label label-warn">
                  {{ timer.breakPending ? 'Break ready · set the length' : timer.paused ? 'Break paused' : 'Break · next block in' }}
                  <span v-if="!timer.breakPending" class="countdown mono" aria-live="polite">{{ String(Math.floor(seconds / 60)).padStart(2, '0') }}:{{ String(seconds % 60).padStart(2, '0') }}</span>
                </p>
                <div class="room-ready-time mono">{{ timer.focusMinutes }}:00</div>
                <template v-if="room.room.requireCheckin && timer.participant">
                  <form v-if="!timer.checkedIn" @submit.prevent="room.action('timer-checkin')">
                    <button type="submit" class="btn-primary">I'm here — check in for session {{ timer.currentSession + 1 }}</button>
                  </form>
                  <p v-else class="label label-accent">Checked in ✓ · in for session {{ timer.currentSession + 1 }}</p>
                </template>
                <form v-if="!timer.participant" @submit.prevent="room.action('timer-join')"><button type="submit" class="btn-primary">Join this block</button></form>
                <form v-if="timer.paused" class="inline-form break-length-form" @submit.prevent="room.action('timer-break-length', { minutes: String(breakLength || timer.breakMinutes) })">
                  <label>Break minutes <input type="number" name="minutes" min="1" max="60" :value="timer.breakMinutes" @input="breakLength = Number($event.target.value)"></label>
                  <button type="submit" class="btn-primary btn-compact">Start break</button>
                </form>
                <form v-if="!timer.breakPending" @submit.prevent="room.action(timer.paused ? 'timer-resume' : 'timer-pause')">
                  <button type="submit" class="btn-ghost">{{ timer.paused ? 'Resume break' : 'Pause break' }}</button>
                </form>
                <form v-if="!room.room.requireCheckin" @submit.prevent="room.action('timer-skip-break')"><button type="submit" class="btn-ghost timer-cancel">Skip break</button></form>
                <form v-if="timer.participant" @submit.prevent="room.action('timer-leave')"><button type="submit" class="btn-ghost timer-cancel">Leave this focus block</button></form>
              </template>

              <template v-if="phase !== 'lobby'">
                <ParticipantStack v-if="timer.participants?.length" :members="timer.participants" small :checkin="room.room.requireCheckin && phase === 'break'" />
                <p class="label">{{ timer.participants?.length === 1 ? 'Focusing solo' : `${timer.participants?.length || 0} focusing` }}</p>
              </template>
            </article>

            <!-- idle / ready -->
            <article v-else class="timer-card panel idle ready-card">
              <p class="label label-accent">Ready · {{ room.room.autoSessions }} × {{ room.room.focusMinutes }}/{{ room.room.breakMinutes }}</p>
              <div class="room-ready-time mono">{{ room.room.focusMinutes }}:00</div>
              <form :data-room-name="room.room.name" @submit.prevent="confirmStart = true"><button type="submit" class="btn-primary big-action">Start focus block</button></form>
              <template v-if="room.waiting">
                <p class="label label-accent">Waiting · you'll join automatically when someone starts</p>
                <form @submit.prevent="room.action('timer-leave')"><button type="submit" class="btn-ghost timer-cancel">Stop waiting</button></form>
              </template>
              <template v-else>
                <form @submit.prevent="room.action('timer-join')"><button type="submit" class="btn-ghost">Join when it starts</button></form>
                <p class="muted room-ready-hint">Start alone or with others — or wait here and get pulled in automatically when anyone starts.</p>
              </template>
            </article>

            <template v-if="!timer">
              <details class="room-details panel" data-room-section="settings" :open="sections.settings.value" @toggle="toggleSection('settings', $event.target.open)">
                <summary><span class="label">Timer settings · {{ room.room.autoSessions }}×{{ room.room.focusMinutes }}/{{ room.room.breakMinutes }}</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
                <form class="settings stack" @submit.prevent="saveSettings">
                  <label>Focus <input type="number" name="focus_minutes" min="5" max="180" :value="room.room.focusMinutes"></label>
                  <label>Break <input type="number" name="break_minutes" min="1" max="60" :value="room.room.breakMinutes"></label>
                  <label>Sessions <input type="number" name="auto_sessions" min="1" max="12" :value="room.room.autoSessions"></label>
                  <div class="settings-auto-roll">
                    <div>
                      <strong>Auto-start breaks</strong>
                      <p class="muted">When off, the break waits after each focus block so the room can adjust its length and start it together.</p>
                    </div>
                    <label class="toggle-control">
                      <input type="checkbox" name="auto_roll" value="1" :checked="room.room.autoRoll" aria-label="Auto-start breaks">
                      <span aria-hidden="true"></span>
                    </label>
                  </div>
                  <div class="settings-auto-roll">
                    <div>
                      <strong>Session check-in</strong>
                      <p class="muted">Everyone taps “I'm here” during each break to stay in the next session. No-shows are dropped from the block.</p>
                    </div>
                    <label class="toggle-control">
                      <input type="checkbox" name="require_checkin" value="1" :checked="room.room.requireCheckin" aria-label="Session check-in">
                      <span aria-hidden="true"></span>
                    </label>
                  </div>
                  <button type="submit" class="btn-primary btn-compact">Save</button>
                </form>
              </details>
              <details class="room-details panel" data-room-section="share" :open="sections.share.value" @toggle="toggleSection('share', $event.target.open)">
                <summary><span class="label">Share room</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
                <p class="room-share room-share-block" :class="{ 'is-copied': copied === 'share' }">
                  <button type="button" class="copy-chip mono room-share-code" @click="copy(inviteMessage(), 'share')">junkie.app/r/{{ room.code }}</button>
                  <button type="button" class="btn-ghost btn-compact room-share-copy" @click="copy(inviteMessage(), 'share')">Copy</button>
                  <span class="room-share-feedback" aria-live="polite">{{ copied === 'share' ? 'COPIED ✓' : '' }}</span>
                </p>
              </details>
              <details class="room-details panel" data-room-section="controls" :open="sections.controls.value" @toggle="toggleSection('controls', $event.target.open)">
                <summary><span class="label">Room controls</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
                <p class="muted">During focus, late joiners can watch the countdown but cannot enter the active block. During break, anyone can join the next block.</p>
                <form v-if="room.isCreator" @submit.prevent="deleteRoom">
                  <button type="submit" class="btn-danger">Delete room</button>
                </form>
              </details>
            </template>
          </div>

          <article class="panel room-todos-panel">
            <div class="panel-title">
              <h2>Room todos</h2>
              <span class="label label-warn">Public to the room</span>
            </div>
            <form class="inline-form todo-add-form" @submit.prevent="addTodo">
              <input name="text" placeholder="What are you working on?" required v-model="draft">
              <button type="submit" class="todo-add-plus" aria-label="Add task" :disabled="draft.trim() === ''">+</button>
            </form>
            <TodoGroups :room-code="room.code" :user-name="auth.user?.displayName" :mine="room.mine" :others="room.others" />
          </article>
        </section>
      </section>

      <StartConfirmModal
        v-if="confirmStart"
        :message="`You’re about to start a focus block for ${room.room.name}.`"
        @confirm="reallyStart"
        @cancel="confirmStart = false"
      />
      <JoinPromptModal
        v-if="room.joinPrompt"
        :prompt="room.joinPrompt"
        @join="room.action('timer-join')"
        @expired="room.refresh()"
        @dismiss="room.joinPrompt = null"
      />
    </template>
  </AppShell>
</template>
