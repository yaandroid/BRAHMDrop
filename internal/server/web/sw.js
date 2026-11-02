// BRAHM Drop service worker — caches the app shell so it opens instantly and
// works as an installed app. Never caches API or transfer responses.
const CACHE = "brahmdrop-v1.1";
const SHELL = [
  "/", "/index.html", "/style.css", "/app.js",
  "/manifest.webmanifest", "/icon-192.png", "/icon-512.png", "/favicon.ico"
];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (e) => {
  const url = new URL(e.request.url);
  // Always hit the network for the API and anything non-GET.
  if (e.request.method !== "GET" || url.pathname.startsWith("/api/")) return;
  e.respondWith(
    caches.match(e.request).then((hit) => hit || fetch(e.request).catch(() => caches.match("/index.html")))
  );
});
