// junkie service worker — deliberately thin.
//
// junkie is a live, server-authoritative app (WebSocket timers, Postgres), so
// this worker exists to make the app installable and to serve static assets
// fast — NOT to run offline. It never caches HTML, todos, timers, avatars, or
// any per-user response, so a client can never see stale application state.
//
// Assets are served from fixed, unhashed paths (/assets/app.css, etc.), so bump
// CACHE_VERSION on every deploy that changes a precached asset to evict the old
// copies.
const CACHE_VERSION = 'junkie-v1';

const PRECACHE = [
  '/assets/app.css',
  '/assets/guest.js',
  '/assets/htmx.min.js',
  '/assets/notifications.js',
  '/assets/icon.svg',
  '/assets/icon-192.png',
  '/assets/icon-512.png',
  '/assets/icon-maskable-512.png',
  '/assets/apple-touch-icon.png',
  '/manifest.webmanifest',
  '/offline',
];
const PRECACHE_SET = new Set(PRECACHE);

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_VERSION)
      .then((cache) => cache.addAll(PRECACHE))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(
        keys.filter((k) => k !== CACHE_VERSION).map((k) => caches.delete(k))
      ))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;                 // never intercept POST etc.

  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;  // let fonts/CDN pass through
  if (url.pathname.startsWith('/ws/') || url.pathname === '/healthz') return;

  // Page loads: always go to the network so todos/timers/auth are live.
  // Only when the network is unreachable do we fall back to the offline page.
  if (req.mode === 'navigate') {
    event.respondWith(fetch(req).catch(() => caches.match('/offline')));
    return;
  }

  // Static assets: serve from cache immediately, refresh the copy in the
  // background (stale-while-revalidate).
  if (PRECACHE_SET.has(url.pathname)) {
    event.respondWith(
      caches.open(CACHE_VERSION).then((cache) =>
        cache.match(req).then((cached) => {
          const network = fetch(req)
            .then((res) => {
              if (res && res.ok) cache.put(req, res.clone());
              return res;
            })
            .catch(() => cached);
          return cached || network;
        })
      )
    );
    return;
  }

  // Everything else same-origin (avatars, HTML fragments): network-only.
  // Falling through without respondWith leaves the default behaviour untouched.
});
