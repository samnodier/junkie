<script setup>
// Terminal sign-in approval (/cli). The terminal client shows a short code;
// this is where it gets approved, in a browser that is already signed in.
//
// The approval is always an explicit click — arriving here with ?code= fills
// the box and says what will happen, and nothing more. A page that approved
// on load would make the URL itself the credential, which is exactly what the
// flow is designed to avoid.
import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { useAuthStore } from '@/stores/auth';
import AppShell from '@/components/AppShell.vue';
import ConfirmCard from '@/components/ConfirmCard.vue';

const route = useRoute();
const auth = useAuthStore();

const code = ref(String(route.query.code || ''));
const error = ref('');
const done = ref(false);
const busy = ref(false);

const displayName = computed(() => auth.user?.displayName || auth.user?.username || 'your account');

// Formats as the terminal prints it while leaving what someone types alone.
function onInput(event) {
  const raw = event.target.value.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8);
  code.value = raw.length > 4 ? `${raw.slice(0, 4)}-${raw.slice(4)}` : raw;
}

async function approve() {
  if (busy.value) return;
  busy.value = true;
  error.value = '';
  try {
    const res = await fetch('/api/cli/link/approve', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ code: code.value }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      error.value = data.error || 'Could not approve that code.';
      return;
    }
    done.value = true;
  } catch {
    error.value = 'Could not reach the server. Check your connection and try again.';
  } finally {
    busy.value = false;
  }
}

onMounted(async () => {
  if (!code.value) return;
  // Peek, so a code that has already expired says so before anyone clicks.
  try {
    const res = await fetch(`/api/cli/link/context?code=${encodeURIComponent(code.value)}`, {
      credentials: 'same-origin',
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) error.value = data.error || '';
    else if (data.approved) done.value = true;
  } catch {
    /* the approve call will report it properly */
  }
});
</script>

<template>
  <AppShell>
    <ConfirmCard eyebrow="Terminal sign-in">
      <template v-if="done">
        <h1>Terminal signed in</h1>
        <p class="muted">
          Your terminal is signing in as <strong>{{ displayName }}</strong> — it should say so
          within a couple of seconds. You can close this tab.
        </p>
        <p class="muted cli-link-note">
          It stays signed in for 30 days. <code>junkie logout</code> ends it immediately, and so
          does changing your password.
        </p>
      </template>

      <template v-else>
        <h1>Sign this terminal in as {{ displayName }}?</h1>
        <p class="muted">
          Check the code below matches the one your terminal is showing. Only approve a code you
          started yourself — it gives that terminal full access to your account.
        </p>

        <form class="stack cli-link-form" @submit.prevent="approve">
          <label class="cli-link-field">
            <span class="label">Code from your terminal</span>
            <input
              :value="code"
              class="mono cli-link-code"
              type="text"
              inputmode="latin"
              autocomplete="off"
              autocapitalize="characters"
              spellcheck="false"
              placeholder="XXXX-XXXX"
              aria-label="Code from your terminal"
              @input="onInput"
            >
          </label>
          <p v-if="error" class="form-error">{{ error }}</p>
          <button type="submit" :disabled="busy || code.length !== 9">
            {{ busy ? 'Approving…' : 'Approve terminal' }}
          </button>
          <a href="/" class="ghost btn">Cancel</a>
        </form>
      </template>
    </ConfirmCard>
  </AppShell>
</template>
