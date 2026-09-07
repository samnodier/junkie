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
    // Public: the visitor forgot their password, so they have no session.
    path: '/reset-password/:token',
    name: 'reset-password',
    component: () => import('./views/ResetPasswordView.vue'),
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
    // The private twin of a room's focus page. "solo" is already a reserved
    // username, so this can't be shadowed by the /{username} catch-all below.
    path: '/solo',
    name: 'solo-focus',
    component: () => import('./views/SoloFocusView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/f/:code',
    name: 'focus-room',
    component: () => import('./views/FocusRoomView.vue'),
    meta: { requiresAuth: true },
  },
  {
    // The OBS overlay twin of the route above. Deliberately unguarded: a
    // browser source has no session to offer, and the Go handler behind it
    // serves temporary rooms only, read-only. Declared after /f/:code so the
    // more specific path wins vue-router's scoring either way.
    path: '/f/:code/embed',
    name: 'focus-embed',
    component: () => import('./views/FocusEmbedView.vue'),
  },
  {
    path: '/r/:code/members',
    name: 'room-members',
    component: () => import('./views/RoomMembersView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/admin',
    name: 'admin',
    component: () => import('./views/AdminView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/admin/users/:id',
    name: 'admin-user',
    component: () => import('./views/AdminUserView.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/admin/rooms/:id',
    name: 'admin-room',
    component: () => import('./views/AdminRoomView.vue'),
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
  {
    // Anything the Go catch-all served the SPA for but no route above
    // claims — previously these rendered an empty <router-view>.
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('./views/NotFoundView.vue'),
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
