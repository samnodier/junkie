<script setup>
// Sign in / sign up card, ported from the Go "login" template. Mode comes
// from ?mode=signup, the ?next= round-trip matches the legacy handlers, and
// success does a full navigation so legacy destinations keep working.
import { computed, ref, watchEffect } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import PasswordField from '@/components/PasswordField.vue';

const route = useRoute();
const signup = computed(() => route.query.mode === 'signup');
const next = computed(() => {
  const n = String(route.query.next || '');
  return n.startsWith('/') && !n.startsWith('//') && !n.startsWith('/\\') ? n : '/';
});
const notice = computed(() => String(route.query.notice || ''));

const username = ref('');
const password = ref('');
const error = ref('');
const banner = ref('');
const submitting = ref(false);

watchEffect(async () => {
  banner.value = '';
  const params = new URLSearchParams();
  if (next.value !== '/') params.set('next', next.value);
  if (signup.value) params.set('mode', 'signup');
  try {
    const res = await fetch(`/api/auth-context?${params}`, { credentials: 'same-origin' });
    if (res.ok) banner.value = (await res.json()).banner || '';
  } catch {
    /* banner is decorative; the form still works without it */
  }
});

async function submit() {
  if (submitting.value) return;
  submitting.value = true;
  error.value = '';
  try {
    const body = new URLSearchParams({
      username: username.value,
      password: password.value,
      next: next.value,
    });
    const res = await fetch(signup.value ? '/api/signup' : '/api/login', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body,
    });
    const data = await res.json();
    if (!res.ok) {
      error.value = data.error || 'Something went wrong. Try again.';
      return;
    }
    location.href = data.next || '/';
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
        <a class="auth-back" href="/" aria-label="Back to home">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 18l-6-6 6-6"/></svg>
        </a>
        <div class="auth-brand"><span class="brand-mark">j</span></div>
      </div>
      <h1 class="auth-title">{{ signup ? 'Start focusing' : 'Welcome back' }}</h1>
      <p v-if="banner" class="context-banner">{{ banner }}</p>
      <p v-if="error" class="notice notice-error">{{ error }}</p>
      <p v-if="notice && !error" class="notice-ok" role="status">{{ notice }}</p>
      <form class="stack auth-form" @submit.prevent="submit">
        <label>Username <input name="username" autocomplete="username" v-model="username" required></label>
        <label>Password
          <PasswordField
            v-model="password"
            name="password"
            :autocomplete="signup ? 'new-password' : 'current-password'"
          />
        </label>
        <button type="submit" class="btn-primary" :disabled="submitting">
          {{ signup ? 'Create account' : 'Sign in' }}
        </button>
      </form>
      <p v-if="signup" class="muted auth-switch">
        Already have an account?
        <router-link :to="{ path: '/login', query: next !== '/' ? { next } : {} }">Sign in</router-link>
      </p>
      <p v-else class="muted auth-switch">
        New here?
        <router-link :to="{ path: '/login', query: { mode: 'signup', ...(next !== '/' ? { next } : {}) } }">Create an account</router-link>
      </p>
    </section>
  </AppShell>
</template>
