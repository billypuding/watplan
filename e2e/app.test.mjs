import { test, before, after, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { startServer, startBrowser, chromePath } from './harness.mjs';

const skip = chromePath ? false : 'brak Chrome (ustaw CHROME_PATH)';
let server;
let browser;
let page;

before(async () => {
    if (skip) return;
    server = await startServer();
    browser = await startBrowser();
    page = browser.page;
});

after(async () => {
    await browser?.stop();
    server?.stop();
});

beforeEach(async () => {
    if (skip) return;
    await page.viewport(390, 844);
    await page.goto(server.base + '/app');
    await page.reset();
    page.errors.length = 0;
});

const app = (query = '') => `${server.base}/app?plan=WMT26AX3S1${query}`;

test('strona główna: wyszukanie grupy otwiera jej plan', { skip }, async () => {
    await page.goto(server.base + '/?landing');
    await page.waitFor('document.querySelector("#group")');
    await page.eval(`(() => { const i = document.querySelector('#group'); i.value = 'WMT26AX3'; i.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
    await page.waitFor(`[...document.querySelectorAll('#group-results button, #group-results a')].some(b => b.textContent.includes('WMT26AX3S1'))`);
    await page.eval(`[...document.querySelectorAll('#group-results button, #group-results a')].find(b => b.textContent.includes('WMT26AX3S1')).click()`);
    await page.waitFor(`location.pathname === '/plan/WMT26AX3S1'`);
    await page.waitFor(`document.querySelector('.day-title h1')?.textContent === 'Czwartek'`);
    assert.deepEqual(page.errors, []);
});

test('strona główna: licznik semestru z danych planów', { skip }, async () => {
    await page.goto(server.base + '/?landing');
    await page.waitFor(`document.querySelector('[data-sem-weeks]')?.textContent.includes('/ 17')`);
    assert.equal(await page.text('#sem-h'), 'Semestr zimowy 2026/27');
    assert.match(await page.text('[data-sem-left]'), /został|zostały|zostało/);
});

const semSamples = `new Promise((resolve) => {
    const out = document.querySelector('[data-sem-pct]');
    const seen = [];
    const t0 = performance.now();
    const tick = () => {
        seen.push(Number(out.textContent));
        if (performance.now() - t0 < 1800) requestAnimationFrame(tick);
        else resolve(seen);
    };
    requestAnimationFrame(tick);
})`;

test('strona główna: odpowiedź API nie przerywa animacji semestru', { skip }, async () => {
    await page.goto(server.base + '/?landing');
    const seen = await page.eval(semSamples);
    const final = seen[seen.length - 1];
    assert.ok(final > 0, `końcowy procent ${final}`);
    assert.ok(new Set(seen).size > 5, `za mało klatek animacji: ${[...new Set(seen)].join(',')}`);
    for (let i = 1; i < seen.length; i++) assert.ok(seen[i] >= seen[i - 1], `procent cofnął się: ${seen[i - 1]} -> ${seen[i]}`);
    assert.equal(await page.eval(`Number(document.querySelector('[data-sem-pct]').textContent)`), final);
    assert.deepEqual(page.errors, []);
});

test('strona główna: bez animacji semestru przy prefers-reduced-motion', { skip }, async () => {
    await page.emulateMedia([{ name: 'prefers-reduced-motion', value: 'reduce' }]);
    try {
        await page.goto(server.base + '/?landing');
        const seen = await page.eval(semSamples);
        assert.equal(new Set(seen).size, 1, `wartości: ${[...new Set(seen)].join(',')}`);
        assert.ok(seen[0] > 0);
    } finally {
        await page.emulateMedia([]);
    }
});

test('bez błędów CSP i Permissions-Policy na stronach i w akcjach', { skip }, async (t) => {
    page.policyErrors.length = 0;
    page.policyNotes.length = 0;
    await page.goto(server.base + '/?landing');
    await page.waitFor(`document.querySelector('[data-sem-weeks]')?.textContent.length > 0`);
    await page.goto(server.base + '/o-watplan');
    await page.waitFor(`document.querySelector('.faq-h1') !== null`);
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.waitFor(`navigator.serviceWorker.controller !== null`, 10000).catch(() => undefined);
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.click('#refreshButton');
    await page.waitFor(`document.querySelector('#notice').classList.contains('show')`);
    await page.eval(`selectPlan('WMT26AX1S1', 'WMT26AX1S1'), true`);
    await page.waitFor(`location.search.includes('WMT26AX1S1') && document.querySelectorAll('.ev-card').length > 0`);
    const shown = await page.eval(`(() => { if (!navigator.serviceWorker.controller) return false; navigator.serviceWorker.dispatchEvent(new Event('controllerchange')); return !document.querySelector('#updateBar').classList.contains('hidden'); })()`);
    if (shown) {
        await page.click('#updateBar');
        await page.waitFor(`document.readyState === 'complete' && document.querySelectorAll('.ev-card').length > 0`);
    }
    assert.ok(shown, 'pasek nowej wersji się nie pokazał');
    for (const note of new Set(page.policyNotes)) t.diagnostic(`zależne od przeglądarki: ${note}`);
    assert.deepEqual(page.policyErrors, []);
    assert.deepEqual(page.errors, []);
});

test('offline: polskie komunikaty i zapisany plan', { skip }, async () => {
    const alertText = `document.querySelector('p.sr-only[role=alert]')?.textContent || ''`;
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.offline(true);
    try {
        await page.eval(`selectPlan('WMT26AX1S1', 'WMT26AX1S1'), true`);
        await page.waitFor(`document.querySelector('#view .message.error') !== null`);
        const message = await page.text('#view .message.error');
        assert.match(message, /Brak połączenia z internetem/);
        assert.doesNotMatch(message, /Failed to fetch|NetworkError|Load failed/);
        assert.equal(await page.eval(`document.querySelector('#notice').classList.contains('show')`), false);
        await page.waitFor(`(${alertText}).includes('Brak połączenia z internetem')`);

        await page.eval(`selectPlan('WMT26AX3S1', 'WMT26AX3S1'), true`);
        await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
        await page.waitFor(`document.querySelector('#notice').textContent.includes('zapisaną wersję')`);

        await page.click('#refreshButton');
        await page.waitFor(`document.querySelector('#notice').textContent.includes('Brak połączenia')`);
        assert.doesNotMatch(await page.text('#notice'), /Failed to fetch/);
        await page.waitFor(`(${alertText}).includes('Brak połączenia')`);
    } finally {
        await page.offline(false);
    }
    assert.deepEqual(page.errors, []);
});

test('strona grupy /plan/ID: tytuł, indeksowanie i przełączanie', { skip }, async () => {
    await page.goto(server.base + '/plan/WMT26AX3S1');
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    assert.match(await page.eval('document.title'), /WMT26AX3S1/);
    assert.equal(await page.eval(`document.querySelector('meta[name=robots]').content`), 'index, follow');
    assert.match(await page.eval(`document.querySelector('link[rel=canonical]').href`), /\/plan\/WMT26AX3S1$/);
    const paths = JSON.parse(await page.eval(`JSON.stringify(performance.getEntriesByType('resource').map(r => new URL(r.name).pathname))`));
    assert.equal(paths.filter(p => p === '/api/plan/WMT26AX3S1').length, 1);
    assert.equal(await page.eval(`window.earlyPlan && window.earlyPlan.id`), 'WMT26AX3S1');
    assert.match(await page.text('.plan-info h2'), /WMT26AX3S1/);
    await page.eval(`selectPlan('WMT26AX1S1', 'WMT26AX1S1'), true`);
    await page.waitFor(`location.pathname === '/plan/WMT26AX1S1' && !location.search.includes('plan=')`);
    assert.equal(await page.eval(`document.querySelector('.plan-info')`), null, 'stopka z przedmiotami innej grupy została po przełączeniu');
    assert.equal(await page.eval('document.title'), 'Plan zajęć WMT26AX1S1 | WATplan');
    assert.deepEqual(page.errors, []);
});

test('strona grupy z planem z serwera: widać go bez /api/plan, bez przesunięć i tak samo jak w JS', { skip }, async () => {
    const html = await (await fetch(server.base + '/plan/WMT26AX3S1')).text();
    const ssrSubjects = [...html.split('<details class="plan-info"')[0].matchAll(/<div class="ev-subject">([^<]*)<\/div>/g)].map(m => m[1].replace(/&#34;/g, '"').replace(/&amp;/g, '&'));
    assert.ok(ssrSubjects.length > 0, 'brak zajęć w HTML z serwera');
    await page.blockURLs(['*/api/plan/*']);
    await page.goto(server.base + '/plan/WMT26AX3S1');
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await new Promise(r => setTimeout(r, 400));
    const jsSubjects = JSON.parse(await page.eval(`JSON.stringify([...document.querySelectorAll('#view .ev-subject')].map(e => e.textContent))`));
    assert.deepEqual(jsSubjects, ssrSubjects);
    assert.equal(await page.eval(`document.getElementById('ssrPlan')`), null);
    assert.equal(await page.eval(`document.querySelector('#view .message.error')`), null);
    assert.ok(await page.eval(`document.querySelectorAll('.plan-info li').length`) > 3);
    const cls = await page.eval(`new Promise((resolve) => {
        let total = 0;
        new PerformanceObserver((list) => { for (const e of list.getEntries()) if (!e.hadRecentInput) total += e.value; }).observe({ type: 'layout-shift', buffered: true });
        setTimeout(() => resolve(total), 300);
    })`);
    assert.ok(cls < 0.05, `CLS ${cls}`);
    assert.deepEqual(page.errors, []);
    await page.blockURLs([]);
});

test('strona grupy na komputerze: tydzień z serwera taki sam jak z JS, bez przesunięć', { skip }, async () => {
    await page.viewport(1366, 900, false);
    await page.reset();
    const html = await (await fetch(server.base + '/plan/WMT26AX3S1')).text();
    const grid = html.split('<div class="week-grid ssr-w')[1].split('<details class="plan-info"')[0];
    const decode = (t) => t.replace(/&#34;/g, '"').replace(/&amp;/g, '&').replace(/&#39;/g, "'");
    const ssrCells = [...grid.matchAll(/<span class="ab">([^<]*)<\/span><span class="full">([^<]*)<\/span><span class="meta sub"><span class="kind"><span class="dot"><\/span>([^<]*)<\/span>/g)].map(m => decode(`${m[1]}|${m[2]}|${m[3]}`));
    assert.ok(ssrCells.length > 5, 'brak komórek tygodnia w HTML');
    await page.blockURLs(['*/api/plan/*']);
    await page.goto(server.base + '/plan/WMT26AX3S1');
    await page.waitFor(`document.querySelectorAll('.wk-cell').length > 0`);
    await new Promise(r => setTimeout(r, 400));
    const jsCells = JSON.parse(await page.eval(`JSON.stringify([...document.querySelectorAll('#view .wk-cell')].map(c => c.querySelector('.ab').textContent + '|' + c.querySelector('.full').textContent + '|' + c.querySelector('.kind').textContent))`));
    assert.deepEqual(jsCells, ssrCells);
    const cls = await page.eval(`new Promise((resolve) => {
        let total = 0;
        new PerformanceObserver((list) => { for (const e of list.getEntries()) if (!e.hadRecentInput) total += e.value; }).observe({ type: 'layout-shift', buffered: true });
        setTimeout(() => resolve(total), 300);
    })`);
    assert.ok(cls < 0.01, `CLS ${cls}`);
    assert.deepEqual(page.errors, []);
    assert.deepEqual(page.policyErrors, []);
    await page.blockURLs([]);
});

test('strona grupy: service worker najpierw pyta sieć, a offline podaje zapisaną kopię', { skip }, async () => {
    await page.goto(server.base + '/plan/WMT26AX3S1');
    await page.waitFor(`navigator.serviceWorker.controller !== null`, 10000).catch(() => undefined);
    await page.goto(server.base + '/plan/WMT26AX3S1');
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.waitFor(`caches.keys().then(async keys => { for (const k of keys) { if (await (await caches.open(k)).match('/plan/WMT26AX3S1')) return true; } return false; })`, 10000);
    await page.offline(true);
    try {
        await page.goto(server.base + '/plan/WMT26AX3S1');
        await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
        assert.match(await page.eval('document.title'), /WMT26AX3S1/);
    } finally {
        await page.offline(false);
    }
    assert.deepEqual(page.errors, []);
});

test('telefon: domyślnie widok dnia z bieżącymi zajęciami', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    assert.equal(await page.text('.day-title h1'), 'Czwartek');
    assert.match(await page.text('.day-title p'), /19 listopada · tydzień \d+\/\d+/);
    assert.equal(await page.eval('document.documentElement.scrollWidth'), 390);
    assert.deepEqual(page.errors, []);
});

test('plan pobiera się raz, już z nagłówka strony', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    const paths = JSON.parse(await page.eval(`JSON.stringify(performance.getEntriesByType('resource').map(r => new URL(r.name).pathname))`));
    assert.equal(paths.filter(p => p === '/api/plan/WMT26AX3S1').length, 1);
    assert.equal(paths.filter(p => p === '/api/changes/WMT26AX3S1').length, 1);
    assert.equal(await page.eval(`window.earlyPlan.id === 'WMT26AX3S1' && window.earlyPlan.plan === null && window.earlyPlan.changes === null`), true);
    assert.deepEqual(page.errors, []);
});

test('komputer: domyślnie siatka tygodnia z dzisiejszą kolumną', { skip }, async () => {
    await page.viewport(1280, 900);
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.wk-cell').length > 0`);
    assert.ok(await page.eval(`!!document.querySelector('.wk-head.today')`));
    assert.match(await page.text('.nav-label small'), /^tydzień \d+\/\d+ · \d+ zaj/);
    assert.deepEqual(page.errors, []);
});

test('wybór tygodnia z listy przenosi do tego tygodnia', { skip }, async () => {
    await page.viewport(1280, 900);
    await page.goto(app());
    await page.waitFor(`!!document.querySelector('.nav-label.pick')`);
    const before = await page.text('.nav-label b');
    await page.click('.nav-label.pick');
    await page.waitFor(`document.getElementById('weekModal').open`);
    const target = await page.eval(`(() => { const items = [...document.querySelectorAll('.wk-pick')]; const i = items.findIndex(b => b.classList.contains('sel')); const next = items[i + 1]; next.click(); return next.querySelector('.when b').textContent; })()`);
    await page.waitFor(`!document.getElementById('weekModal').open`);
    const after = await page.text('.nav-label b');
    assert.notEqual(after, before);
    assert.equal(after, target);
});

test('ukrycie przedmiotu usuwa go z planu i z linku kalendarza', { skip }, async () => {
    await page.goto(app('&view=week'));
    await page.waitFor(`document.querySelectorAll('.wk-cell').length > 0`);
    const subject = await page.eval(`document.querySelector('.wk-cell .full').textContent`);
    await page.click('#settingsBtn');
    await page.waitFor(`document.querySelectorAll('#subjectsList input').length > 0`);
    await page.eval(`[...document.querySelectorAll('#subjectsList label')].find(l => l.querySelector('span').textContent === ${JSON.stringify(subject)}).querySelector('input').click()`);
    const visible = await page.eval(`[...document.querySelectorAll('.wk-cell .full')].map(e => e.textContent)`);
    assert.ok(!visible.includes(subject));
    const href = await page.eval(`document.getElementById('calApple').href`);
    assert.ok(href.startsWith('webcal://'));
    assert.ok(decodeURIComponent(href.replace(/\+/g, ' ')).includes(`hide=${subject}`));
});

test('wybór semestru na stronie głównej przełącza plan', { skip }, async () => {
    await page.goto(server.base + '/?landing');
    await page.waitFor(`!!document.querySelector('[data-sem-pick] [aria-pressed="true"]')`);
    assert.equal(await page.eval(`document.querySelector('[data-sem-pick] [aria-pressed="true"]').textContent`), 'Zimowy');
    await page.click('[data-sem-pick] [data-semester="lato"]');
    await page.goto(app('&view=day'));
    await page.waitFor(`document.querySelector('.empty-day b')?.textContent === 'Brak planu na semestr letni'`);
    assert.equal(await page.eval(`document.querySelectorAll('.ev-card').length`), 0);
    assert.equal(await page.eval(`!!document.querySelector('.sem-switch')`), false);
    await page.goto(server.base + '/?landing');
    await page.waitFor(`document.querySelector('[data-sem-pick] [data-semester="lato"]')?.getAttribute('aria-pressed') === 'true'`);
    await page.click('[data-sem-pick] [data-semester="zima"]');
    await page.goto(app('&view=day'));
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
});

test('subskrypcja kalendarza zwraca poprawny plik .ics', { skip }, async () => {
    const res = await fetch(`${server.base}/cal/WMT26AX3S1.ics?alarm=15`);
    assert.equal(res.status, 200);
    assert.match(res.headers.get('content-type'), /^text\/calendar/);
    const body = await res.text();
    assert.ok(body.startsWith('BEGIN:VCALENDAR\r\n'));
    assert.ok(body.includes('BEGIN:VALARM'));
    assert.ok(body.split('BEGIN:VEVENT').length > 100);
});

test('historia zmian pokazuje przeniesione i zmienione zajęcia', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`!document.getElementById('changeChip').classList.contains('hidden')`);
    await page.click('#changeChip');
    await page.waitFor(`document.querySelectorAll('.chg-item').length > 0`);
    const kinds = await page.eval(`[...document.querySelectorAll('.chg-item')].map(i => i.classList[1])`);
    assert.deepEqual(kinds.sort(), ['changed', 'moved']);
});

