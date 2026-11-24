import { test, before, after, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { startServer, startBrowser, chromePath } from './harness.mjs';

const skip = chromePath ? false : 'brak Chrome (ustaw CHROME_PATH)';
let server;
let browser;
let page;
let cdn;
const objects = new Map();
const cdnGets = [];

function startFakeR2() {
    return new Promise((resolve) => {
        const srv = createServer((req, res) => {
            const url = new URL(req.url, 'http://x');
            if (req.method === 'PUT' && url.pathname.startsWith('/bucket/')) {
                const chunks = [];
                req.on('data', (c) => chunks.push(c));
                req.on('end', () => {
                    objects.set(url.pathname.slice('/bucket/'.length), Buffer.concat(chunks));
                    res.writeHead(200).end();
                });
                return;
            }
            if (req.method === 'HEAD' && url.pathname.startsWith('/bucket/')) {
                res.writeHead(objects.has(url.pathname.slice('/bucket/'.length)) ? 200 : 404).end();
                return;
            }
            if (req.method === 'GET' && url.pathname.startsWith('/plans/')) {
                cdnGets.push(url.pathname);
                const body = objects.get(url.pathname.slice(1));
                res.writeHead(body ? 200 : 404, { 'Content-Type': 'application/json', 'Access-Control-Allow-Origin': '*' }).end(body);
                return;
            }
            res.writeHead(404).end();
        });
        srv.listen(0, '127.0.0.1', () => resolve(srv));
    });
}

before(async () => {
    if (skip) return;
    cdn = await startFakeR2();
    const base = `http://127.0.0.1:${cdn.address().port}`;
    server = await startServer({
        PLANWAT_R2_ENDPOINT: base,
        PLANWAT_R2_BUCKET: 'bucket',
        PLANWAT_R2_ACCESS_KEY_ID: 'test',
        PLANWAT_R2_SECRET_ACCESS_KEY: 'test',
        PLANWAT_CDN_URL: base,
    });
    browser = await startBrowser();
    page = browser.page;
    const deadline = Date.now() + 10000;
    while (!objects.has('plans/WMT26AX3S1.json') && Date.now() < deadline) await new Promise(r => setTimeout(r, 100));
});

after(async () => {
    await browser?.stop();
    server?.stop();
    cdn?.close();
});

beforeEach(async () => {
    if (skip) return;
    await page.viewport(390, 844);
    await page.goto(server.base + '/app');
    await page.reset();
    await page.blockURLs([]);
    page.errors.length = 0;
    page.policyErrors.length = 0;
    cdnGets.length = 0;
});

const cdnBase = () => `http://127.0.0.1:${cdn.address().port}`;

test('cdn: serwer przy starcie wysyła plany do R2, a strona zna adres CDN', { skip }, async () => {
    assert.ok(objects.has('plans/WMT26AX3S1.json'), `w R2: ${[...objects.keys()].join(', ')}`);
    const plan = JSON.parse(objects.get('plans/WMT26AX3S1.json'));
    assert.ok(Array.isArray(plan.events) && plan.events.length > 0);
    const html = await (await fetch(server.base + '/plan/WMT26AX3S1')).text();
    assert.ok(html.includes(`<meta name="watplan-cdn" content="${cdnBase()}">`));
    const csp = (await fetch(server.base + '/app')).headers.get('content-security-policy');
    assert.ok(csp.includes(`connect-src 'self' https://cloud.umami.is https://gateway.umami.is https://tiles.openfreemap.org ${cdnBase()}`), csp);
});

test('cdn: pierwsze wejście pobiera plan także z CDN', { skip }, async () => {
    await page.goto(`${server.base}/plan/WMT26AX3S1`);
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    assert.ok(cdnGets.includes('/plans/WMT26AX3S1.json'), `zapytania do CDN: ${cdnGets.join(', ')}`);
    assert.deepEqual(page.policyErrors, []);
    assert.deepEqual(page.errors, []);
});

test('cdn: gdy serwer nie odpowiada, nowy użytkownik i tak widzi plan', { skip }, async () => {
    await page.blockURLs(['*/api/plan/*']);
    await page.goto(`${server.base}/plan/WMT26AX3S1`);
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    assert.ok(cdnGets.includes('/plans/WMT26AX3S1.json'));
    assert.equal(await page.eval(`document.querySelector('#view .message.error') === null`), true);
    assert.deepEqual(page.errors, []);
});

test('cdn: przy zapisanym planie CDN nie jest pytany', { skip }, async () => {
    await page.goto(`${server.base}/plan/WMT26AX3S1`);
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.waitFor(`(localStorage.getItem('planCache') || '').includes('"id":"WMT26AX3S1"')`);
    cdnGets.length = 0;
    await page.goto(`${server.base}/plan/WMT26AX3S1`);
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await new Promise(r => setTimeout(r, 300));
    assert.deepEqual(cdnGets, []);
});
