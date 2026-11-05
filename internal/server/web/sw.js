// BRAHM Drop service worker — network-first so an updated build always shows
// the latest UI; the cache is only an offline fallback.
const CACHE = "brahmdrop-v1.2";
const SHELL = ["/", "/index.html", "/style.css", "/app.js", "/manifest.webmanifest", "/icon-192.png", "/icon-512.png", "/favicon.ico"];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).catch(() => {}).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== "GET" || url.pathname.startsWith("/api/")) return; // live
  // Network-first: always try the server; fall back to cache only when offline.
  e.respondWith(
    fetch(e.request)
      .then((resp) => {
        try {
          const copy = resp.clone();
          caches.open(CACHE).then((c) => c.put(e.request, copy)).catch(() => {});
        } catch (x) {}
        return resp;
      })
      .catch(() => caches.match(e.request).then((hit) => hit || caches.match("/index.html")))
  );
});
