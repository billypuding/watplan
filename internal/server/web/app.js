'use strict';

const DAY_NAMES = ['Poniedziałek', 'Wtorek', 'Środa', 'Czwartek', 'Piątek', 'Sobota', 'Niedziela'];
const DAY_SHORT = ['Pn', 'Wt', 'Śr', 'Cz', 'Pt', 'So', 'Nd'];
const DAY_ABBR = ['pon.', 'wt.', 'śr.', 'czw.', 'pt.', 'sob.', 'niedz.'];
const MONTHS_GEN = ['stycznia', 'lutego', 'marca', 'kwietnia', 'maja', 'czerwca', 'lipca', 'sierpnia', 'września', 'października', 'listopada', 'grudnia'];
const MONTHS_NOM = ['styczeń', 'luty', 'marzec', 'kwiecień', 'maj', 'czerwiec', 'lipiec', 'sierpień', 'wrzesień', 'październik', 'listopad', 'grudzień'];
const MONTHS_SHORT = ['sty', 'lut', 'mar', 'kwi', 'maj', 'cze', 'lip', 'sie', 'wrz', 'paź', 'lis', 'gru'];
const SLOTS = [
    ['08:00', '09:35'], ['09:50', '11:25'], ['11:40', '13:15'], ['13:30', '15:05'],
    ['16:00', '17:35'], ['17:50', '19:25'], ['19:40', '21:15'],
];
const LONG_GAP_MIN = 30;
const NOTICE_MS = 5000;
const TICK_MS = 15000;
const SETTINGS_KEY = 'settings';
const SETTINGS_VERSION = 2;

const $ = (id) => document.getElementById(id);
const app = $('app');
const view = $('view');
const groupBtn = $('groupBtn');
const groupLabel = $('groupLabel');
const groupModal = $('groupModal');
const favBar = $('favBar');
const favSection = $('favSection');
const favList = $('favList');
const planSearch = $('planSearch');
const searchResults = $('searchResults');
const todayBtn = $('todayBtn');
const settingsBtn = $('settingsBtn');
const settingsModal = $('settingsModal');
const optLive = $('optLive');
const optDimPast = $('optDimPast');
const downloadIcsBtn = $('downloadIcs');
const refreshButton = $('refreshButton');
const fetchedLabel = $('fetched');
const modifiedLabel = $('modified');
const notice = $('notice');
const seriesModal = $('seriesModal');
const seriesBack = $('seriesBack');
const seriesSubject = $('seriesSubject');
const seriesSub = $('seriesSub');
const seriesBody = $('seriesBody');
const staleBanner = $('staleBanner');
const STALE_AFTER_MS = 24 * 3600 * 1000;
const PLAN_CACHE_KEY = 'planCache';
const PLAN_CACHE_SIZE = 5;

let allPlans = [];
let currentPlanId = '';
let allEvents = [];
let eventsByDate = {};
let seriesByKey = {};
let selectedDate = '';
let loadCounter = 0;
let noticeTimer = null;
let status = 'idle';
let statusMessage = '';

const defaults = { v: SETTINGS_VERSION, theme: 'auto', view: 'agenda', live: true, dimPast: true };

function loadSettings() {
    let stored = {};
    try { stored = JSON.parse(localStorage.getItem(SETTINGS_KEY) || '{}') || {}; } catch (_) { stored = {}; }
    const legacyTheme = safeGet('theme');
    const merged = { ...defaults, ...(legacyTheme ? { theme: legacyTheme } : {}), ...stored };
    if (merged.v !== SETTINGS_VERSION) {
        merged.v = SETTINGS_VERSION;
        merged.view = 'agenda';
    }
    if (!['agenda', 'week', 'month'].includes(merged.view)) merged.view = 'agenda';
    return merged;
}

function safeGet(key) { try { return localStorage.getItem(key); } catch (_) { return null; } }
function safeSet(key, value) { try { localStorage.setItem(key, value); } catch (_) { } }

const settings = loadSettings();
const saveSettings = () => safeSet(SETTINGS_KEY, JSON.stringify(settings));

const urlView = { day: 'agenda', dzien: 'agenda', agenda: 'agenda', week: 'week', month: 'month' }[new URLSearchParams(window.location.search).get('view')];
if (urlView) settings.view = urlView;

const darkQuery = window.matchMedia('(prefers-color-scheme: dark)');
function applyTheme() {
    const theme = settings.theme === 'auto' ? (darkQuery.matches ? 'dark' : 'light') : settings.theme;
    document.documentElement.setAttribute('data-theme', theme);
}
darkQuery.addEventListener('change', applyTheme);

function syncSettingsUI() {
    document.querySelectorAll('.segmented').forEach(group => {
        const key = group.dataset.setting;
        group.querySelectorAll('button').forEach(btn => {
            btn.setAttribute('aria-pressed', String(btn.dataset.value === settings[key]));
        });
    });
    optLive.checked = settings.live;
    optDimPast.checked = settings.dimPast;
}

document.querySelectorAll('.segmented button').forEach(btn => {
    btn.addEventListener('click', () => {
        const key = btn.closest('.segmented').dataset.setting;
        settings[key] = btn.dataset.value;
        saveSettings();
        syncSettingsUI();
        if (key === 'theme') applyTheme();
        if (key === 'view') render();
    });
});

optLive.addEventListener('change', () => { settings.live = optLive.checked; saveSettings(); tickLive(); });
optDimPast.addEventListener('change', () => { settings.dimPast = optDimPast.checked; saveSettings(); tickLive(); });

