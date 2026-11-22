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

        await page.eval(`selectPlan('WMT26AX3S1', 'WMT26AX3S1'), true`);
        await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
        await page.waitFor(`document.querySelector('#notice').textContent.includes('zapisaną wersję')`);

        await page.click('#refreshButton');
        await page.waitFor(`document.querySelector('#notice').textContent.includes('Brak połączenia')`);
        assert.doesNotMatch(await page.text('#notice'), /Failed to fetch/);
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
    await page.eval(`selectPlan('WMT26AX1S1', 'WMT26AX1S1'), true`);
    await page.waitFor(`location.pathname === '/plan/WMT26AX1S1' && !location.search.includes('plan=')`);
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

test('wolne sale pokazują sale dla wybranego bloku', { skip }, async () => {
    await page.goto(app());
    await page.waitFor(`document.querySelectorAll('.ev-card').length > 0`);
    await page.click('#toolsBtn');
    await page.click('[data-tool="rooms"]');
    await page.waitFor(`document.querySelectorAll('.room-chip').length > 0 || !!document.querySelector('#roomsBody .empty-day')`);
    assert.ok(await page.eval(`document.querySelectorAll('.room-building').length > 0`));
    assert.match(await page.text('#roomsSub'), /\d{2}:\d{2}–\d{2}:\d{2}/);
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
    assert.match(await page.text('.pr-top h1'), /listopad 2026/);
    assert.ok(await page.eval(`document.querySelectorAll('.pr-ev').length > 20`));
    assert.deepEqual(page.errors, []);
});
