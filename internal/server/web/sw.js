'use strict';

const CACHE = 'watplan-v2';

self.addEventListener('install', () => self.skipWaiting());

self.addEventListener('activate', (event) => {
    event.waitUntil((async () => {
        const keys = await caches.keys();
        await Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k)));
        await self.clients.claim();
    })());
});

function shellKey(url) {
    if (url.pathname === '/app') return '/app';
    if (url.pathname === '/' && !url.searchParams.has('plan') && !url.searchParams.has('view')) return '/';
    return null;
}

async function staleWhileRevalidate(event, request, key) {
    const cache = await caches.open(CACHE);
    const cached = await cache.match(key);
    const network = fetch(request).then(async (response) => {
        if (response.type === 'opaqueredirect' || response.redirected) {
            await cache.delete(key, { ignoreSearch: true });
            return response;
        }
        if (response.ok && response.type === 'basic') {
            if (!cached && typeof key !== 'string') await cache.delete(key, { ignoreSearch: true });
            await cache.put(key, response.clone());
        }
        return response;
    });
    if (cached) {
        event.waitUntil(network.catch(() => undefined));
        return cached;
    }
    return network.catch(async (err) => {
        const fallback = key === '/' ? await cache.match('/app') : null;
        if (fallback) return fallback;
        throw err;
    });
}

self.addEventListener('fetch', (event) => {
    const request = event.request;
    if (request.method !== 'GET') return;
    const url = new URL(request.url);
    if (url.origin !== self.location.origin) return;
    if (url.pathname.startsWith('/api/') || url.pathname === '/sw.js') return;

    if (request.mode === 'navigate') {
        const key = shellKey(url);
        if (key) event.respondWith(staleWhileRevalidate(event, request, key));
        return;
    }
    event.respondWith(staleWhileRevalidate(event, request, request));
});
