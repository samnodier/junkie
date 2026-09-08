<script setup>
// Private focus screen (/solo) — the solo twin of the room's fullscreen focus
// page. The desk shows the private ring beside the todos panel, which is the
// right shape for picking a length and the wrong one for actually focusing:
// the list you're avoiding sits next to the clock and the topbar only fades.
// Here the same timer gets the screen to itself, with the topbar hidden
// outright during focus (composables/focusChrome.js) and the ring as the way
// to bring it back. Reached from the "Private · {name}'s room" pill on the
// desk; the pill here goes back.
import { computed, onMounted, onUnmounted, watchEffect } from 'vue';
import { useAuthStore } from '@/stores/auth';
import { useDeskStore } from '@/stores/desk';
import { requestPermission, onTimerEnd } from '@/lib/notify';
import { useWakeLock } from '@/composables/wakeLock';
import { useTimerDeadline, expireNudge } from '@/composables/timerDeadline';
import { useFocusChrome } from '@/composables/focusChrome';
import AppShell from '@/components/AppShell.vue';
import RingCountdown from '@/components/RingCountdown.vue';
import RingIdle from '@/components/RingIdle.vue';
import BreakReadyRing from '@/components/BreakReadyRing.vue';
import PipTimer from '@/components/PipTimer.vue';
import { askConfirm } from '@/composables/confirm';

const auth = useAuthStore();
const desk = useDeskStore();
const wakeLock = useWakeLock();

const timer = computed(() => desk.soloTimer);
const focusing = computed(() => timer.value?.phase === 'focus');
const { endsAt } = useTimerDeadline(timer);
const phaseKey = () => `${timer.value?.phase || 'idle'}:${timer.value?.breakPending ? 1 : 0}`;

function start(minutes) {
  requestPermission();
  desk.soloStart(minutes);
}
async function cancel() {
  const ok = await askConfirm({
    title: 'End this focus session?',
    body: "It won't count toward your map.",
    confirmLabel: 'End session',
    danger: true,
  });
  if (!ok) return;
  desk.soloCancel();
}
function expired(phase) {
  onTimerEnd(phase);
  expireNudge(() => desk.refresh(), phaseKey);
}

const { toggle: toggleChrome } = useFocusChrome(focusing);

// The desk sets focus-active from its own watcher and clears it in its
// unmounted hook, which lands after this view has mounted on an in-app nav.
// Re-asserting on mount keeps a handover mid-focus from leaving it off.
function syncFocusActive() {
  document.body.classList.toggle('focus-active', focusing.value);
}
watchEffect(() => {
  syncFocusActive();
  if (timer.value) wakeLock.want();
  else wakeLock.release();
});

onMounted(async () => {
  // Grid-free the whole time this screen is open, not only during the block —
  // same reasoning as the temporary focus rooms.
  document.body.classList.add('focus-room-open');
  syncFocusActive();
  await desk.refresh();
  desk.connect(auth.user?.id, { rooms: false });
});
onUnmounted(() => {
  desk.disconnect();
  document.body.classList.remove('focus-active', 'focus-room-open');
});
</script>

<template>
  <AppShell show-menu next="/solo" :rooms="desk.rooms">
    <div v-if="desk.loaded" class="room-focus-page" data-solo-page>
      <div class="desk-room-bar">
        <RouterLink class="room-membership-pill" to="/">
          <span class="room-membership-dot" aria-hidden="true"></span>
          <span class="room-membership-label">Private</span>
          <span class="room-membership-name">{{ auth.privateRoomName }}</span>
        </RouterLink>
      </div>

      <PipTimer>
        <section class="room-focus-shell">
          <!-- Ready: pick a length and go, same ring as the desk card -->
          <template v-if="!timer">
            <p class="label label-accent">Ready · private focus</p>
            <p class="room-focus-name">{{ auth.privateRoomName }}</p>
            <form class="circle-timer-form" @submit.prevent>
              <RingIdle :model-value="50" ring-class="room-focus-ring" aria-label="Set private focus duration" @submit="start" />
            </form>
            <p class="label desk-ring-hint">Scroll ±1 · buttons ±5 · tap ring to focus</p>
          </template>

          <!-- Focus: the distraction-free block -->
          <template v-else-if="timer.phase === 'focus'">
            <p class="label label-accent">Private focus</p>
            <p class="room-focus-name">{{ auth.privateRoomName }}</p>
            <RingCountdown key="solo-focus" :ends-at="endsAt" :total-seconds="timer.focusMinutes * 60" ring-class="running room-focus-ring focus-chrome-toggle" aria-label="Private focus countdown" @expired="expired('focus')" @click="toggleChrome" />
            <p class="label">Focusing solo</p>
            <button type="button" class="btn-ghost timer-cancel" @click="cancel">End early</button>
          </template>

          <!-- Break waiting to start -->
          <template v-else-if="timer.breakPending">
            <p class="label label-warn">Private break ready · set the length · tap to start</p>
            <p class="room-focus-name">{{ auth.privateRoomName }}</p>
            <BreakReadyRing :break-minutes="timer.breakMinutes" :room-name="auth.privateRoomName" ring-class="room-focus-ring" @start="desk.soloBreakStart" />
            <button type="button" class="btn-ghost timer-cancel" @click="desk.soloBreakSkip()">Skip break</button>
          </template>

          <!-- Break running -->
          <template v-else>
            <p class="label label-warn">Private break</p>
            <p class="room-focus-name">{{ auth.privateRoomName }}</p>
            <RingCountdown key="solo-break" :ends-at="endsAt" :total-seconds="timer.breakMinutes * 60" ring-class="break-running breather room-focus-ring" aria-label="Private break countdown" @expired="expired('break')" />
            <button type="button" class="btn-ghost timer-cancel" @click="desk.soloBreakSkip()">Skip break</button>
          </template>
        </section>
      </PipTimer>
    </div>
  </AppShell>
</template>
