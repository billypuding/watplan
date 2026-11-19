import { spawn, spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, rmSync, cpSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createServer } from 'node:net';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const sleep = (ms) => new Promise(r => setTimeout(r, ms));

export const FIXED_NOW = '2026-11-19T10:15:00+01:00';

const CHROME_PATHS = [
    process.env.CHROME_PATH,
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
].filter(Boolean);

export const chromePath = CHROME_PATHS.find(p => existsSync(p));

function freePort() {
    return new Promise((resolve, reject) => {
        const srv = createServer();
        srv.listen(0, '127.0.0.1', () => {
            const { port } = srv.address();
            srv.close(() => resolve(port));
        });
        srv.on('error', reject);
    });
}

async function waitFor(fn, timeout = 20000, step = 100) {
    const end = Date.now() + timeout;
    let last;
    while (Date.now() < end) {
        try {
            const v = await fn();
            if (v) return v;
        } catch (err) {
            last = err;
        }
        await sleep(step);
    }
    throw last || new Error('timeout');
}

export async function startServer() {
    const work = mkdtempSync(join(tmpdir(), 'watplan-e2e-'));
    cpSync(join(root, 'e2e', 'fixtures'), work, { recursive: true });
    const bin = join(work, 'watplan');
    const build = spawnSync('go', ['build', '-o', bin, './cmd/watplan'], { cwd: root, stdio: 'inherit' });
    if (build.status !== 0) throw new Error('go build nie powiódł się');
    const port = await freePort();
    const proc = spawn(bin, ['-offline'], {
        cwd: work,
        env: { ...process.env, PLANWAT_ADDR: `127.0.0.1:${port}`, PLANWAT_CONFIG: join(work, 'plans.json'), PLANWAT_DATA: join(work, 'data') },
        stdio: 'ignore',
    });
    const base = `http://127.0.0.1:${port}`;
    await waitFor(async () => (await fetch(base + '/api/status')).ok);
    return {
        base,
        stop() {
            proc.kill();
            rmSync(work, { recursive: true, force: true });
        },
    };
}

const clockScript = (iso) => `(() => {
    const fixed = new Date(${JSON.stringify(iso)}).getTime();
    const start = performance.now();
    const Real = Date;
    class FakeDate extends Real {
        constructor(...args) { super(...(args.length ? args : [fixed + (performance.now() - start)])); }
        static now() { return fixed + (performance.now() - start); }
    }
    globalThis.Date = FakeDate;
})();`;

export async function startBrowser() {
    const profile = mkdtempSync(join(tmpdir(), 'watplan-chrome-'));
    const port = await freePort();
    const flags = ['--headless=new', '--disable-gpu', '--no-first-run', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`];
    if (process.env.CI) flags.push('--no-sandbox', '--disable-dev-shm-usage');
    const proc = spawn(chromePath, [...flags, 'about:blank'], { stdio: 'ignore' });
    const target = await waitFor(async () => (await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()).find(t => t.type === 'page'));
    const ws = new WebSocket(target.webSocketDebuggerUrl);
    await new Promise((resolve, reject) => { ws.onopen = resolve; ws.onerror = reject; });
    let seq = 0;
    const pending = new Map();
    const errors = [];
    ws.onmessage = (m) => {
        const msg = JSON.parse(m.data);
        if (msg.id && pending.has(msg.id)) {
            pending.get(msg.id)(msg);
            pending.delete(msg.id);
        } else if (msg.method === 'Runtime.exceptionThrown') {
            errors.push(msg.params.exceptionDetails.exception?.description || msg.params.exceptionDetails.text);
        }
    };
    const send = (method, params = {}) => new Promise(resolve => {
        const id = ++seq;
        pending.set(id, resolve);
        ws.send(JSON.stringify({ id, method, params }));
    });
    await send('Page.enable');
    await send('Runtime.enable');
    await send('Page.addScriptToEvaluateOnNewDocument', { source: clockScript(FIXED_NOW) });

    const page = {
        errors,
        async viewport(width, height, mobile = width < 600) {
            await send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile });
        },
        async goto(url) {
            await send('Page.navigate', { url });
            await sleep(300);
            await waitFor(() => page.eval('document.readyState === "complete"'));
        },
        async eval(expression) {
            const r = await send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
            if (r.result.exceptionDetails) throw new Error(r.result.exceptionDetails.exception?.description || r.result.exceptionDetails.text);
            return r.result.result.value;
        },
        async waitFor(expression, timeout = 8000) {
            return waitFor(() => page.eval(expression), timeout);
        },
        async click(selector) {
            const ok = await page.eval(`(() => { const el = document.querySelector(${JSON.stringify(selector)}); if (!el) return false; el.click(); return true; })()`);
            if (!ok) throw new Error(`brak elementu ${selector}`);
        },
        async text(selector) {
            return page.eval(`document.querySelector(${JSON.stringify(selector)})?.textContent ?? null`);
        },
        async reset() {
            await page.eval('localStorage.clear(); sessionStorage.clear(); true').catch(() => undefined);
        },
    };
    return {
        page,
        async stop() {
            ws.close();
            proc.kill();
            await sleep(200);
            rmSync(profile, { recursive: true, force: true });
        },
    };
}
