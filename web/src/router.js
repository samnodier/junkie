import { createRouter, createWebHistory } from 'vue-router';
import { useAuthStore } from '@/stores/auth';

// Routes are added here as each screen is ported off htmx. The Go server
// decides which paths serve the SPA index vs. the legacy template, so a route
// existing here is inert until the server flips that path.
const routes = [
  {
    path: '/__vue',
    name: 'pipeline-check',
    component: () => import('./views/PipelineCheckView.vue'),
  },
  {
    // The Go handler serves the SPA here for guests only until the
    // logged-in desk is ported.
    path: '/',
    name: 'desk',
    component: () => import('./views/DeskView.vue'),
  },
  {
    path: '/dashboard',
    name: 'desk-dashboard',
    component: () => import('./views/DeskView.vue'),
  },
  {
    path: '/login',
    name: 'login',
    component: () => import('./views/AuthView.vue'),
  },
  {
    path: '/privacy',
    name: 'privacy',
    component: () => import('./views/PrivacyView.vue'),
  },
  {
    path: '/terms',
    name: 'terms',
    component: () => import('./views/TermsView.vue'),
  },
  {
    path: '/join/confirm',
    name: 'join-confirm',
    component: () => import('./views/JoinConfirmView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/discord/link/:token',
    name: 'discord-link',
    component: () => import('./views/DiscordLinkView.vue'),
    meta: { requiresAuth: true },
  },
  {
    // Public: guests get the local-only profile variant, like the Go page.
    path: '/profile',
    name: 'profile',
    component: () => import('./views/ProfileView.vue'),
  },
  {
    path: '/connections',
    name: 'connections',
    component: () => import('./views/ConnectionsView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/r/:code',
    name: 'room',
    component: () => import('./views/RoomView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/r/:code/members',
    name: 'room-members',
    component: () => import('./views/RoomMembersView.vue'),
    meta: { requiresAuth: true },
  },
  {
    // Catch-all for /{username}; static routes above always win in vue-router
    // scoring, and the Go handler 404s invalid names before the SPA loads.
    path: '/:username([a-z0-9._-]{2,32})',
    name: 'public-profile',
    component: () => import('./views/PublicProfileView.vue'),
    meta: { requiresAuth: true },
  },
];

const router = createRouter({
  history: createWebHistory('/'),
  routes,
});

// Session is loaded once before the first route resolves; guarded routes send
// guests to /login with the same ?next= round-trip the Go handlers use.
router.beforeEach(async (to) => {
  const auth = useAuthStore();
  if (!auth.loaded) await auth.load();
  if (to.meta.requiresAuth && !auth.isAuthed) {
    return { path: '/login', query: { next: to.fullPath } };
  }
});

export default router;
