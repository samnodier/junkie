<script setup>
// Set-new-password page (/reset-password/{token}). The token arrives by
// Discord DM (/junkie reset-password) or from the owner via the admin space.
// The context API peeks the token; redeeming stays single-use server-side.
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import PasswordField from '@/components/PasswordField.vue';

const route = useRoute();
const token = String(route.params.token || '');
const username = ref('');
const invalid = ref(false);
const password = ref('');
const confirm = ref('');
const error = ref('');
const submitting = ref(false);

onMounted(async () => {
  try {
    const res = await fetch(`/api/password-reset-context/${encodeURIComponent(token)}`, {
      credentials: 'same-origin',
    });
    const data = await res.json();
    if (data.username) username.value = data.username;
    else invalid.value = true;
  } catch {
    invalid.value = true;
  }
});

async function submit() {
  if (submitting.value) return;
  submitting.value = true;
  error.value = '';
  try {
    const res = await fetch(`/api/password-reset/${encodeURIComponent(token)}`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ new_password: password.value, confirm_password: confirm.value }),
    });
    const data = await res.json();
    if (!res.ok) {
      error.value = data.error || 'Something went wrong. Try again.';
      return;
    }
    location.href = data.next || '/login';
  } catch {
    error.value = 'Could not reach the server. Check your connection and try again.';
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <AppShell>
    <section class="auth-card">
      <div class="auth-card-top">
        <a class="auth-back" href="/login" aria-label="Back to sign in">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 18l-6-6 6-6"/></svg>
        </a>
        <div class="auth-brand"><span class="brand-mark">j</span></div>
      </div>
      <template v-if="invalid">
        <h1 class="auth-title">Reset link expired</h1>
        <p class="muted">This reset link is invalid, already used, or older than 30 minutes. Run <span class="mono">/junkie reset-password</span> again in the <span class="mono">#junkie-bot</span> channel of the <a href="https://discord.gg/qEEzdXQHtK" target="_blank" rel="noopener">junkie Discord server</a> to get a fresh one, or ask the admin there.</p>
        <p class="muted auth-switch"><router-link to="/login">Back to sign in</router-link></p>
      </template>
      <template v-else-if="username">
        <h1 class="auth-title">Set a new password</h1>
        <p class="context-banner">Choosing a new password for <strong>{{ username }}</strong>. All signed-in devices will be signed out.</p>
        <p v-if="error" class="notice notice-error">{{ error }}</p>
        <form class="stack auth-form" @submit.prevent="submit">
          <label>New password
            <PasswordField v-model="password" name="new_password" autocomplete="new-password" />
          </label>
          <label>Confirm new password
            <PasswordField v-model="confirm" name="confirm_password" autocomplete="new-password" />
          </label>
          <button type="submit" class="btn-primary" :disabled="submitting">Save new password</button>
        </form>
      </template>
    </section>
  </AppShell>
</template>