applyTheme();
syncSettingsUI();

function h(tag, attrs, ...children) {
    const node = document.createElement(tag);
    if (attrs) {
        for (const [k, v] of Object.entries(attrs)) {
            if (v === undefined || v === null || v === false) continue;
            if (k === 'class') node.className = v;
            else if (k === 'style') node.style.cssText = v;
            else if (k.startsWith('on')) node.addEventListener(k.slice(2), v);
            else node.setAttribute(k, v === true ? '' : v);
        }
    }
    for (const c of children.flat()) {
        if (c === null || c === undefined || c === false) continue;
        node.append(c instanceof Node ? c : document.createTextNode(String(c)));
    }
    return node;
}

function svg(paths, size = 14) {
    const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    s.setAttribute('width', size);
    s.setAttribute('height', size);
    s.setAttribute('viewBox', '0 0 24 24');
    s.setAttribute('fill', 'none');
    s.setAttribute('stroke', 'currentColor');
    s.setAttribute('stroke-width', '2');
    s.setAttribute('stroke-linecap', 'round');
    s.setAttribute('stroke-linejoin', 'round');
    s.setAttribute('aria-hidden', 'true');
    s.innerHTML = paths;
    return s;
}
const ICON_BUILDING = '<path d="M4 21V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v16"/><path d="M16 9h2a2 2 0 0 1 2 2v10"/><path d="M2 21h20"/><path d="M8 7h4M8 11h4M8 15h4"/>';
const ICON_USER = '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>';
const ICON_LEFT = '<path d="m15 18-6-6 6-6"/>';
const ICON_RIGHT = '<path d="m9 18 6-6-6-6"/>';
const ICON_CHECK = '<path d="M20 6 9 17l-5-5"/>';

const pad = (n) => String(n).padStart(2, '0');
const toISO = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
function parseISO(str) { const [y, m, d] = str.split('-').map(Number); return new Date(y, m - 1, d); }
function addDays(date, n) { const r = new Date(date); r.setDate(r.getDate() + n); return r; }
const addDaysISO = (iso, n) => toISO(addDays(parseISO(iso), n));
const dayIndex = (date) => (date.getDay() + 6) % 7;
const mondayOf = (iso) => { const d = parseISO(iso); return toISO(addDays(d, -dayIndex(d))); };
const toMin = (t) => { const [hh, mm] = t.split(':').map(Number); return hh * 60 + mm; };

function isoWeek(date) {
    const d = new Date(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()));
    const day = d.getUTCDay() || 7;
    d.setUTCDate(d.getUTCDate() + 4 - day);
    const yearStart = new Date(Date.UTC(d.getUTCFullYear(), 0, 1));
    return Math.ceil(((d - yearStart) / 86400000 + 1) / 7);
}

function warsawNow() {
    return new Date(new Date().toLocaleString('en-US', { timeZone: 'Europe/Warsaw' }));
}
const todayISO = () => toISO(warsawNow());

function eventRange(e) {
    const [y, m, d] = e.date.split('-').map(Number);
    const [sh, sm] = e.start.split(':').map(Number);
    const [eh, em] = e.end.split(':').map(Number);
    return [new Date(y, m - 1, d, sh, sm).getTime(), new Date(y, m - 1, d, eh, em).getTime()];
}

function formatMinutes(mins) {
    if (mins < 60) return `${mins} min`;
    const hh = Math.floor(mins / 60);
    const mm = mins % 60;
    return mm ? `${hh} h ${mm} min` : `${hh} h`;
}

function longDate(iso) {
    const d = parseISO(iso);
    return `${d.getDate()} ${MONTHS_GEN[d.getMonth()]}`;
}
function shortDate(iso) {
    const d = parseISO(iso);
    return `${DAY_ABBR[dayIndex(d)]} ${d.getDate()} ${MONTHS_SHORT[d.getMonth()]}`;
}

function plural(n, one, few, many) {
    if (n === 1) return one;
    const d = n % 10, dd = n % 100;
    return d >= 2 && d <= 4 && (dd < 12 || dd > 14) ? few : many;
}
const classesWord = (n) => `${n} ${plural(n, 'zajęcia', 'zajęcia', 'zajęć')}`;

const hueCache = {};
function hueOf(hex) {
    if (hueCache[hex]) return hueCache[hex];
    const m = /^#([0-9a-f]{6})$/i.exec(hex || '');
    let res = { h: 220, s: 0 };
    if (m) {
        const n = parseInt(m[1], 16);
        const r = (n >> 16 & 255) / 255, g = (n >> 8 & 255) / 255, b = (n & 255) / 255;
        const mx = Math.max(r, g, b), mn = Math.min(r, g, b), d = mx - mn;
        let hue = 0;
        if (d) {
            hue = mx === r ? ((g - b) / d) % 6 : mx === g ? (b - r) / d + 2 : (r - g) / d + 4;
            hue = Math.round(hue * 60);
            if (hue < 0) hue += 360;
        }
        res = { h: hue, s: d > 0.08 ? 1 : 0 };
    }
    return (hueCache[hex] = res);
}
function tint(node, hex) {
    const { h: hue, s } = hueOf(hex);
    node.style.setProperty('--h', hue);
    node.style.setProperty('--s', s);
    return node;
}