test('moje zajęcia: dodanie, kolizja z planem, zapis i usunięcie', { skip }, async () => {
    await page.reset();
    await page.goto(app('&view=week'));
    await page.waitFor(`document.querySelectorAll('.wk-cell').length > 0`);
    const target = JSON.parse(await page.eval(`JSON.stringify((({ date, start, end, subject }) => ({ date, start, end, subject, weekday: dayIndex(parseISO(date)) }))(allEvents.find(e => mondayOf(e.date) === mondayOf(selectedDate))))`));
    await page.click('#toolsBtn');
    await page.click('[data-tool="extras"]');
    await page.waitFor(`document.getElementById('extrasModal')?.open`);
    assert.equal(await page.eval(`getComputedStyle(document.getElementById('extraDateLabel')).display`), 'none');
    await page.eval(`(() => { const r = document.getElementById('extraRepeat'); r.value = 'once'; r.dispatchEvent(new Event('change')); return true; })()`);
    assert.equal(await page.eval(`getComputedStyle(document.getElementById('extraWeekdayLabel')).display`), 'none');
    assert.notEqual(await page.eval(`getComputedStyle(document.getElementById('extraDateLabel')).display`), 'none');
    await page.eval(`(() => { const r = document.getElementById('extraRepeat'); r.value = 'weekly'; r.dispatchEvent(new Event('change')); return true; })()`);
    await page.eval(`document.getElementById('extraName').value = ''; true`);
    await page.click('#extraForm button[type=submit]');
    assert.equal(await page.text('#extraError'), 'Podaj nazwę zajęć.');
    await page.eval(`(() => {
        document.getElementById('extraName').value = 'Lektorat';
        document.getElementById('extraWeekday').value = '${target.weekday}';
        document.getElementById('extraStart').value = '${target.start}';
        document.getElementById('extraEnd').value = '${target.end}';
        document.getElementById('extraRoom').value = '65/100';
        return true;
    })()`);
    await page.click('#extraForm button[type=submit]');
    await page.waitFor(`document.querySelectorAll('.extra-item').length === 1`);
    assert.match(await page.text('.extra-item'), /Lektorat/);
    assert.match(await page.text('.extra-item'), /Kolizje z planem: \d+/);
    const cell = await page.eval(`(() => { const c = document.querySelector('.wk-cell.extra'); return c && c.title; })()`);
    assert.ok(cell, 'brak własnych zajęć w siatce tygodnia');
    assert.ok(cell.includes(`Kolizja: ${target.subject}`), cell);
    const stored = JSON.parse(await page.eval(`localStorage.getItem('extraClasses')`));
    assert.equal(Object.values(stored)[0][0].subject, 'Lektorat');

    await page.goto(app('&view=week'));
    await page.waitFor(`document.querySelectorAll('.wk-cell.extra').length > 0`);
    await page.click('#toolsBtn');
    await page.click('[data-tool="extras"]');
    await page.waitFor(`document.querySelectorAll('.extra-item').length === 1`);
    await page.click('.extra-item .link-btn');
    await page.waitFor(`document.querySelectorAll('.extra-item').length === 0`);
    assert.equal(await page.eval(`document.querySelectorAll('.wk-cell.extra').length`), 0);
    assert.equal(await page.eval(`localStorage.getItem('extraClasses')`), '{}');
    assert.deepEqual(page.errors, []);
});

