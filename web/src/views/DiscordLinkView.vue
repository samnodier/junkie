<script setup>
// Discord link confirmation (/discord/link/{token}), ported from the
// "Link Discord" template branch. The context API peeks the token; redeeming
// it stays with the legacy POST so single-use semantics are unchanged.
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import AppShell from '@/components/AppShell.vue';
import ConfirmCard from '@/components/ConfirmCard.vue';

const route = useRoute();
const token = String(route.params.token || '');
const discordUsername = ref('');

onMounted(async () => {
  try {
    const res = await fetch(`/api/discord-link-context/${encodeURIComponent(token)}`, {
      credentials: 'same-origin',
    });
    const data = await res.json();
    if (data.redirect) {
      location.href = data.redirect;
      return;
    }
    discordUsername.value = data.discordUsername;
  } catch {
    location.href = '/profile';
  }
});
</script>

<template>
  <AppShell>
    <ConfirmCard v-if="discordUsername" eyebrow="Discord link">
      <h1>Link <span class="mono">@{{ discordUsername }}</span> to your account?</h1>
      <p class="muted">The junkie bot's commands run by this Discord account will act as you: joining runs, adding todos, showing your stats. Only continue if this is your Discord account.</p>
      <form class="stack room-invite-actions" method="post" :action="`/discord/link/${encodeURIComponent(token)}`">
        <button type="submit">Link Discord account</button>
        <a href="/profile" class="ghost btn">Not now</a>
      </form>
    </ConfirmCard>
  </AppShell>
</template>