const SMALL_WORDS = new Set(['i', 'w', 'z', 'do', 'na', 'we', 'ze', 'o', 'od', 'dla', 'oraz', 'a', '-']);
function abbreviate(subject) {
    const words = subject.split(/\s+/).filter(Boolean);
    const big = words.filter(w => !SMALL_WORDS.has(w.toLowerCase()) && !/^\d+$/.test(w) && !/^[IVX]+$/.test(w));
    const nums = words.filter(w => /^\d+$/.test(w) || /^[IVX]+$/.test(w));
    let ab;
    if (big.length <= 1) {
        const w = big[0] || subject;
        ab = w === w.toUpperCase() ? w.slice(0, 5) : w.slice(0, 3);
    } else {
        ab = words.filter(w => !/^\d+$/.test(w)).map(w => {
            if (SMALL_WORDS.has(w.toLowerCase())) return w === '-' ? '' : w[0].toLowerCase();
            return w[0].toUpperCase();
        }).join('');
    }
    return nums.length ? `${ab} ${nums.join(' ')}` : ab;
}

function typeLabel(type) {
    if (!type) return 'Zajęcia';
    return type.charAt(0).toUpperCase() + type.slice(1);
}
function typeShort(type) {
    const t = (type || '').toLowerCase();
    if (t.startsWith('wykład')) return 'wykł.';
    if (t.startsWith('ćwicz')) return 'ćw.';
    if (t.startsWith('labor')) return 'lab.';
    if (t.startsWith('semin')) return 'sem.';
    if (t.startsWith('projekt')) return 'proj.';
    return t;
}
function lastLabel(type) {
    const t = (type || '').toLowerCase();
    if (t.startsWith('wykład')) return 'Ostatni wykład';
    if (t.startsWith('ćwicz')) return 'Ostatnie ćwiczenia';
    if (t.startsWith('labor')) return 'Ostatnie laboratorium';
    if (t.startsWith('semin')) return 'Ostatnie seminarium';
    if (t.startsWith('projekt')) return 'Ostatni projekt';
    if (t.startsWith('egzamin') || t.includes('(e')) return 'Egzamin';
    return 'Ostatnie zajęcia';
}

function setNotice(text, isError) {
    clearTimeout(noticeTimer);
    notice.textContent = text;
    notice.classList.toggle('error', Boolean(isError));
    notice.classList.toggle('show', Boolean(text));
    if (text) noticeTimer = setTimeout(() => notice.classList.remove('show'), NOTICE_MS);
}

async function fetchJSON(url, options) {
    const response = await fetch(url, options);
    let body = null;
    try { body = await response.json(); } catch (_) { body = null; }
    if (!response.ok) throw new Error((body && body.error) || `Błąd serwera (${response.status})`);
    return body;
}

const seriesKey = (e) => `${e.subject}\u0000${e.type || ''}`;

function indexEvents(events) {
    eventsByDate = {};
    seriesByKey = {};
    const sorted = events.slice().sort((a, b) => (a.date + a.start).localeCompare(b.date + b.start));
    for (const e of sorted) {
        (eventsByDate[e.date] ||= []).push(e);
        (seriesByKey[seriesKey(e)] ||= []).push(e);
    }
    for (const list of Object.values(seriesByKey)) {
        list.forEach((e, i) => { e._n = i + 1; e._total = list.length; });
    }
    allEvents = sorted;
}

function pickInitialDate() {
    if (allEvents.length === 0) return todayISO();
    const today = todayISO();
    const first = allEvents[0].date;
    const last = allEvents[allEvents.length - 1].date;
    if (today < first) return first;
    if (today > last) return last;
    return today;
}

function nextDateWithEvents(fromISO, dir = 1) {
    const dates = Object.keys(eventsByDate).sort();
    if (dir > 0) return dates.find(d => d > fromISO) || '';
    return dates.reverse().find(d => d < fromISO) || '';
}

async function loadPlans() {
    const params = new URLSearchParams(window.location.search);
    const target = params.get('plan') || safeGet('selectedPlan');
    if (target) {
        currentPlanId = target;
        groupLabel.textContent = target;
        renderFavorites();
        loadPlan();
    } else {
        status = 'loading';
        render();
    }
    try {
        allPlans = await WatGroups.load();
        const found = target && allPlans.find(p => p.id === target);
        if (found) {
            if (currentPlanId === found.id) {
                groupLabel.textContent = found.name;
                safeSet('selectedPlan', found.id);
            }
        } else if (currentPlanId === target) {
            loadCounter++;
            currentPlanId = '';
            groupLabel.textContent = 'Wybierz grupę';
            status = 'idle';
            render();
        } else if (!currentPlanId) {
            status = 'idle';
            render();
        }
        renderFavorites();
    } catch (err) {
        if (currentPlanId) return;
        status = 'error';
        statusMessage = err.message;
        render();
    }
}

function renderSearchResults() {
    WatGroups.render(searchResults, planSearch.value, {
        currentId: currentPlanId,
        onSelect: (plan) => selectPlan(plan.id, plan.name),
    });
}

function renderFavorites() {
    const pick = (fav) => selectPlan(fav.id, fav.name);
    const favs = WatGroups.favorites();
    WatGroups.renderFavorites(favBar, { currentId: currentPlanId, onSelect: pick });
    favBar.hidden = !favs.some(f => f.id !== currentPlanId);
    favSection.hidden = WatGroups.renderFavorites(favList, { currentId: currentPlanId, onSelect: pick, removable: true }) === 0;
}

WatGroups.onFavoritesChange(renderFavorites);