test('statystyki planu liczą godziny, przedmioty i dni tygodnia', { skip }, async () => {
    await page.reset();
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    const expected = JSON.parse(await page.eval(`JSON.stringify({ mins: allEvents.reduce((n, e) => n + toMin(e.end) - toMin(e.start), 0), subjects: new Set(allEvents.map(e => e.subject)).size })`));
    await page.click('#toolsBtn');
    await page.click('[data-tool="stats"]');
    await page.waitFor(`document.getElementById('statsModal')?.open`);
    const first = await page.text('#statsModalBody .stat b');
    assert.equal(first, `${String(Math.round(expected.mins / 6) / 10).replace('.', ',')} h`);
    assert.equal(await page.eval(`document.querySelectorAll('#statsModalBody .stat-bars')[0].children.length`), expected.subjects);
    assert.ok(await page.eval(`document.querySelectorAll('#statsModalBody .stat-bars')[1].children.length`) >= 5);
    assert.match(await page.text('#statsModalSub'), /semestr (zimowy|letni)/);
    assert.equal(await page.eval(`[...document.querySelectorAll('script[src]')].filter(s => s.src.includes('/more.js?v=')).length`), 1);
    assert.deepEqual(page.errors, []);
});

test('udostępnianie pokazuje link i działający kod QR', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.click('#toolsBtn');
    await page.click('[data-tool="share"]');
    await page.waitFor(`document.querySelector('.share-qr')?.complete && document.querySelector('.share-qr').naturalWidth > 0`);
    assert.equal(await page.eval(`document.getElementById('shareLink').value`), `${server.base}/plan/WMT26AX3S1`);
    const decoded = await page.eval(`(async () => {
        if (!('BarcodeDetector' in window)) return null;
        const c = document.createElement('canvas');
        c.width = c.height = 440;
        c.getContext('2d').drawImage(document.querySelector('.share-qr'), 0, 0, 440, 440);
        const found = await new BarcodeDetector({ formats: ['qr_code'] }).detect(c);
        return found.map(x => x.rawValue).join('|');
    })()`);
    if (decoded !== null) assert.equal(decoded, `${server.base}/plan/WMT26AX3S1`);
    assert.deepEqual(page.errors, []);
});

