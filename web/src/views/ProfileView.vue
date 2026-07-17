<script setup>
// Profile page, ported from the "Profile" template branch (guest + user
// variants). Mutating forms still post to the legacy endpoints, which
// redirect back to /profile?error=/?notice= — the SPA reads those params for
// the banners, exactly like the Go handler did.
import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import AppShell from '@/components/AppShell.vue';
import HeatmapChart from '@/components/HeatmapChart.vue';
import { buildGuestHeatmap } from '@/lib/guestActivity';
import { useConnectLink } from '@/composables/connectLink';

const NOTIFY_KEY = 'junkie:roomInviteNotifications';

const route = useRoute();
const auth = useAuthStore();
const profile = ref(null);
const guestHeatmap = ref(null);
const error = ref(String(route.query.error || ''));
const notice = ref(String(route.query.notice || ''));

const focusHours = computed(() =>
  profile.value ? Math.floor((profile.value.heatmap.totalMinutes + 30) / 60) : 0
);

// Room invite notifications preference (same key/semantics as notifications.js).
const invitesOn = ref(localStorage.getItem(NOTIFY_KEY) !== 'disabled');
function setInvites(on) {
  invitesOn.value = on;
  localStorage.setItem(NOTIFY_KEY, on ? 'enabled' : 'disabled');
  if (on && 'Notification' in window && Notification.permission === 'default') {
    try {
      Notification.requestPermission().catch(() => {});
    } catch {
      /* older Safari throws on the promise form */
    }
  }
}

const { label: connectLabel, copy: copyConnectLink } = useConnectLink();

// Avatar upload: center-crop to a square, shrink to 128px, re-encode as JPEG
// before upload — same client-side downscale as the legacy page.
const avatarInput = ref(null);
async function submitAvatar() {
  const file = avatarInput.value?.files?.[0];
  if (!file) return;
  try {
    const bmp = await createImageBitmap(file);
    const size = 128;
    const canvas = document.createElement('canvas');
    canvas.width = size;
    canvas.height = size;
    const ctx = canvas.getContext('2d');
    const side = Math.min(bmp.width, bmp.height);
    ctx.drawImage(bmp, (bmp.width - side) / 2, (bmp.height - side) / 2, side, side, 0, 0, size, size);
    const blob = await new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', 0.85));
    if (!blob) throw new Error('encode failed');
    const fd = new FormData();
    fd.append('avatar', blob, 'avatar.jpg');
    const resp = await fetch('/profile/avatar', { method: 'POST', body: fd, credentials: 'same-origin' });
    location.href = resp.redirected ? resp.url : '/profile';
  } catch {
    location.href = '/profile?error=' + encodeURIComponent("That image can't be used. Try a different one.");
  }
}

function confirmDelete(event) {
  if (!confirm('Permanently delete your account and all its data? This cannot be undone.')) {
    event.preventDefault();
  }
}

// Native file inputs can't be styled, so a hidden input backs a real button
// and the chosen filename is echoed next to it.
const avatarFileName = ref('');
function onAvatarPicked() {
  avatarFileName.value = avatarInput.value?.files?.[0]?.name || '';
}

const usernameDraft = ref('');

// Header nudge for accounts without Discord linked: without it there's no
// self-service password reset, so the blinking badge walks them to the
// Discord section that explains how to connect.
const discordSection = ref(null);
const discordFlash = ref(false);
function scrollToDiscord() {
  discordSection.value?.scrollIntoView({ behavior: 'smooth', block: 'center' });
  discordFlash.value = true;
  setTimeout(() => (discordFlash.value = false), 2500);
}

onMounted(async () => {
  if (!auth.isAuthed) {
    guestHeatmap.value = buildGuestHeatmap();
    return;
  }
  usernameDraft.value = auth.user.username;
  try {
    const res = await fetch('/api/profile', { credentials: 'same-origin' });
    if (res.ok) profile.value = await res.json();
  } catch {
    /* the static parts of the page still render */
  }
});
</script>

