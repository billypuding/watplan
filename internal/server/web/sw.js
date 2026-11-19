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
    if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/cal/') || url.pathname === '/sw.js') return;

    if (request.mode === 'navigate') {
        const key = shellKey(url);
        if (key) event.respondWith(staleWhileRevalidate(event, request, key));
        return;
    }
    event.respondWith(staleWhileRevalidate(event, request, request));
});

self.addEventListener('push', (event) => {
    let data = {};
    try { data = event.data ? event.data.json() : {}; } catch (_) { data = { body: event.data ? event.data.text() : '' }; }
    const title = data.title || 'Zmiana w planie';
    event.waitUntil(self.registration.showNotification(title, {
        body: data.body || '',
        tag: data.tag || 'plan',
        renotify: true,
        icon: '/favicon-192.png?v=3',
        badge: '/favicon-192.png?v=3',
        data: { url: data.url || '/app' },
    }));
});

self.addEventListener('notificationclick', (event) => {
    event.notification.close();
    const target = new URL((event.notification.data && event.notification.data.url) || '/app', self.location.origin).href;
    event.waitUntil((async () => {
        const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
        for (const client of windows) {
            if (new URL(client.url).pathname === '/app' && 'navigate' in client) {
                await client.focus();
                return client.navigate(target);
            }
        }
        return self.clients.openWindow(target);
    })());
});