test('przycisk instalacji pojawia się po beforeinstallprompt i wywołuje prompt', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.eval(`(() => {
        document.getElementById('installTool').hidden = true;
        const e = new Event('beforeinstallprompt', { cancelable: true });
        e.prompt = () => { window.__prompted = true; return Promise.resolve(); };
        e.userChoice = Promise.resolve({ outcome: 'accepted' });
        window.dispatchEvent(e);
        window.__prevented = e.defaultPrevented;
        return true;
    })()`);
    assert.equal(await page.eval(`document.getElementById('installTool').hidden`), false);
    assert.equal(await page.eval(`window.__prevented`), true);
    await page.click('#toolsBtn');
    await page.click('[data-tool="install"]');
    await page.waitFor(`window.__prompted === true && document.getElementById('installTool').hidden`);
    assert.deepEqual(page.errors, []);
});

test('dostępność: nazwy dialogów, Esc, powrót fokusu i krótkie komunikaty', { skip }, async () => {
    await page.reset();
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    assert.equal(await page.eval(`document.getElementById('view').hasAttribute('aria-live')`), false);
    const mismatched = await page.eval(`[...document.querySelectorAll('#view button[aria-label]')].filter(b => {
        const full = b.innerText.replace(/\\s+/g, ' ').trim().toLowerCase();
        const visible = full.length <= 12 ? full : b.innerText.trim().split('\\n')[0].trim().toLowerCase();
        return visible && !b.getAttribute('aria-label').toLowerCase().replace(/\\s+/g, ' ').startsWith(visible);
    }).map(b => b.innerText.replace(/\\s+/g, ' ').trim() + ' → ' + b.getAttribute('aria-label'))`);
    assert.deepEqual(mismatched, []);
    const unnamed = await page.eval(`[...document.querySelectorAll('dialog')].filter(d => { const id = d.getAttribute('aria-labelledby'); return !id || !document.getElementById(id); }).map(d => d.id)`);
    assert.deepEqual(unnamed, []);

    await page.eval(`document.getElementById('toolsBtn').focus(), true`);
    await page.click('#toolsBtn');
    await page.waitFor(`document.getElementById('toolsModal').open`);
    assert.equal(await page.eval(`document.getElementById('toolsModal').contains(document.activeElement)`), true);
    assert.equal(await page.eval(`document.getElementById(document.getElementById('toolsModal').getAttribute('aria-labelledby')).textContent.trim().length > 0`), true);
    await page.eval(`document.querySelector('[data-tool="stats"]').focus(), true`);
    await page.click('[data-tool="stats"]');
    await page.waitFor(`document.getElementById('statsModal')?.open`);
    assert.equal(await page.eval(`document.getElementById(document.getElementById('statsModal').getAttribute('aria-labelledby')).textContent`), 'Statystyki planu');
    await page.key('Escape');
    await page.waitFor(`!document.getElementById('statsModal').open`);
    await page.waitFor(`document.activeElement && document.activeElement.id === 'toolsBtn'`, 2000);

    await page.eval(`document.getElementById('settingsBtn').focus(), true`);
    await page.click('#settingsBtn');
    await page.waitFor(`document.getElementById('settingsModal').open`);
    await page.key('Escape');
    await page.waitFor(`!document.getElementById('settingsModal').open && document.activeElement.id === 'settingsBtn'`, 2000);

    const live = `document.querySelector('p.sr-only[aria-live]')`;
    assert.equal(await page.eval(`${live}.textContent`), '');
    await page.eval(`stepByView(1), true`);
    await page.waitFor(`${live}.textContent.length > 0`);
    assert.match(await page.eval(`${live}.textContent`), /^(Tydzień .+|[A-ZŚ][a-ząęłńóśźż]+, .+): (\d+ zaj|brak zajęć)/);
    assert.ok(await page.eval(`${live}.textContent.length < 80`));
    assert.equal(await page.eval(`document.getElementById('notice').getAttribute('aria-hidden')`), 'true');
    await page.eval(`setNotice('Testowy błąd', true), true`);
    await page.waitFor(`document.querySelector('p.sr-only[role=alert]').textContent === 'Testowy błąd'`);
    await page.eval(`setNotice('Skopiowano link', false), true`);
    await page.waitFor(`${live}.textContent === 'Skopiowano link'`);
    assert.deepEqual(page.errors, []);
});