function openGroupPicker() {
    planSearch.value = '';
    renderFavorites();
    renderSearchResults();
    groupModal.showModal();
    if (window.matchMedia('(min-width: 720px)').matches) planSearch.focus();
}

planSearch.addEventListener('input', renderSearchResults);
planSearch.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter') return;
    const plan = WatGroups.resolve(planSearch.value);
    if (plan) selectPlan(plan.id, plan.name);
});
groupBtn.addEventListener('click', openGroupPicker);

function selectPlan(id, name) {
    groupModal.close();
    if (id === currentPlanId) return;
    currentPlanId = id;
    groupLabel.textContent = name;
    safeSet('selectedPlan', currentPlanId);
    const url = new URL(window.location);
    url.searchParams.set('plan', currentPlanId);
    window.history.replaceState(window.history.state, '', url);
    selectedDate = '';
    renderFavorites();
    loadPlan();
}

function readPlanCache() {
    try {
        const cache = JSON.parse(safeGet(PLAN_CACHE_KEY));
        return Array.isArray(cache) ? cache.filter(c => c && c.id && typeof c.raw === 'string') : [];
    } catch (_) {
        return [];
    }
}

function cachedPlan(id) {
    const entry = readPlanCache().find(c => c.id === id);
    if (!entry) return null;
    try { return { raw: entry.raw, data: JSON.parse(entry.raw) }; } catch (_) { return null; }
}

function storePlan(id, raw) {
    const cache = [{ id, raw }, ...readPlanCache().filter(c => c.id !== id)].slice(0, PLAN_CACHE_SIZE);
    while (cache.length) {
        try {
            localStorage.setItem(PLAN_CACHE_KEY, JSON.stringify(cache));
            return;
        } catch (_) {
            cache.pop();
        }
    }
}

function applyPlanData(data) {
    fetchedLabel.textContent = data.fetched_at || 'Brak';
    modifiedLabel.textContent = data.modified_at || 'Brak';
    updateStaleBanner(data.fetched_at);
    indexEvents(data.events || []);
    if (!selectedDate) selectedDate = pickInitialDate();
    status = 'ready';
}

async function loadPlan() {
    if (!currentPlanId) return;
    const requestId = ++loadCounter;
    const planId = currentPlanId;
    const cached = cachedPlan(planId);
    if (cached) {
        applyPlanData(cached.data);
    } else {
        status = 'loading';
    }
    render();
    try {
        const response = await fetch('/api/plan/' + encodeURIComponent(planId));
        const text = await response.text();
        let data = null;
        try { data = JSON.parse(text); } catch (_) { data = null; }
        if (!response.ok || !data) throw new Error((data && data.error) || `Błąd serwera (${response.status})`);
        if (requestId !== loadCounter) return;
        storePlan(planId, text);
        if (cached && cached.raw === text) return;
        applyPlanData(data);
    } catch (err) {
        if (requestId !== loadCounter) return;
        if (cached) {
            setNotice('Nie udało się pobrać planu, pokazuję zapisaną wersję', true);
            return;
        }
        indexEvents([]);
        updateStaleBanner('');
        fetchedLabel.textContent = '-';
        modifiedLabel.textContent = '-';
        status = 'error';
        statusMessage = err.message;
    }
    render();
}

function updateStaleBanner(fetchedAt) {
    const m = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})/.exec(fetchedAt || '');
    if (!m) { staleBanner.classList.add('hidden'); return; }
    const fetched = new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]);
    const stale = warsawNow() - fetched > STALE_AFTER_MS;
    staleBanner.classList.toggle('hidden', !stale);
    if (stale) {
        staleBanner.textContent = `Plan pobrany ${fetched.getDate()} ${MONTHS_GEN[fetched.getMonth()]} o ${m[4]}:${m[5]} – może być nieaktualny. W razie wątpliwości sprawdź oficjalny plan WAT.`;
    }
}

function render() {
    app.classList.toggle('wide', settings.view === 'week' && status === 'ready');
    todayBtn.classList.toggle('hidden', status !== 'ready' || !selectedDate || !todayOffScreen());
    refreshButton.classList.toggle('hidden', !currentPlanId);
    if (status === 'loading') { view.replaceChildren(h('div', { class: 'message' }, 'Ładowanie...')); return; }
    if (status === 'error') { view.replaceChildren(h('div', { class: 'message error' }, statusMessage)); return; }
    if (!currentPlanId) {
        view.replaceChildren(h('div', { class: 'empty-day' },
            h('b', null, 'Wybierz swoją grupę'),
            'Wpisz nazwę grupy, np. WEL26EY4S0, a plan zapamięta się na tym urządzeniu.',
            h('br'),
            h('button', { type: 'button', class: 'link-btn', onclick: openGroupPicker }, 'Wybierz grupę'),
        ));
        return;
    }
    if (allEvents.length === 0) { view.replaceChildren(h('div', { class: 'message' }, 'Brak zajęć w planie')); return; }
    if (!selectedDate) selectedDate = pickInitialDate();

    if (settings.view === 'week') renderWeek();
    else if (settings.view === 'month') renderMonth();
    else renderAgenda();
    tickLive();
}

function todayOffScreen() {
    const today = todayISO();
    if (settings.view === 'week') return mondayOf(selectedDate) !== mondayOf(today);
    return selectedDate !== today;
}