<template>
  <AppShell show-menu next="/profile">
    <!-- Guest variant: everything lives on this device. -->
    <section v-if="!auth.isAuthed" class="panel profile-page">
      <div class="panel-title">
        <h1>Profile</h1>
        <span>Stored on this device</span>
      </div>
      <div id="guest-profile-work-map">
        <HeatmapChart v-if="guestHeatmap" :heatmap="guestHeatmap" />
      </div>
      <p class="muted legal-links"><a href="/terms">Terms</a> · <a href="/privacy">Privacy</a> · <a href="https://github.com/samnodier/junkie" target="_blank" rel="noopener">GitHub</a></p>
    </section>

    <!-- Signed-in variant -->
    <section v-else class="profile-page profile-stack">
      <header class="profile-head">
        <h1>Profile</h1>
        <span class="profile-title-actions">
          <button
            v-if="profile && !profile.discord.linked"
            type="button"
            class="discord-nudge"
            title="Discord isn't connected — without it you can't reset a forgotten password"
            aria-label="Discord isn't connected — see how to set up password recovery"
            @click="scrollToDiscord"
          >
            <svg viewBox="0 0 127 96" fill="currentColor" aria-hidden="true" width="20" height="15"><path d="M107.7 8.07A105.15 105.15 0 0 0 81.47 0a72.06 72.06 0 0 0-3.36 6.83 97.68 97.68 0 0 0-29.11 0A72.37 72.37 0 0 0 45.64 0a105.89 105.89 0 0 0-26.25 8.09C2.79 32.65-1.71 56.6.54 80.21a105.73 105.73 0 0 0 32.17 16.15 77.7 77.7 0 0 0 6.89-11.11 68.42 68.42 0 0 1-10.85-5.18c.91-.66 1.8-1.34 2.66-2a75.57 75.57 0 0 0 64.32 0c.87.71 1.76 1.39 2.66 2a68.68 68.68 0 0 1-10.87 5.19 77 77 0 0 0 6.89 11.1 105.25 105.25 0 0 0 32.19-16.14c2.64-27.38-4.51-51.11-18.9-72.15ZM42.45 65.69C36.18 65.69 31 60 31 53s5-12.74 11.43-12.74S54 46 53.89 53s-5.05 12.69-11.44 12.69Zm42.24 0C78.41 65.69 73.25 60 73.25 53s5-12.74 11.44-12.74S96.23 46 96.12 53s-5.04 12.69-11.43 12.69Z"/></svg>
            <span class="nudge-badge" aria-hidden="true">!</span>
          </button>
          <a href="/connections" class="btn btn-ghost btn-compact">Connections</a><span>{{ auth.user.displayName }}</span>
        </span>
      </header>
      <p v-if="error" class="notice-error" role="alert">{{ error }}</p>
      <p v-if="notice" class="notice-ok" role="status">{{ notice }}</p>

      <article class="panel profile-card">
        <p class="eyebrow">Activity</p>
        <div class="profile-stats">
          <div>
            <strong>{{ focusHours }}</strong>
            <span>focus hours</span>
          </div>
          <div>
            <strong>{{ profile ? profile.roomsCount : 0 }}</strong>
            <span>rooms joined</span>
          </div>
        </div>
        <HeatmapChart v-if="profile" :heatmap="profile.heatmap" />
      </article>

      <article class="panel profile-card">
        <p class="eyebrow">Preferences</p>
        <div class="profile-preference">
          <div>
            <strong>Room invite notifications</strong>
            <p class="muted">Notify me when a room starts a focus lobby while junkie is in the background.</p>
          </div>
          <label class="toggle-control">
            <input
              type="checkbox"
              id="room-invite-notifications"
              aria-label="Room invite notifications"
              :checked="invitesOn"
              @change="setInvites($event.target.checked)"
            >
            <span aria-hidden="true"></span>
          </label>
        </div>
        <div class="profile-preference profile-avatar-pref">
          <div>
            <strong>Profile picture</strong>
            <p class="muted">Shown next to your todos in rooms. Pictures are shrunk to a small square before upload, so any photo works.</p>
          </div>
          <span class="profile-avatar"><img v-if="auth.avatarURL" class="avatar-img" :src="auth.avatarURL" alt="Your profile picture"><template v-else>{{ auth.initial }}</template></span>
        </div>
        <div class="profile-avatar-actions">
          <form id="avatar-form" class="avatar-pick-row" @submit.prevent="submitAvatar">
            <input type="file" name="avatar" id="avatar-input" ref="avatarInput" accept="image/*" class="visually-hidden" aria-label="Choose profile picture" @change="onAvatarPicked">
            <button type="button" class="btn-ghost btn-compact" @click="avatarInput.click()">Choose picture</button>
            <span class="avatar-file-name" aria-live="polite">{{ avatarFileName || 'No file chosen' }}</span>
          </form>
          <div class="avatar-action-row">
            <button type="submit" form="avatar-form" class="btn-primary btn-compact" :disabled="!avatarFileName">Upload picture</button>
            <form v-if="auth.avatarURL" method="post" action="/profile/avatar/remove" class="avatar-remove-form">
              <button type="submit" class="btn-ghost btn-compact">Remove picture</button>
            </form>
          </div>
        </div>
      </article>

      <article class="panel profile-card">
        <p class="eyebrow">Account</p>
        <template v-if="auth.user.role !== 'owner'">
          <div class="profile-preference">
            <div>
              <strong>Username</strong>
              <p class="muted">Changes how you sign in and how your name appears to others. Your focus history stays with your account.</p>
            </div>
          </div>
          <form class="profile-username-form" method="post" action="/profile/username">
            <label>Username <input name="username" v-model="usernameDraft" autocomplete="username" required minlength="2" maxlength="32"></label>
            <button type="submit" class="btn-primary btn-compact">Save username</button>
          </form>
        </template>
        <div class="profile-preference profile-password">
          <div>
            <strong>Change password</strong>
            <p class="muted">Updating your password signs out all other devices, so anyone else using your account loses access.</p>
          </div>
        </div>
        <form class="profile-password-form" method="post" action="/profile/password">
          <label>Current password <input type="password" name="current_password" autocomplete="current-password" required></label>
          <label>New password <input type="password" name="new_password" autocomplete="new-password" required minlength="8"></label>
          <label>Confirm new password <input type="password" name="confirm_password" autocomplete="new-password" required minlength="8"></label>
          <button type="submit" class="btn-primary btn-compact">Update password</button>
        </form>
      </article>

      <article class="panel profile-card">
        <p class="eyebrow">Connections</p>
        <div class="profile-preference">
          <div>
            <strong>Share your focus map</strong>
            <p class="muted">Share a one-time link to connect with someone. Connections see each other's focus heatmaps — nothing else, and nobody else sees your profile at all. <a href="/connections">See your connections</a>.</p>
          </div>
          <button type="button" id="connect-link-copy" class="btn-ghost btn-compact" @click="copyConnectLink">{{ connectLabel }}</button>
        </div>
        <div class="profile-preference" ref="discordSection" :class="{ 'flash-highlight': discordFlash }">
          <div>
            <strong><svg class="discord-mark" viewBox="0 0 127 96" fill="currentColor" aria-hidden="true" width="18" height="14" style="vertical-align:-2px"><path d="M107.7 8.07A105.15 105.15 0 0 0 81.47 0a72.06 72.06 0 0 0-3.36 6.83 97.68 97.68 0 0 0-29.11 0A72.37 72.37 0 0 0 45.64 0a105.89 105.89 0 0 0-26.25 8.09C2.79 32.65-1.71 56.6.54 80.21a105.73 105.73 0 0 0 32.17 16.15 77.7 77.7 0 0 0 6.89-11.11 68.42 68.42 0 0 1-10.85-5.18c.91-.66 1.8-1.34 2.66-2a75.57 75.57 0 0 0 64.32 0c.87.71 1.76 1.39 2.66 2a68.68 68.68 0 0 1-10.87 5.19 77 77 0 0 0 6.89 11.1 105.25 105.25 0 0 0 32.19-16.14c2.64-27.38-4.51-51.11-18.9-72.15ZM42.45 65.69C36.18 65.69 31 60 31 53s5-12.74 11.43-12.74S54 46 53.89 53s-5.05 12.69-11.44 12.69Zm42.24 0C78.41 65.69 73.25 60 73.25 53s5-12.74 11.44-12.74S96.23 46 96.12 53s-5.04 12.69-11.43 12.69Z"/></svg> Discord</strong>
            <p v-if="profile?.discord.linked" class="muted">Connected as <span class="mono">@{{ profile.discord.username }}</span>. Use <span class="mono">/junkie</span> commands in servers running the junkie bot — including <span class="mono">/junkie reset-password</span> if you ever forget your password. <a href="https://discord.com/oauth2/authorize?client_id=1526585859044933684&scope=bot+applications.commands&permissions=2048" target="_blank" rel="noopener">Add the bot to a server</a>.</p>
            <p v-else class="muted">Discord is also how you reset your password if you ever forget it — without it you'll need to track down the admin. To connect: <a href="https://discord.gg/qEEzdXQHtK" target="_blank" rel="noopener">join the junkie Discord server</a>, run <span class="mono">/junkie link</span> in the <span class="mono">#junkie-bot</span> channel, and open the link it gives you. (Already run the bot in your own server? <span class="mono">/junkie link</span> works there too.)</p>
          </div>
          <form v-if="profile?.discord.linked" method="post" action="/profile/discord/unlink">
            <button type="submit" class="btn-ghost btn-compact">Disconnect</button>
          </form>
        </div>
      </article>

      <article v-if="auth.user.role !== 'owner'" class="panel profile-card profile-card-danger">
        <p class="eyebrow eyebrow-danger">Danger zone</p>
        <div class="profile-preference profile-danger">
          <div>
            <strong>Delete account</strong>
            <p class="muted">Permanently deletes your account, any rooms you created, your todos, and your activity history. This cannot be undone.</p>
          </div>
        </div>
        <form class="profile-delete-form" method="post" action="/profile/delete" @submit="confirmDelete">
          <label>Confirm your password <input type="password" name="password" autocomplete="current-password" required></label>
          <button type="submit" class="btn-danger btn-compact">Delete account</button>
        </form>
      </article>

      <p class="muted legal-links"><a href="/terms">Terms</a> · <a href="/privacy">Privacy</a> · <a href="https://github.com/samnodier/junkie" target="_blank" rel="noopener">GitHub</a></p>
    </section>
  </AppShell>
</template>