test('ustawienia pokazują wersję i informują o nowszej', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    const version = await page.eval(`document.querySelector('meta[name="watplan-version"]').content`);
    assert.ok(version.length >= 7);
    await page.click('#settingsBtn');
    await page.waitFor(`document.getElementById('settingsModal').open`);
    await new Promise(r => setTimeout(r, 300));
    assert.equal(await page.text('.setting.status .version b'), version);
    assert.equal(await page.eval(`document.querySelector('.version-update')`), null, 'przy tej samej wersji nie ma przycisku');
    await page.eval(`document.getElementById('settingsModal').close(), true`);
    await page.eval(`(() => { const real = window.fetch; window.fetch = (url, opts) => String(url).includes('/api/status') ? Promise.resolve(new Response(JSON.stringify({ version: 'nowa1234' }), { headers: { 'Content-Type': 'application/json' } })) : real(url, opts); return true; })()`);
    await page.click('#settingsBtn');
    await page.waitFor(`document.querySelector('.version-update') !== null`);
    assert.match(await page.text('.version-update'), /Dostępna nowsza wersja/);
    assert.deepEqual(page.errors, []);
});

test('mapa kampusu: przycisk przy znanym budynku, ładowanie na żądanie, komunikat bez WebGL', { skip }, async () => {
    await page.reset();
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    const cases = JSON.parse(await page.eval(`JSON.stringify(['19 36', '233 K', '032 S', '0.19/135', 'aula/ 135', '111 58', 'AW 3/038', '2.01 54A', 'online'].map(roomBuilding))`));
    assert.deepEqual(cases, ['36', 'K', 'S', '135', '135', '58', 'AW 3', '54A', '']);
    assert.equal(await page.eval(`MAP_BUILDINGS.has('54A')`), false);
    const loaded = `[...document.querySelectorAll('script[src], link[href]')].map(e => e.src || e.href).filter(u => /map\\.js|maplibre|campus\\.json/.test(u)).length`;
    assert.equal(await page.eval(loaded), 0, 'mapa nie może ładować się na starcie');
    await page.click('.ev-card');
    await page.waitFor(`document.getElementById('seriesModal').open`);
    assert.match(await page.text('.map-btn'), /Pokaż na mapie · Budynek 36/);
    await page.click('.map-btn');
    await page.waitFor(`document.getElementById('mapModal')?.open`);
    assert.equal(await page.text('#mapTitle'), 'Budynek 36');
    assert.match(await page.eval(`document.getElementById('mapGo').href`), /destination=52\.25\d+,20\.90\d+&travelmode=walking/);
    await page.waitFor(`document.querySelector('#campusMap canvas') !== null || document.querySelector('.map-message') !== null`, 15000);
    const message = await page.eval(`(document.querySelector('.map-message') || {}).textContent || ''`);
    if (message) assert.match(message, /Prowadź/);
    assert.deepEqual(page.errors, []);
    assert.deepEqual(page.policyErrors, []);
});