function renderAgenda() {
    const d = parseISO(selectedDate);
    const count = (eventsByDate[selectedDate] || []).length;
    const sub = `${longDate(selectedDate)} · tydzień ${isoWeek(d)}` + (count ? ` · ${classesWord(count)}` : '');
    view.replaceChildren(
        h('div', { class: 'day-title' }, h('h1', null, DAY_NAMES[dayIndex(d)]), h('p', null, sub)),
        weekStrip(),
        dayList(selectedDate),
    );
}

function weekStrip() {
    const monday = parseISO(mondayOf(selectedDate));
    const today = todayISO();
    const pills = [];
    for (let i = 0; i < 7; i++) {
        const iso = toISO(addDays(monday, i));
        const has = Boolean(eventsByDate[iso]);
        const cls = ['day-pill', has ? 'has' : 'empty', iso === selectedDate && 'sel', iso === today && 'today'].filter(Boolean).join(' ');
        pills.push(h('button', {
            type: 'button', class: cls, 'aria-pressed': String(iso === selectedDate),
            'aria-label': `${DAY_NAMES[i]}, ${longDate(iso)}${has ? '' : ', brak zajęć'}`,
            onclick: () => { selectedDate = iso; render(); },
        }, h('span', { class: 'dn' }, DAY_SHORT[i]), h('span', { class: 'dd' }, parseISO(iso).getDate()), h('span', { class: 'dot' })));
    }
    return h('div', { class: 'week-strip' },
        h('button', { type: 'button', class: 'arrow', 'aria-label': 'Poprzedni tydzień', onclick: () => step(-7) }, svg(ICON_LEFT, 20)),
        pills,
        h('button', { type: 'button', class: 'arrow', 'aria-label': 'Następny tydzień', onclick: () => step(7) }, svg(ICON_RIGHT, 20)),
    );
}

function dayList(iso) {
    const events = eventsByDate[iso] || [];
    if (events.length === 0) {
        const next = nextDateWithEvents(iso, 1);
        return h('div', { class: 'empty-day' },
            h('b', null, 'Wolne'),
            'Brak zajęć tego dnia.',
            next ? h('br') : null,
            next ? h('button', { type: 'button', class: 'link-btn', onclick: () => { selectedDate = next; render(); } },
                `Następne zajęcia: ${shortDate(next)}`) : null,
        );
    }
    const rows = [];
    events.forEach((e, i) => {
        if (i > 0) {
            const gap = toMin(e.start) - toMin(events[i - 1].end);
            if (gap >= LONG_GAP_MIN) rows.push(h('div', { class: 'gap-row' }, `przerwa ${formatMinutes(gap)}`));
        }
        rows.push(h('div', { class: 'ev-row' },
            h('div', { class: 'ev-time' }, h('b', null, e.start), h('span', null, e.end)),
            eventCard(e),
        ));
    });
    return h('div', { class: 'day-list' }, rows);
}

function splitRooms(room) {
    const rooms = (room || '').split(/\s*[;,]\s*/).map(r => r.trim()).filter(Boolean);
    return { main: rooms[0] || '', more: Math.max(rooms.length - 1, 0) };
}

function eventCard(e) {
    const [start, end] = eventRange(e);
    const { main: room, more } = splitRooms(e.room);
    const card = h('button', {
        type: 'button', class: 'ev-card tint', 'data-start': start, 'data-end': end,
        'aria-label': `${e.subject}, ${typeLabel(e.type)}${e.room ? ', sala ' + e.room : ''}, ${e._n} z ${e._total}. Pokaż wszystkie`,
        onclick: () => openSeries(e),
    },
        h('div', { class: 'ev-subject' }, e.subject),
        h('div', { class: 'ev-type sub' }, h('span', { class: 'dot' }), typeLabel(e.type), h('span', { class: 'count' }, `${e._n}/${e._total}`)),
        (room || e.teacher) ? h('div', { class: 'ev-meta sub' },
            room ? h('span', { class: 'ev-place', title: e.room }, svg(ICON_BUILDING, 16), h('b', null, room),
                more ? ` +${more} ${plural(more, 'sala', 'sale', 'sal')}` : null) : null,
            e.teacher ? h('span', { class: 'ev-person' }, svg(ICON_USER, 16), e.teacher) : null,
        ) : null,
        h('div', { class: 'ev-live' },
            h('div', { class: 'row' }, h('span', null, 'Trwa teraz'), h('span', { class: 'remaining' })),
            h('div', { class: 'track' }, h('div', { class: 'fill' })),
        ),
    );
    return tint(card, e.color);
}

function slotStart(t) {
    const m = toMin(t);
    let idx = 0;
    SLOTS.forEach(([s], i) => { if (toMin(s) <= m) idx = i; });
    return idx;
}
function slotEnd(t) {
    const m = toMin(t);
    const idx = SLOTS.findIndex(([, e]) => toMin(e) >= m);
    return idx === -1 ? SLOTS.length - 1 : idx;
}

