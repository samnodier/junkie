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