test('kliknięcie sali w znanym budynku otwiera od razu mapę, reszta karty otwiera szczegóły', { skip }, async () => {
    await page.reset();
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-place.on-map').length > 0`);
    await page.click('.ev-place.on-map');
    await page.waitFor(`document.getElementById('mapModal')?.open`, 10000);
    assert.equal(await page.eval(`document.getElementById('seriesModal').open`), false);
    await page.eval(`document.getElementById('mapModal').close()`);
    await page.eval(`document.querySelector('.ev-place.on-map').closest('.ev-card').querySelector('.ev-subject').click()`);
    await page.waitFor(`document.getElementById('seriesModal').open`);
    assert.deepEqual(page.errors, []);
});

test('wychowanie fizyczne bez sali prowadzi na mapę do Studium WF', { skip }, async () => {
    await page.reset();
    await page.goto(app('&view=week'));
    await page.waitFor(`document.querySelectorAll('.wk-cell').length > 0`);
    const opened = await page.eval(`(() => {
        const e = allEvents.find(x => /wychowanie fizyczne/i.test(x.subject) && !x.room);
        if (!e) return false;
        openSeries(e);
        return true;
    })()`);
    assert.ok(opened, 'w danych testowych brak WF bez sali');
    await page.waitFor(`document.getElementById('seriesModal').open`);
    assert.equal(await page.text('#seriesModal .map-btn'), 'Pokaż na mapie · Studium WF');
    assert.deepEqual(page.errors, []);
});