function renderWeek() {
    const mondayISO = mondayOf(selectedDate);
    const monday = parseISO(mondayISO);
    const sunday = addDays(monday, 6);
    const dates = Array.from({ length: 7 }, (_, i) => toISO(addDays(monday, i)));
    const events = dates.flatMap(d => eventsByDate[d] || []);
    const days = events.some(e => dayIndex(parseISO(e.date)) >= 5) ? 7 : 5;

    const range = monday.getMonth() === sunday.getMonth()
        ? `${monday.getDate()}–${sunday.getDate()} ${MONTHS_GEN[sunday.getMonth()]}`
        : `${monday.getDate()} ${MONTHS_SHORT[monday.getMonth()]} – ${sunday.getDate()} ${MONTHS_SHORT[sunday.getMonth()]}`;
    const nav = navRow(range, `tydzień ${isoWeek(monday)} · ${classesWord(events.length)}`, -7, 7);

    if (events.length === 0) {
        view.replaceChildren(nav, emptyWeek(mondayISO));
        return;
    }

    let minSlot = SLOTS.length - 1, maxSlot = 0;
    for (const e of events) { minSlot = Math.min(minSlot, slotStart(e.start)); maxSlot = Math.max(maxSlot, slotEnd(e.end)); }

    const grid = h('div', { class: 'week-grid', style: `--days:${days}` });
    const today = todayISO();
    for (let i = 0; i < days; i++) {
        const iso = dates[i];
        grid.append(h('div', { class: 'wk-head' + (iso === today ? ' today' : ''), style: `grid-row:1;grid-column:${i + 2}` },
            h('b', null, DAY_SHORT[i]), h('span', null, parseISO(iso).getDate())));
    }
    for (let s = minSlot; s <= maxSlot; s++) {
        const [st] = SLOTS[s];
        const [hh, mm] = st.split(':');
        grid.append(h('div', { class: 'wk-time', style: `grid-row:${s - minSlot + 2};grid-column:1` }, h('span', null, hh), h('span', null, mm)));
    }

    const buckets = {};
    for (const e of events) {
        const key = `${e.date}|${slotStart(e.start)}`;
        (buckets[key] ||= []).push(e);
    }
    for (const list of Object.values(buckets)) {
        const first = list[0];
        const col = dayIndex(parseISO(first.date)) + 2;
        const rowStart = slotStart(first.start) - minSlot + 2;
        const rowEnd = Math.max(...list.map(e => slotEnd(e.end))) - minSlot + 3;
        grid.append(h('div', { class: 'wk-slot', style: `grid-column:${col};grid-row:${rowStart} / ${rowEnd}` },
            list.map(weekCell)));
    }

    view.replaceChildren(nav, grid);
}

function emptyWeek(mondayISO) {
    const next = nextDateWithEvents(addDaysISO(mondayISO, 6), 1);
    return h('div', { class: 'empty-day' }, h('b', null, 'Wolny tydzień'), 'Brak zajęć w tym tygodniu.',
        next ? h('br') : null,
        next ? h('button', { type: 'button', class: 'link-btn', onclick: () => { selectedDate = next; render(); } }, `Następne zajęcia: ${shortDate(next)}`) : null);
}

function weekCell(e) {
    const [start, end] = eventRange(e);
    const cell = h('button', {
        type: 'button', class: 'wk-cell tint', 'data-start': start, 'data-end': end,
        title: `${e.start}–${e.end} ${e.subject}${e.room ? ', sala ' + e.room : ''}${e.teacher ? ', ' + e.teacher : ''}`,
        'aria-label': `${e.subject}, ${typeLabel(e.type)}, ${e.start}. Pokaż wszystkie`,
        onclick: () => openSeries(e),
    },
        h('span', { class: 'ab' }, abbreviate(e.subject)),
        h('span', { class: 'full' }, e.subject),
        h('span', { class: 'meta sub' },
            h('span', null, `${typeShort(e.type)} ${e._n}/${e._total}`),
            e.room ? h('span', null, e.room) : null),
    );
    return tint(cell, e.color);
}

function renderMonth() {
    const sel = parseISO(selectedDate);
    const year = sel.getFullYear(), month = sel.getMonth();
    const first = new Date(year, month, 1);
    const start = addDays(first, -dayIndex(first));
    const daysInMonth = new Date(year, month + 1, 0).getDate();
    const cells = Math.ceil((dayIndex(first) + daysInMonth) / 7) * 7;
    const monthCount = Object.keys(eventsByDate).filter(d => d.startsWith(`${year}-${pad(month + 1)}`))
        .reduce((n, d) => n + eventsByDate[d].length, 0);

    const nav = navRow(`${MONTHS_NOM[month]} ${year}`, classesWord(monthCount), 'prevMonth', 'nextMonth');
    const grid = h('div', { class: 'month-grid' }, DAY_SHORT.map(d => h('div', { class: 'mo-head' }, d)));
    const today = todayISO();
    for (let i = 0; i < cells; i++) {
        const day = addDays(start, i);
        const iso = toISO(day);
        const evs = eventsByDate[iso] || [];
        const cls = ['mo-day', day.getMonth() !== month && 'outside', iso === today && 'today', iso === selectedDate && 'sel'].filter(Boolean).join(' ');
        const dots = h('span', { class: 'mo-dots' }, evs.slice(0, 6).map(e => tint(h('span', { class: 'dot tint-dot' }), e.color)));
        grid.append(h('button', {
            type: 'button', class: cls, 'aria-pressed': String(iso === selectedDate),
            'aria-label': `${longDate(iso)}, ${evs.length ? classesWord(evs.length) : 'brak zajęć'}`,
            onclick: () => { selectedDate = iso; render(); },
        }, h('span', null, day.getDate()), dots));
    }

    view.replaceChildren(nav, grid,
        h('h2', { class: 'mo-day-title' }, `${DAY_NAMES[dayIndex(sel)]}, ${longDate(selectedDate)}`),
        dayList(selectedDate));
}

