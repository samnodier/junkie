import { createRouter, createWebHistory } from 'vue-router';

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

export default createRouter({
  history: createWebHistory('/'),
  routes,
});