test('szczegóły zajęć bez znanego budynku nie mają przycisku mapy ani napisu null', { skip }, async () => {
    await page.reset();
    await page.goto(app('&view=week'));
    await page.waitFor(`document.querySelectorAll('.wk-cell').length > 0`);
    const opened = await page.eval(`(() => {
        MAP_BUILDINGS.delete('SWF');
        const e = allEvents.find(x => (seriesByKey[seriesKey(x)] || []).every(y => !MAP_BUILDINGS.has(eventBuilding(y))));
        if (!e) return false;
        openSeries(e);
        return true;
    })()`);
    assert.ok(opened, 'w danych testowych brak zajęć bez znanego budynku');
    await page.waitFor(`document.getElementById('seriesModal').open`);
    assert.equal(await page.eval(`document.querySelector('#seriesModal .map-btn')`), null);
    assert.doesNotMatch(await page.eval(`document.getElementById('seriesBody').textContent`), /null|undefined/);
    assert.deepEqual(page.errors, []);
});

test('wolne sale pokazują sale dla wybranego bloku', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    const toolsScripts = `[...document.querySelectorAll('script[src]')].filter(s => s.src.includes('/tools.js?v=')).length`;
    assert.equal(await page.eval(toolsScripts), 0, 'tools.js nie powinien ładować się na starcie');
    await page.click('#toolsBtn');
    await page.click('[data-tool="rooms"]');
    await page.waitFor(`document.querySelectorAll('.room-chip').length > 0 || !!document.querySelector('#roomsBody .empty-day')`);
    assert.ok(await page.eval(`document.querySelectorAll('.room-building').length > 0`));
    assert.ok(await page.eval(`[...document.querySelectorAll('.room-building')].some(b => b.querySelector('.map-link'))`), 'brak linku „Mapa” przy budynku');
    assert.match(await page.text('#roomsSub'), /\d{2}:\d{2}–\d{2}:\d{2}/);
    assert.equal(await page.eval(toolsScripts), 1);
});

