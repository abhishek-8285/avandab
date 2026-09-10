const CACHE_NAME = 'avandab-v3';
const STATIC_CACHE = 'avandab-static-v3';

// Assets to pre-cache on install (shell). Runtime requests carry ?v= query
// strings, so lookups use ignoreSearch (see cacheFirst) — bare paths here
// still match versioned requests.
const PRECACHE_ASSETS = [
  '/static/css/tailwind.css',
  '/static/css/app.css',
  '/static/css/fonts.css',
  '/static/css/material-symbols.css',
  '/static/css/material-icons.css',
  '/static/js/htmx.min.js',
  '/static/js/router.js',
  '/static/js/toast.js',
  '/static/js/console.js',
  '/static/js/compress-image.js',
  '/static/icons/icon-192.png',
  '/static/icons/icon-512.png',
];

// HTML pages safe to cache for offline use: public marketing/legal routes
// only. Authenticated pages (dashboard, bookings, ...) are never cached —
// they carry per-user data and must not linger on shared devices.
const PUBLIC_HTML_ROUTES = ['/', '/features', '/contact-us', '/privacy', '/terms', '/refunds'];
const isPublicHTML = (pathname) =>
  pathname === '/' ||
  PUBLIC_HTML_ROUTES.some((p) => p !== '/' && (pathname === p || pathname.startsWith(p + '/')));

// Install: pre-cache shell assets
self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(STATIC_CACHE).then((cache) => {
      return cache.addAll(PRECACHE_ASSETS);
    }).then(() => self.skipWaiting())
  );
});

// Activate: clean old caches
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => {
      return Promise.all(
        keys
          .filter((key) => key !== CACHE_NAME && key !== STATIC_CACHE)
          .map((key) => caches.delete(key))
      );
    }).then(() => self.clients.claim())
  );
});

// Fetch: strategy per request type
self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  // Only handle same-origin GETs
  if (url.origin !== self.location.origin || event.request.method !== 'GET') return;

  // Strategy 1: Cache-first for /static assets (immutable, versioned).
  // ignoreSearch makes bare-path precache entries match ?v= requests.
  if (url.pathname.startsWith('/static/')) {
    event.respondWith(cacheFirst(event.request, STATIC_CACHE));
    return;
  }

  // Strategy 2: Network-first ONLY for public HTML pages (always fresh,
  // cached as offline fallback). Authenticated pages pass through untouched.
  if (
    event.request.headers.get('accept')?.includes('text/html') &&
    isPublicHTML(url.pathname)
  ) {
    event.respondWith(networkFirst(event.request, CACHE_NAME));
    return;
  }

  // Strategy 3: Everything else (API/SSE/authed pages) — pass through.
  // Do NOT cache SSE streams or API responses.
});

// Cache-first: try cache, fall back to network, update cache
async function cacheFirst(request, cacheName) {
  const cached = await caches.match(request, { ignoreSearch: true });
  if (cached) return cached;

  try {
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(cacheName);
      cache.put(request, response.clone());
    }
    return response;
  } catch (err) {
    return new Response('Offline', { status: 503, statusText: 'Offline' });
  }
}

// Network-first: try network, fall back to cache
async function networkFirst(request, cacheName) {
  try {
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(cacheName);
      cache.put(request, response.clone());
    }
    return response;
  } catch (err) {
    const cached = await caches.match(request);
    if (cached) return cached;

    // Offline fallback page
    return new Response(
      '<html><body><h1>Offline</h1><p>You are offline. Please check your connection.</p></body></html>',
      { status: 503, headers: { 'Content-Type': 'text/html' } }
    );
  }
}