function navRow(title, subtitle, prev, next) {
    const go = (dir) => () => (typeof dir === 'number' ? step(dir) : stepMonth(dir === 'nextMonth' ? 1 : -1));
    return h('div', { class: 'nav-row' },
        h('button', { type: 'button', class: 'round-btn', 'aria-label': 'Wstecz', onclick: go(prev) }, svg(ICON_LEFT, 20)),
        h('div', { class: 'nav-label' }, h('b', null, title), h('small', null, subtitle)),
        h('button', { type: 'button', class: 'round-btn', 'aria-label': 'Dalej', onclick: go(next) }, svg(ICON_RIGHT, 20)),
    );
}

let seriesSubjectName = '';
let seriesType = '';
let seriesPicked = null;

function openSeries(e) {
    seriesSubjectName = e.subject;
    seriesType = e.type || '';
    seriesPicked = e;
    renderSeries();
    if (!seriesModal.open) {
        seriesModal.showModal();
        history.pushState({ series: true }, '');
    }
    seriesModal.scrollTo(0, 0);
}

function closeSeries() {
    if (!seriesModal.open) return;
    if (history.state && history.state.series) history.back();
    else seriesModal.close();
}

window.addEventListener('popstate', () => { if (seriesModal.open) seriesModal.close(); });
seriesBack.addEventListener('click', closeSeries);
seriesModal.addEventListener('cancel', (ev) => { ev.preventDefault(); closeSeries(); });

function renderSeries() {
    const list = seriesByKey[seriesKey({ subject: seriesSubjectName, type: seriesType })] || [];
    const nowMs = warsawNow().getTime();
    const done = list.filter(e => eventRange(e)[1] <= nowMs).length;
    const left = list.length - done;
    const next = list.find(e => eventRange(e)[1] > nowMs);
    const last = list[list.length - 1];
    const sample = list[0] || seriesPicked;

    seriesSubject.textContent = seriesSubjectName;
    seriesSub.textContent = typeLabel(seriesType) + (sample && sample.teacher ? ` · ${sample.teacher}` : '');

    const types = Object.keys(seriesByKey)
        .map(k => k.split('\u0000'))
        .filter(([subj]) => subj === seriesSubjectName)
        .map(([, type]) => type);
    const chips = types.length > 1 ? h('div', { class: 'type-chips' }, types.map(type => h('button', {
        type: 'button', 'aria-pressed': String(type === seriesType),
        onclick: () => { seriesType = type; seriesPicked = null; renderSeries(); seriesModal.scrollTo(0, 0); },
    }, `${typeLabel(type)} · ${seriesByKey[seriesKey({ subject: seriesSubjectName, type })].length}`))) : h('div', { style: 'height:16px' });

    const pct = list.length ? (done / list.length) * 100 : 0;
    const summary = tint(h('div', { class: 'series-summary tint' },
        h('div', { class: 'big' }, h('b', null, left), h('span', null, left === 0 ? 'wszystko za tobą' : `${plural(left, 'zostały', 'zostały', 'zostało')} z ${list.length}`)),
        h('div', { class: 'track' }, h('div', { class: 'fill', style: `--progress:${pct.toFixed(1)}%` })),
        h('div', { class: 'series-facts' },
            h('div', null, h('small', { class: 'sub' }, 'Następne'), h('b', null, next ? `${shortDate(next.date)}, ${next.start}` : '—')),
            h('div', null, h('small', { class: 'sub' }, lastLabel(seriesType)), h('b', null, last ? `${shortDate(last.date)}, ${last.start}` : '—')),
        ),
    ), sample ? sample.color : '');

    const items = [];
    let month = '';
    for (const e of list) {
        const m = e.date.slice(0, 7);
        if (m !== month) {
            month = m;
            const d = parseISO(e.date);
            items.push(h('li', { class: 'series-month' }, `${MONTHS_NOM[d.getMonth()]} ${d.getFullYear()}`));
        }
        const isDone = eventRange(e)[1] <= nowMs;
        const isNext = e === next;
        const cls = ['series-item', isDone && 'done', isNext && 'next', e === seriesPicked && 'picked'].filter(Boolean).join(' ');
        const details = [`${e.start}–${e.end}`, e.room, e.teacher && e.teacher !== sample.teacher ? e.teacher : ''].filter(Boolean).join(' · ');
        items.push(h('li', { class: cls },
            h('span', { class: 'idx' }, e._n, h('small', null, `/${e._total}`)),
            h('span', { class: 'when' }, h('b', null, shortDate(e.date)), h('span', null, details)),
            h('span', { class: 'state' }, isDone ? svg(ICON_CHECK, 16) : isNext ? 'następne' : ''),
        ));
    }

    seriesBody.replaceChildren(chips, summary, h('ul', { class: 'series-list' }, items));
}

function tickLive() {
    const now = warsawNow().getTime();
    document.querySelectorAll('[data-start]').forEach(card => {
        const start = Number(card.dataset.start);
        const end = Number(card.dataset.end);
        const live = settings.live && now >= start && now < end;
        card.classList.toggle('live', live);
        card.classList.toggle('past', settings.dimPast && now >= end);
        if (live) {
            card.style.setProperty('--progress', (((now - start) / (end - start)) * 100).toFixed(2) + '%');
            const rem = card.querySelector('.remaining');
            if (rem) rem.textContent = `zostało ${formatMinutes(Math.ceil((end - now) / 60000))}`;
        }
    });
}

setInterval(tickLive, TICK_MS);
document.addEventListener('visibilitychange', () => { if (!document.hidden) tickLive(); });