test('porównanie grup pokazuje plany obok siebie', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.click('#toolsBtn');
    await page.click('[data-tool="compare"]');
    await page.eval(`(() => { const i = document.getElementById('compareSearch'); i.value = 'WMT26AX1'; i.dispatchEvent(new Event('input')); return true; })()`);
    await page.waitFor(`document.querySelectorAll('#compareResults button').length > 0`);
    await page.click('#compareResults button');
    await page.waitFor(`document.querySelectorAll('.cmp-day').length > 0 && document.querySelectorAll('.cmp-chip').length === 2`);
    assert.ok(await page.eval(`document.querySelectorAll('.cmp-cell:not(.empty)').length > 0`));
});

test('gdzie odrobić szuka zajęć w innych grupach rocznika', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.click('.ev-card');
    await page.waitFor(`!!document.querySelector('.makeup')`);
    await page.waitFor(`!document.querySelector('.makeup-results')?.textContent.includes('Szukam')`);
    assert.match(await page.text('.makeup-results'), /WMT26/);
});

test('sesja i egzaminy pokazuje ostatnie zajęcia z przedmiotów', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.click('#toolsBtn');
    await page.click('[data-tool="exams"]');
    await page.waitFor(`document.getElementById('examsModal').open && document.querySelectorAll('.ex-lasts li').length > 0`);
    assert.ok(await page.eval(`[...document.querySelectorAll('.ex-h')].some(h => h.textContent === 'Egzaminy')`));
    assert.match(await page.text('.ex-lasts li .ex-when b'), /\d/);
    assert.deepEqual(page.errors, []);
});

test('miesiąc do PDF buduje arkusz wydruku', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.eval('window.print = () => {}; true');
    await page.click('#toolsBtn');
    await page.click('[data-tool="print"]');
    await page.waitFor(`document.querySelector('.pr-top h1') !== null`);
    assert.match(await page.text('.pr-top h1'), /listopad 2026/);
    assert.ok(await page.eval(`document.querySelectorAll('.pr-ev').length > 20`));
    assert.deepEqual(page.errors, []);
});