function step(days) {
    if (!selectedDate) return;
    selectedDate = addDaysISO(selectedDate, days);
    render();
}

function stepMonth(dir) {
    const d = parseISO(selectedDate);
    const target = new Date(d.getFullYear(), d.getMonth() + dir, 1);
    const prefix = `${target.getFullYear()}-${pad(target.getMonth() + 1)}`;
    const firstWithEvents = Object.keys(eventsByDate).sort().find(x => x.startsWith(prefix));
    selectedDate = firstWithEvents || toISO(target);
    render();
}

function stepByView(dir) {
    if (settings.view === 'month') stepMonth(dir);
    else if (settings.view === 'week') step(7 * dir);
    else step(dir);
}

todayBtn.addEventListener('click', () => { selectedDate = todayISO(); render(); });

document.addEventListener('keydown', (e) => {
    if (e.target.matches('input, select, textarea') || document.querySelector('dialog[open]')) return;
    if (status !== 'ready') return;
    if (e.key === 'ArrowLeft') stepByView(-1);
    if (e.key === 'ArrowRight') stepByView(1);
});

let touchX = null, touchY = null;
view.addEventListener('touchstart', (e) => {
    if (e.touches.length !== 1) { touchX = null; return; }
    touchX = e.touches[0].clientX;
    touchY = e.touches[0].clientY;
}, { passive: true });
view.addEventListener('touchend', (e) => {
    if (touchX === null || status !== 'ready') return;
    const dx = e.changedTouches[0].clientX - touchX;
    const dy = e.changedTouches[0].clientY - touchY;
    touchX = null;
    if (Math.abs(dx) > 60 && Math.abs(dy) < 40) stepByView(dx < 0 ? 1 : -1);
}, { passive: true });

settingsBtn.addEventListener('click', () => settingsModal.showModal());
for (const dlg of [settingsModal, groupModal]) {
    dlg.addEventListener('click', (e) => { if (e.target === dlg || e.target.closest('[data-close]')) dlg.close(); });
}

async function refreshServer() {
    if (!currentPlanId || refreshButton.disabled) return;
    refreshButton.disabled = true;
    refreshButton.classList.add('spinning');
    refreshButton.setAttribute('aria-busy', 'true');
    try {
        await fetchJSON('/api/refresh?plan=' + encodeURIComponent(currentPlanId), { method: 'POST' });
        await loadPlan();
        setNotice('Plan odświeżony z WAT', false);
    } catch (err) {
        setNotice(err.message, true);
    } finally {
        refreshButton.disabled = false;
        refreshButton.classList.remove('spinning');
        refreshButton.removeAttribute('aria-busy');
    }
}
refreshButton.addEventListener('click', refreshServer);

function icsEscape(text) {
    return String(text).replace(/\\/g, '\\\\').replace(/;/g, '\\;').replace(/,/g, '\\,').replace(/\r?\n/g, '\\n');
}
const icsTime = (date, time) => date.replace(/-/g, '') + 'T' + time.replace(':', '') + '00';

function buildICS() {
    const planName = (allPlans.find(p => p.id === currentPlanId) || {}).name || currentPlanId;
    const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d+/, '');
    const lines = [
        'BEGIN:VCALENDAR', 'VERSION:2.0', 'PRODID:-//WATplan//PL', 'CALSCALE:GREGORIAN',
        'X-WR-CALNAME:' + icsEscape('WAT ' + planName), 'X-WR-TIMEZONE:Europe/Warsaw',
        'BEGIN:VTIMEZONE', 'TZID:Europe/Warsaw',
        'BEGIN:STANDARD', 'DTSTART:19701025T030000', 'TZOFFSETFROM:+0200', 'TZOFFSETTO:+0100', 'RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU', 'END:STANDARD',
        'BEGIN:DAYLIGHT', 'DTSTART:19700329T020000', 'TZOFFSETFROM:+0100', 'TZOFFSETTO:+0200', 'RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU', 'END:DAYLIGHT',
        'END:VTIMEZONE',
    ];
    allEvents.forEach((e, i) => {
        lines.push(
            'BEGIN:VEVENT',
            `UID:${currentPlanId}-${e.date}-${e.start.replace(':', '')}-${i}@watplan`,
            `DTSTAMP:${stamp}`,
            `DTSTART;TZID=Europe/Warsaw:${icsTime(e.date, e.start)}`,
            `DTEND;TZID=Europe/Warsaw:${icsTime(e.date, e.end)}`,
            'SUMMARY:' + icsEscape(e.subject + (e.type ? ` (${e.type})` : '')),
        );
        if (e.room) lines.push('LOCATION:' + icsEscape('Sala ' + e.room));
        if (e.teacher) lines.push('DESCRIPTION:' + icsEscape(e.teacher));
        lines.push('END:VEVENT');
    });
    lines.push('END:VCALENDAR');
    return lines.join('\r\n') + '\r\n';
}

downloadIcsBtn.addEventListener('click', () => {
    if (!currentPlanId || allEvents.length === 0) {
        setNotice('Najpierw wybierz grupę z planem', true);
        return;
    }
    const blob = new Blob([buildICS()], { type: 'text/calendar;charset=utf-8' });
    const link = document.createElement('a');
    link.href = URL.createObjectURL(blob);
    link.download = `plan-${currentPlanId}.ics`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(link.href);
    setNotice('Pobrano plan w formacie .ics', false);
});

loadPlans();
