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
const SETTINGS_VERSION = 3;

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
const STALE_RECHECK_MS = 8000;
let staleRecheckFor = '';
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

const defaults = { v: SETTINGS_VERSION, theme: 'auto', view: 'auto', live: true, dimPast: true, calAlarm: false, semester: 'auto' };

function loadSettings() {
    let stored = {};
    try { stored = JSON.parse(localStorage.getItem(SETTINGS_KEY) || '{}') || {}; } catch (_) { stored = {}; }
    const legacyTheme = safeGet('theme');
    const merged = { ...defaults, ...(legacyTheme ? { theme: legacyTheme } : {}), ...stored };
    if (merged.v !== SETTINGS_VERSION) {
        merged.v = SETTINGS_VERSION;
        merged.view = 'auto';
    }
    if (!['auto', 'agenda', 'week', 'month'].includes(merged.view)) merged.view = 'auto';
    return merged;
}

function safeGet(key) { try { return localStorage.getItem(key); } catch (_) { return null; } }
function safeSet(key, value) { try { localStorage.setItem(key, value); } catch (_) { } }

const settings = loadSettings();
const saveSettings = () => safeSet(SETTINGS_KEY, JSON.stringify(settings));

const urlView = { day: 'agenda', dzien: 'agenda', agenda: 'agenda', week: 'week', month: 'month' }[new URLSearchParams(window.location.search).get('view')];
if (urlView) settings.view = urlView;

const wideQuery = window.matchMedia('(min-width: 900px)');
const currentView = () => settings.view === 'auto' ? (wideQuery.matches ? 'week' : 'agenda') : settings.view;

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
            const value = key === 'view' ? currentView() : settings[key];
            btn.setAttribute('aria-pressed', String(btn.dataset.value === value));
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

wideQuery.addEventListener('change', () => {
    if (settings.view !== 'auto') return;
    syncSettingsUI();
    render();
});

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
const ICON_DOWN = '<path d="m6 9 6 6 6-6"/>';
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
const classesWord = (n) => `${n} ${plural(n, 'zajęcie', 'zajęcia', 'zajęć')}`;

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
    const words = subject.replace(/[()[\]{},.;:]/g, ' ').split(/\s+/).filter(Boolean);
    const big = words.filter(w => !SMALL_WORDS.has(w.toLowerCase()) && !/^\d+$/.test(w) && !/^[IVX]+$/.test(w));
    const nums = words.filter(w => /^\d+$/.test(w) || /^[IVX]+$/.test(w));
    let ab;
    if (big.length <= 1) {
        const w = big[0] || subject;
        ab = w === w.toUpperCase() ? w.slice(0, 5) : w.slice(0, 3);
    } else {
        ab = words.filter(w => !/^\d+$/.test(w) && !/^[IVX]+$/.test(w)).map(w => {
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

let planEvents = [];
let planWeeks = [];

const SEMESTER_GAP_DAYS = 21;

function semesterSegments() {
    const segments = [];
    for (const monday of planWeeks) {
        const last = segments[segments.length - 1];
        if (last && (parseISO(monday) - parseISO(last[last.length - 1])) / 86400000 <= SEMESTER_GAP_DAYS) last.push(monday);
        else segments.push([monday]);
    }
    return segments.map(weeks => {
        const first = parseISO(weeks[0]);
        const winter = first.getMonth() >= 7 || first.getMonth() === 0;
        const startYear = first.getMonth() >= 7 ? first.getFullYear() : first.getFullYear() - 1;
        return { weeks, name: winter ? 'Semestr zimowy' : 'Semestr letni', years: `${startYear}/${String(startYear + 1).slice(2)}` };
    });
}

function weekNumber(mondayISO) {
    for (const seg of semesterSegments()) {
        const i = seg.weeks.indexOf(mondayISO);
        if (i >= 0) return `tydzień ${i + 1}/${seg.weeks.length}`;
    }
    if (!planWeeks.length) return 'tydzień bez zajęć';
    if (mondayISO < planWeeks[0]) return 'przed semestrem';
    if (mondayISO > planWeeks[planWeeks.length - 1]) return 'po semestrze';
    return 'tydzień bez zajęć';
}

function weekRange(mondayISO) {
    const monday = parseISO(mondayISO);
    const sunday = addDays(monday, 6);
    return monday.getMonth() === sunday.getMonth()
        ? `${monday.getDate()}–${sunday.getDate()} ${MONTHS_GEN[sunday.getMonth()]}`
        : `${monday.getDate()} ${MONTHS_SHORT[monday.getMonth()]} – ${sunday.getDate()} ${MONTHS_SHORT[sunday.getMonth()]}`;
}

function weekCount(mondayISO) {
    let n = 0;
    for (let i = 0; i < 7; i++) n += (eventsByDate[addDaysISO(mondayISO, i)] || []).length;
    return n;
}

function openWeekPicker() {
    if (!planWeeks.length) return;
    const current = mondayOf(selectedDate);
    const thisWeek = mondayOf(todayISO());
    const segments = semesterSegments();
    const items = [];
    segments.forEach((seg, si) => {
        if (segments.length > 1) items.push(h('li', { class: 'wk-sem', id: `wk-sem-${si}` }, `${seg.name} ${seg.years}`));
        let month = '';
        seg.weeks.forEach((monday, i) => {
            const d = parseISO(monday);
            const m = `${d.getFullYear()}-${d.getMonth()}`;
            if (m !== month) {
                month = m;
                items.push(h('li', { class: 'series-month' }, `${MONTHS_NOM[d.getMonth()]} ${d.getFullYear()}`));
            }
            const n = weekCount(monday);
            const cls = ['wk-pick', monday === current && 'sel', monday === thisWeek && 'now', monday < thisWeek && 'done'].filter(Boolean).join(' ');
            items.push(h('li', null, h('button', {
                type: 'button', class: cls, 'aria-current': monday === current ? 'true' : null,
                onclick: () => { weekModal.close(); selectedDate = monday === thisWeek ? todayISO() : (nextDateWithEvents(addDaysISO(monday, -1), 1) || monday); render(); },
            },
                h('span', { class: 'idx' }, String(i + 1)),
                h('span', { class: 'when' }, h('b', null, weekRange(monday)), h('span', null, n ? classesWord(n) : 'bez zajęć')),
                monday === thisWeek ? h('span', { class: 'tag' }, 'Ten tydzień') : null)));
        });
    });
    const jump = segments.length > 1 ? h('div', { class: 'segmented wk-sem-jump' }, segments.map((seg, si) => h('button', {
        type: 'button', 'aria-pressed': String(seg.weeks.includes(current)),
        onclick: () => weekList.querySelector(`#wk-sem-${si}`)?.scrollIntoView({ block: 'start' }),
    }, seg.name.replace('Semestr ', '').replace(/^./, c => c.toUpperCase())))) : null;
    if (jump) items.unshift(h('li', null, jump));
    weekList.replaceChildren(h('ol', { class: 'wk-pick-list' }, items));
    weekModal.showModal();
    const sel = weekList.querySelector('.sel');
    if (sel) sel.scrollIntoView({ block: 'center' });
}

function loadHidden() {
    try { return JSON.parse(safeGet('hiddenSubjects') || '{}') || {}; } catch (_) { return {}; }
}
const hiddenSubjects = (planId) => new Set(loadHidden()[planId] || []);
function setHiddenSubjects(planId, set) {
    const all = loadHidden();
    if (set.size) all[planId] = [...set].sort(); else delete all[planId];
    safeSet('hiddenSubjects', JSON.stringify(all));
}

function summerStart(year) {
    const d = new Date(year, 1, 22);
    return toISO(addDays(d, -dayIndex(d)));
}

function semesterOf(iso) {
    const [y, m] = iso.split('-').map(Number);
    if (m >= 8) return 'zima';
    return iso < summerStart(y) ? 'zima' : 'lato';
}

const currentSemester = () => settings.semester === 'zima' || settings.semester === 'lato' ? settings.semester : semesterOf(todayISO());

function indexEvents(events) {
    planEvents = events;
    eventsByDate = {};
    seriesByKey = {};
    const hidden = hiddenSubjects(currentPlanId);
    const semester = currentSemester();
    const sorted = events.filter(e => semesterOf(e.date) === semester).sort((a, b) => (a.date + a.start).localeCompare(b.date + b.start));
    for (const e of sorted) (seriesByKey[seriesKey(e)] ||= []).push(e);
    for (const list of Object.values(seriesByKey)) {
        list.forEach((e, i) => { e._n = i + 1; e._total = list.length; });
    }
    planWeeks = [...new Set(sorted.map(e => mondayOf(e.date)))].sort();
    const visible = sorted.filter(e => !hidden.has(e.subject));
    for (const e of visible) (eventsByDate[e.date] ||= []).push(e);
    allEvents = visible;
}

function renderSubjects() {
    const counts = new Map();
    for (const e of planEvents) counts.set(e.subject, (counts.get(e.subject) || 0) + 1);
    const hidden = hiddenSubjects(currentPlanId);
    const names = [...counts.keys()].sort((a, b) => a.localeCompare(b, 'pl'));
    subjectsSection.hidden = names.length === 0;
    $('subjectsSummary').textContent = `Pokaż przedmioty (${names.length})`;
    subjectsHint.textContent = hidden.size
        ? `Ukryte: ${hidden.size}. Nie pokazują się w planie ani w subskrypcji kalendarza.`
        : 'Odznacz przedmioty, na które nie chodzisz, np. lektorat innej grupy.';
    subjectsList.replaceChildren(...names.map(name => {
        const input = h('input', { type: 'checkbox' });
        input.checked = !hidden.has(name);
        input.addEventListener('change', () => {
            const set = hiddenSubjects(currentPlanId);
            if (input.checked) set.delete(name); else set.add(name);
            setHiddenSubjects(currentPlanId, set);
            indexEvents(planEvents);
            renderSubjects();
            syncCalendar();
            render();
        });
        return h('label', { class: 'switch subject' }, input, h('span', null, name), h('small', null, String(counts.get(name))));
    }));
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

function acceptPlan(planId, text, data, cached) {
    storePlan(planId, text);
    scheduleStaleRecheck(planId, data.fetched_at);
    loadChangeChip(planId);
    if (cached && cached.raw === text) return false;
    applyPlanData(data);
    return true;
}

async function loadPlan(quiet = false) {
    if (!currentPlanId) return null;
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
        if (requestId !== loadCounter) return null;
        if (response.headers.get('X-Plan-Refresh') === 'background') watchVisit(planId, data);
        if (!acceptPlan(planId, text, data, cached)) return data;
        render();
        return data;
    } catch (err) {
        if (requestId !== loadCounter) return null;
        if (cached) {
            if (!quiet) setNotice('Nie udało się pobrać planu, pokazuję zapisaną wersję', true);
            return null;
        }
        indexEvents([]);
        updateStaleBanner('');
        fetchedLabel.textContent = '-';
        modifiedLabel.textContent = '-';
        status = 'error';
        statusMessage = err.message;
    }
    render();
    return null;
}

function parseFetched(fetchedAt) {
    const m = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})/.exec(fetchedAt || '');
    if (!m) return null;
    return { m, date: new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]) };
}

function scheduleStaleRecheck(planId, fetchedAt) {
    const parsed = parseFetched(fetchedAt);
    if (!parsed || staleRecheckFor === planId || warsawNow() - parsed.date <= STALE_AFTER_MS) return;
    staleRecheckFor = planId;
    setTimeout(() => { if (currentPlanId === planId) loadPlan(); }, STALE_RECHECK_MS);
}

function updateStaleBanner(fetchedAt) {
    const parsed = parseFetched(fetchedAt);
    if (!parsed) { staleBanner.classList.add('hidden'); return; }
    const { m, date: fetched } = parsed;
    const stale = warsawNow() - fetched > STALE_AFTER_MS;
    staleBanner.classList.toggle('hidden', !stale);
    if (stale) {
        staleBanner.textContent = `Plan pobrany ${fetched.getDate()} ${MONTHS_GEN[fetched.getMonth()]} o ${m[4]}:${m[5]} – może być nieaktualny. W razie wątpliwości sprawdź oficjalny plan WAT.`;
    }
}

function render() {
    app.classList.toggle('wide', currentView() === 'week' && status === 'ready');
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
    if (allEvents.length === 0) {
        const other = currentSemester() === 'lato' ? 'zimowy' : 'letni';
        view.replaceChildren(h('div', { class: 'empty-day' },
            h('b', null, currentSemester() === 'lato' ? 'Brak planu na semestr letni' : 'Brak planu na semestr zimowy'),
            planEvents.length ? `Wydział nie opublikował jeszcze tego planu. WATplan doda go sam, gdy pojawi się na stronie WAT.` : 'Brak zajęć w planie.',
            planEvents.length ? h('br') : null,
            planEvents.length ? h('a', { class: 'link-btn', href: '/?landing' }, `Wybierz semestr ${other} na stronie głównej`) : null));
        return;
    }
    if (!selectedDate) selectedDate = pickInitialDate();

    if (currentView() === 'week') renderWeek();
    else if (currentView() === 'month') renderMonth();
    else renderAgenda();
    placeChangeChip();
    tickLive();
}

function todayOffScreen() {
    const today = todayISO();
    if (currentView() === 'week') return mondayOf(selectedDate) !== mondayOf(today);
    return selectedDate !== today;
}

function renderAgenda() {
    const d = parseISO(selectedDate);
    const count = (eventsByDate[selectedDate] || []).length;
    view.replaceChildren(
        h('div', { class: 'day-title' }, h('h1', null, DAY_NAMES[dayIndex(d)]), h('p', null,
            `${longDate(selectedDate)} · `,
            h('button', { type: 'button', class: 'week-link', onclick: openWeekPicker, 'aria-haspopup': 'dialog' }, weekNumber(mondayOf(selectedDate))),
            count ? ` · ${classesWord(count)}` : '')),
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

    const nav = navRow(weekRange(mondayISO), `${weekNumber(mondayISO)} · ${classesWord(events.length)}`, -7, 7, openWeekPicker);

    if (events.length === 0) {
        view.replaceChildren(nav, emptyWeek(mondayISO));
        return;
    }

    let minSlot = SLOTS.length - 1, maxSlot = 0;
    for (const e of events) { minSlot = Math.min(minSlot, slotStart(e.start)); maxSlot = Math.max(maxSlot, slotEnd(e.end)); }

    const grid = h('div', { class: 'week-grid', style: `--days:${days}` });
    const today = todayISO();
    const lastRow = maxSlot - minSlot + 2;
    for (let s = minSlot; s <= maxSlot; s++) {
        grid.append(h('div', { class: 'wk-line', style: `grid-row:${s - minSlot + 2};grid-column:2 / ${days + 2}` }));
    }
    const todayCol = dates.slice(0, days).indexOf(today);
    if (todayCol >= 0) grid.append(h('div', { class: 'wk-today', style: `grid-row:1 / ${lastRow + 1};grid-column:${todayCol + 2}` }));
    for (let i = 0; i < days; i++) {
        const iso = dates[i];
        const n = (eventsByDate[iso] || []).length;
        const date = parseISO(iso);
        grid.append(h('div', { class: 'wk-head' + (iso === today ? ' today' : ''), style: `grid-row:1;grid-column:${i + 2}` },
            h('b', { class: 'short' }, DAY_SHORT[i]),
            h('b', { class: 'long' }, DAY_NAMES[i]),
            h('span', { class: 'dd' }, h('span', { class: 'num' }, date.getDate()), h('span', { class: 'long' }, ` ${MONTHS_SHORT[date.getMonth()]}`)),
            h('small', { class: 'long' }, n ? classesWord(n) : 'wolne')));
    }
    for (let s = minSlot; s <= maxSlot; s++) {
        const [st, en] = SLOTS[s];
        const [hh, mm] = st.split(':');
        grid.append(h('div', { class: 'wk-time', style: `grid-row:${s - minSlot + 2};grid-column:1` },
            h('span', { class: 'short' }, hh), h('span', { class: 'short' }, mm),
            h('b', { class: 'long' }, st), h('span', { class: 'long' }, en)));
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
    const { main: room, more } = splitRooms(e.room);
    const cell = h('button', {
        type: 'button', class: 'wk-cell tint', 'data-start': start, 'data-end': end,
        title: `${e.start}–${e.end} ${e.subject}${e.room ? ', sala ' + e.room : ''}${e.teacher ? ', ' + e.teacher : ''}`,
        'aria-label': `${e.subject}, ${typeLabel(e.type)}, ${e.start}–${e.end}${e.room ? ', sala ' + e.room : ''}. Pokaż wszystkie`,
        onclick: () => openSeries(e),
    },
        h('span', { class: 'time sub' }, `${e.start}–${e.end}`),
        h('span', { class: 'ab' }, abbreviate(e.subject)),
        h('span', { class: 'full' }, e.subject),
        h('span', { class: 'meta sub' },
            h('span', { class: 'kind' }, h('span', { class: 'dot' }), `${typeShort(e.type)} ${e._n}/${e._total}`),
            room ? h('span', { class: 'room' }, svg(ICON_BUILDING, 13), room, more ? ` +${more}` : null) : null,
            e.teacher ? h('span', { class: 'teacher' }, svg(ICON_USER, 13), e.teacher) : null),
        h('span', { class: 'wk-live' }, h('span', { class: 'fill' })),
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

function navRow(title, subtitle, prev, next, onLabel) {
    const go = (dir) => () => (typeof dir === 'number' ? step(dir) : stepMonth(dir === 'nextMonth' ? 1 : -1));
    const label = onLabel
        ? h('button', { type: 'button', class: 'nav-label pick', onclick: onLabel, 'aria-haspopup': 'dialog', 'aria-label': `${title}, ${subtitle}. Wybierz tydzień` },
            h('b', null, title, svg(ICON_DOWN, 16)), h('small', null, subtitle))
        : h('div', { class: 'nav-label' }, h('b', null, title), h('small', null, subtitle));
    return h('div', { class: 'nav-row' },
        h('button', { type: 'button', class: 'round-btn', 'aria-label': 'Wstecz', onclick: go(prev) }, svg(ICON_LEFT, 20)),
        label,
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

    seriesBody.replaceChildren(chips, summary, makeupSection(list, next), h('ul', { class: 'series-list' }, items));
}

function overlapsMine(m) {
    const a = toMin(m.start), b = toMin(m.end);
    return (eventsByDate[m.date] || []).find(e => toMin(e.start) < b && toMin(e.end) > a);
}

function makeupSection(list, next) {
    if (!list.length) return null;
    const picked = seriesPicked && list.includes(seriesPicked) ? seriesPicked : (next || list[list.length - 1]);
    const select = h('select', { 'aria-label': 'Które zajęcia chcesz odrobić' },
        ...list.map(e => {
            const opt = h('option', { value: String(e._n) }, `nr ${e._n} · ${shortDate(e.date)}`);
            if (e === picked) opt.selected = true;
            return opt;
        }));
    const results = h('div', { class: 'makeup-results' });
    const load = async () => {
        const n = select.value;
        results.replaceChildren(h('p', { class: 'hint' }, 'Szukam w innych grupach...'));
        try {
            const params = new URLSearchParams({ plan: currentPlanId, subject: seriesSubjectName, type: seriesType, n });
            const data = await fetchJSON('/api/makeup?' + params);
            if (select.value !== n) return;
            const matches = data.matches || [];
            if (!matches.length) {
                results.replaceChildren(h('p', { class: 'hint' }, data.cohort
                    ? `Żadna inna grupa ${data.cohort} nie ma zajęć nr ${n} z tego przedmiotu.`
                    : 'Dla tej grupy nie da się ustalić rocznika.'));
                return;
            }
            const today = todayISO();
            const slots = new Map();
            for (const m of matches) {
                const mine = overlapsMine(m);
                if (mine && mine.subject === seriesSubjectName && (mine.type || '') === seriesType) continue;
                const key = `${m.date}|${m.start}|${m.room || ''}`;
                if (!slots.has(key)) slots.set(key, { ...m, groups: [], clash: mine });
                slots.get(key).groups.push(m.group);
            }
            const all = [...slots.values()];
            const upcoming = all.filter(m => m.date >= today);
            const past = all.filter(m => m.date < today);
            const row = (m) => h('li', { class: 'makeup-item' + (m.date < today ? ' done' : '') },
                h('span', { class: 'when' }, h('b', null, `${shortDate(m.date)}, ${m.start}–${m.end}`),
                    h('span', null, [m.room && `sala ${m.room}`, m.groups.length > 2 ? `${m.groups.length} grup: ${m.groups.slice(0, 2).join(', ')}…` : m.groups.join(', ')].filter(Boolean).join(' · '))),
                m.clash ? h('span', { class: 'makeup-tag clash', title: `${m.clash.subject} ${m.clash.start}–${m.clash.end}` }, 'koliduje')
                    : m.date < today ? h('span', { class: 'makeup-tag' }, 'minęło')
                        : h('span', { class: 'makeup-tag ok' }, 'pasuje'));
            if (!all.length) {
                results.replaceChildren(h('p', { class: 'hint' }, `Inne grupy ${data.cohort} mają zajęcia nr ${n} w tym samym terminie co Ty, np. wspólny wykład.`));
                return;
            }
            const list = h('ul', { class: 'makeup-list' }, upcoming.map(row));
            const morePast = past.length ? h('button', { type: 'button', class: 'link-btn', onclick: (e) => { list.prepend(...past.map(row)); e.currentTarget.remove(); } },
                `Pokaż minione (${past.length})`) : null;
            results.replaceChildren(
                h('p', { class: 'hint' }, upcoming.length ? `Zajęcia nr ${n} w innych grupach ${data.cohort}:` : `Wszystkie terminy zajęć nr ${n} w innych grupach ${data.cohort} już minęły.`),
                list, morePast);
        } catch (err) {
            results.replaceChildren(h('p', { class: 'hint error' }, err.message));
        }
    };
    select.addEventListener('change', load);
    load();
    return h('section', { class: 'makeup' },
        h('header', null, h('b', null, 'Gdzie odrobić?'), select),
        results);
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
    if (currentView() === 'month') stepMonth(dir);
    else if (currentView() === 'week') step(7 * dir);
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
const weekModal = $('weekModal');
const weekList = $('weekList');
for (const dlg of [settingsModal, groupModal, weekModal]) {
    dlg.addEventListener('click', (e) => { if (e.target === dlg || e.target.closest('[data-close]')) dlg.close(); });
}

const REFRESH_CHECKS_MS = [2000, 5000, 10000, 20000];

const sleep = (ms) => new Promise(resolve => setTimeout(resolve, ms));
const eventsKey = (data) => JSON.stringify((data && data.events) || []);
const checkedKey = (data) => (data && (data.checked_at || data.fetched_at)) || '';

function checkedAgo(iso) {
    const minutes = Math.floor((Date.now() - Date.parse(iso)) / 60000);
    if (!Number.isFinite(minutes) || minutes < 1) return 'przed chwilą';
    return `${minutes} min temu`;
}

async function postRefresh(planId) {
    const response = await fetch('/api/refresh?plan=' + encodeURIComponent(planId), { method: 'POST' });
    const text = await response.text();
    let data = null;
    try { data = JSON.parse(text); } catch (_) { data = null; }
    if (!response.ok) throw new Error((data && data.error) || `Błąd serwera (${response.status})`);
    const state = response.headers.get('X-Plan-Refresh') || '';
    const checked = response.headers.get('X-Plan-Checked-At') || '';
    if (!data || !Array.isArray(data.events)) return { plan: null, state, checked };
    if (currentPlanId === planId && acceptPlan(planId, text, data, cachedPlan(planId))) render();
    return { plan: data, state, checked };
}

let activeWatch = null;

async function watchRefresh(planId, baseline, quiet = false) {
    const watch = { planId };
    activeWatch = watch;
    try {
        let waited = 0;
        for (const at of REFRESH_CHECKS_MS) {
            await sleep(at - waited);
            waited = at;
            if (activeWatch !== watch || currentPlanId !== planId) return 'left';
            const data = await loadPlan(quiet);
            if (activeWatch !== watch || currentPlanId !== planId) return 'left';
            if (!data) continue;
            if (!baseline || checkedKey(data) !== checkedKey(baseline)) {
                return baseline && eventsKey(data) === eventsKey(baseline) ? 'same' : 'changed';
            }
        }
        return 'timeout';
    } finally {
        if (activeWatch === watch) activeWatch = null;
    }
}

async function watchVisit(planId, baseline) {
    if (activeWatch && activeWatch.planId === planId) return;
    if (await watchRefresh(planId, baseline, true) === 'changed') setNotice('Plan zaktualizowany', false);
}

async function refreshServer() {
    if (!currentPlanId || refreshButton.disabled) return;
    const planId = currentPlanId;
    refreshButton.disabled = true;
    refreshButton.classList.add('spinning');
    refreshButton.setAttribute('aria-busy', 'true');
    try {
        const { plan, state, checked } = await postRefresh(planId);
        if (state === 'fresh') {
            setNotice(`Plan aktualny (sprawdzony ${checkedAgo(checked)})`, false);
            return;
        }
        if (state === 'cooldown') {
            setNotice('Plan był odświeżany przed chwilą', false);
            return;
        }
        setNotice(state === 'pending' ? 'Sprawdzam plan...' : 'Odświeżanie w tle...', false);
        const outcome = await watchRefresh(planId, plan);
        if (outcome === 'same') setNotice('Plan aktualny', false);
        else if (outcome === 'changed') setNotice('Plan zaktualizowany', false);
        else if (outcome === 'timeout') setNotice('Odświeżanie trwa dłużej niż zwykle, spróbuj ponownie później', true);
    } catch (err) {
        setNotice(err.message, true);
    } finally {
        refreshButton.disabled = false;
        refreshButton.classList.remove('spinning');
        refreshButton.removeAttribute('aria-busy');
    }
}
refreshButton.addEventListener('click', refreshServer);

const calApple = $('calApple');
const calGoogle = $('calGoogle');
const calCopy = $('calCopy');
const calAlarm = $('calAlarm');

function calendarURL() {
    const params = new URLSearchParams();
    if (settings.calAlarm) params.set('alarm', '15');
    for (const name of hiddenSubjects(currentPlanId)) params.append('hide', name);
    const query = params.toString();
    return window.location.origin + `/cal/${encodeURIComponent(currentPlanId)}.ics` + (query ? '?' + query : '');
}

function syncCalendar() {
    calAlarm.checked = settings.calAlarm;
    const ready = !!currentPlanId;
    for (const el of [calApple, calGoogle, calCopy, downloadIcsBtn]) el.toggleAttribute('aria-disabled', !ready);
    if (!ready) return;
    const webcal = calendarURL().replace(/^https?:/, 'webcal:');
    calApple.href = webcal;
    calGoogle.href = 'https://calendar.google.com/calendar/render?cid=' + encodeURIComponent(webcal);
}

function needPlan(e) {
    if (currentPlanId) return false;
    if (e) e.preventDefault();
    setNotice('Najpierw wybierz grupę z planem', true);
    return true;
}

calApple.addEventListener('click', needPlan);
calGoogle.addEventListener('click', needPlan);
calAlarm.addEventListener('change', () => { settings.calAlarm = calAlarm.checked; saveSettings(); syncCalendar(); });

calCopy.addEventListener('click', async () => {
    if (needPlan()) return;
    const link = calendarURL();
    try {
        await navigator.clipboard.writeText(link);
        setNotice('Skopiowano link do kalendarza', false);
    } catch (_) {
        window.prompt('Skopiuj link do kalendarza:', link);
    }
});

downloadIcsBtn.addEventListener('click', () => {
    if (needPlan()) return;
    const url = calendarURL();
    window.location.href = url + (url.includes('?') ? '&' : '?') + 'download=1';
});

const subjectsSection = $('subjectsSection');
const subjectsList = $('subjectsList');
const subjectsHint = $('subjectsHint');

settingsBtn.addEventListener('click', () => { syncCalendar(); renderSubjects(); });

const changeChip = $('changeChip');
const changesModal = $('changesModal');
const changesPlan = $('changesPlan');
const changesBody = $('changesBody');
const changesSub = $('changesSub');
const openChangesBtn = $('openChanges');
const pushRow = $('pushRow');
const pushToggle = $('pushToggle');
const pushHint = $('pushHint');
const CHIP_DAYS = 7;

async function fetchChanges(planId) {
    return fetchJSON('/api/changes/' + encodeURIComponent(planId));
}

function parseStamp(text) {
    const m = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})/.exec(text || '');
    return m ? new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]) : null;
}

function agoText(date) {
    const min = Math.max(0, Math.round((warsawNow() - date) / 60000));
    if (min < 60) return min <= 1 ? 'przed chwilą' : `${min} min temu`;
    const hrs = Math.round(min / 60);
    if (hrs < 24) return `${hrs} godz. temu`;
    const days = Math.round(hrs / 24);
    return days === 1 ? 'wczoraj' : `${days} dni temu`;
}

function stampText(date) {
    return `${date.getDate()} ${MONTHS_GEN[date.getMonth()]}, ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function staleSince(c) {
    const at = parseStamp(c.at);
    const since = parseStamp(c.since);
    return at && since && at - since > 36 * 3600000 ? since : null;
}

const dayMonth = (d) => `${d.getDate()} ${MONTHS_GEN[d.getMonth()]}`;

const changeCount = (c) => (c.added || []).length + (c.removed || []).length + (c.moved || []).length + (c.changed || []).length;

function placeChangeChip() {
    const anchor = view.querySelector('.day-title, .nav-row');
    if (anchor) anchor.after(changeChip);
}

async function loadChangeChip(planId) {
    try {
        const data = await fetchChanges(planId);
        if (planId !== currentPlanId) return;
        const last = (data.changes || [])[0];
        const at = last && parseStamp(last.at);
        if (!at || warsawNow() - at > CHIP_DAYS * 86400000) { changeChip.classList.add('hidden'); return; }
        const n = changeCount(last);
        const since = staleSince(last);
        changeChip.replaceChildren(h('span', { class: 'pulse' }),
            `${n} ${plural(n, 'zmiana', 'zmiany', 'zmian')} w planie · ${since ? 'od ' + dayMonth(since) : agoText(at)}`);
        changeChip.classList.remove('hidden');
        placeChangeChip();
    } catch (_) {
        changeChip.classList.add('hidden');
    }
}

function eventWhen(e) {
    return `${shortDate(e.date)}, ${e.start}–${e.end}`;
}

function eventName(e) {
    return e.type ? `${e.subject} · ${typeLabel(e.type)}` : e.subject;
}

function jumpTo(date) {
    changesModal.close();
    if (!date) return;
    selectedDate = date;
    render();
}

function changeItem(kind, label, title, lines, date) {
    return h('li', null, h('button', { type: 'button', class: 'chg-item ' + kind, onclick: () => jumpTo(date) },
        h('span', { class: 'chg-kind' }, label),
        h('b', null, title),
        lines.map(line => h('span', { class: 'chg-line' }, line))));
}

function detailParts(before, after) {
    const parts = [];
    if (before.room !== after.room) parts.push(h('span', null, 'sala ', h('s', null, before.room || 'brak'), ' → ', h('b', null, after.room || 'brak')));
    if (before.end !== after.end) parts.push(h('span', null, 'koniec ', h('s', null, before.end), ' → ', h('b', null, after.end)));
    if (before.teacher !== after.teacher) parts.push(h('span', null, 'prowadzący ', h('s', null, before.teacher || 'brak'), ' → ', h('b', null, after.teacher || 'brak')));
    return parts.length ? parts : [h('span', null, 'zmienione szczegóły')];
}

function renderChangeEntry(c) {
    const at = parseStamp(c.at);
    const counts = [
        [(c.moved || []).length, 'przeniesione'], [(c.changed || []).length, 'zmienione'],
        [(c.removed || []).length, 'odwołane'], [(c.added || []).length, 'nowe'],
    ].filter(([n]) => n > 0);
    const items = [];
    for (const m of c.moved || []) items.push(changeItem('moved', 'Przeniesione', eventName(m.after),
        [h('span', null, h('s', null, eventWhen(m.before))), h('span', null, '→ ', h('b', null, eventWhen(m.after)))], m.after.date));
    for (const m of c.changed || []) items.push(changeItem('changed', 'Zmiana', eventName(m.after),
        [eventWhen(m.after), ...detailParts(m.before, m.after)], m.after.date));
    for (const e of c.removed || []) items.push(changeItem('removed', 'Odwołane', eventName(e),
        [h('s', null, eventWhen(e)), e.room ? `sala ${e.room}` : null].filter(Boolean), e.date));
    for (const e of c.added || []) items.push(changeItem('added', 'Nowe', eventName(e),
        [eventWhen(e), e.room ? `sala ${e.room}` : null].filter(Boolean), e.date));
    const since = staleSince(c);
    return h('section', { class: 'chg-entry' },
        h('header', null,
            h('b', null, since ? `Zmiany od ${dayMonth(since)}` : (at ? stampText(at) : c.at)),
            h('span', null, at ? agoText(at) : '')),
        since ? h('p', { class: 'chg-note' }, `Wykryte ${at ? stampText(at) : c.at} przy pierwszym sprawdzeniu po przerwie. Dokładny moment zmiany w tym okresie nie jest znany.`) : null,
        h('div', { class: 'chg-counts' }, counts.map(([n, label]) => h('span', { class: 'chg-count' }, `${n} ${label}`))),
        h('ol', { class: 'chg-list' }, items));
}

function changesPlans() {
    const ids = [];
    const add = (id, name) => { if (id && !ids.some(x => x.id === id)) ids.push({ id, name: name || id }); };
    add(currentPlanId, (allPlans.find(p => p.id === currentPlanId) || {}).name);
    for (const f of WatGroups.favorites()) add(f.id, f.name);
    for (const id of pushPlans()) add(id);
    return ids;
}

async function renderChanges(planId) {
    changesPlan.replaceChildren(...changesPlans().map(p => {
        const opt = h('option', { value: p.id }, p.name);
        if (p.id === planId) opt.selected = true;
        return opt;
    }));
    changesSub.textContent = '';
    changesBody.replaceChildren(h('div', { class: 'message' }, 'Ładowanie...'));
    try {
        const data = await fetchChanges(planId);
        if (changesPlan.value !== planId) return;
        const list = data.changes || [];
        const checked = parseStamp(data.fetched_at);
        changesSub.textContent = checked ? `Sprawdzono ${agoText(checked)}` : '';
        const total = list.reduce((n, c) => n + changeCount(c), 0);
        const summary = h('div', { class: 'chg-summary' },
            h('div', null, h('b', null, String(list.length)), h('span', null, plural(list.length, 'aktualizacja', 'aktualizacje', 'aktualizacji'))),
            h('div', null, h('b', null, String(total)), h('span', null, plural(total, 'zmiana', 'zmiany', 'zmian'))),
            h('div', null, h('b', null, list[0] ? (staleSince(list[0]) ? 'od ' + dayMonth(staleSince(list[0])) : agoText(parseStamp(list[0].at))) : '—'), h('span', null, 'ostatnia zmiana')));
        if (list.length === 0) {
            changesBody.replaceChildren(summary, h('div', { class: 'empty-day' },
                h('b', null, 'Brak zapisanych zmian'),
                'WATplan zapisuje zmiany, gdy zauważy je przy odświeżaniu planu. Włącz powiadomienia, żeby dowiedzieć się o nich od razu.'));
            return;
        }
        changesBody.replaceChildren(summary, ...list.map(renderChangeEntry));
    } catch (err) {
        changesBody.replaceChildren(h('div', { class: 'message error' }, err.message));
    }
}

function openChanges(planId) {
    if (!planId) { setNotice('Najpierw wybierz grupę z planem', true); return; }
    if (settingsModal.open) settingsModal.close();
    renderChanges(planId);
    if (!changesModal.open) changesModal.showModal();
    changesModal.scrollTo(0, 0);
}

changesPlan.addEventListener('change', () => renderChanges(changesPlan.value));
changeChip.addEventListener('click', () => openChanges(currentPlanId));
openChangesBtn.addEventListener('click', () => openChanges(currentPlanId));
changesModal.addEventListener('click', (e) => { if (e.target.closest('[data-close]')) changesModal.close(); });

const pushSupported = 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;
const isIOS = /iP(hone|ad|od)/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
let pushKey = null;

function pushPlans() {
    try { const list = JSON.parse(safeGet('pushPlans') || '[]'); return Array.isArray(list) ? list.filter(x => typeof x === 'string') : []; } catch (_) { return []; }
}

async function loadPushKey() {
    if (pushKey !== null) return pushKey;
    try {
        const res = await fetch('/api/push/key');
        pushKey = res.ok ? ((await res.json()).key || '') : '';
    } catch (_) { pushKey = ''; }
    return pushKey;
}

function keyBytes(base64) {
    const padded = (base64 + '='.repeat((4 - base64.length % 4) % 4)).replace(/-/g, '+').replace(/_/g, '/');
    return Uint8Array.from(atob(padded), c => c.charCodeAt(0));
}

async function syncPush(plans) {
    const reg = await navigator.serviceWorker.ready;
    let sub = await reg.pushManager.getSubscription();
    if (plans.length === 0) {
        if (sub) {
            await fetch('/api/push/unsubscribe', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ endpoint: sub.endpoint }) }).catch(() => undefined);
            await sub.unsubscribe().catch(() => undefined);
        }
        return;
    }
    if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(await loadPushKey()) });
    await fetchJSON('/api/push/subscribe', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ subscription: sub.toJSON(), plans }),
    });
}

async function syncPushUI() {
    const key = await loadPushKey();
    if (!key) { pushRow.classList.add('hidden'); return; }
    pushRow.classList.remove('hidden');
    const on = Boolean(currentPlanId) && pushPlans().includes(currentPlanId) && pushSupported && Notification.permission === 'granted';
    pushToggle.checked = on;
    pushToggle.disabled = !currentPlanId || !pushSupported;
    if (!pushSupported) {
        pushHint.textContent = isIOS
            ? 'Na iPhonie powiadomienia działają po dodaniu WATplan do ekranu początkowego: Udostępnij → Do ekranu początkowego. Potem włącz je tutaj.'
            : 'Ta przeglądarka nie obsługuje powiadomień.';
    } else if (Notification.permission === 'denied') {
        pushHint.textContent = 'Powiadomienia są zablokowane w ustawieniach przeglądarki dla watplan.me.';
    } else {
        pushHint.textContent = 'Powiadomienie przyjdzie, gdy WAT przeniesie, odwoła lub doda zajęcia albo zmieni salę. Wszystkie zmiany znajdziesz w historii.';
    }
}

pushToggle.addEventListener('change', async () => {
    const plans = new Set(pushPlans());
    const enable = pushToggle.checked;
    pushToggle.disabled = true;
    try {
        if (enable) {
            const permission = await Notification.requestPermission();
            if (permission !== 'granted') throw new Error('Bez zgody przeglądarki nie wyślemy powiadomień');
            plans.add(currentPlanId);
        } else {
            plans.delete(currentPlanId);
        }
        const list = [...plans].slice(0, 10);
        await syncPush(list);
        safeSet('pushPlans', JSON.stringify(list));
        setNotice(enable ? `Powiadomienia o zmianach w ${currentPlanId} włączone` : 'Powiadomienia wyłączone', false);
    } catch (err) {
        pushToggle.checked = !enable;
        setNotice(err.message || 'Nie udało się zmienić powiadomień', true);
    } finally {
        syncPushUI();
    }
});

settingsBtn.addEventListener('click', syncPushUI);

if (pushSupported && pushPlans().length && Notification.permission === 'granted') {
    loadPushKey().then(key => { if (key) syncPush(pushPlans()).catch(() => undefined); });
}

const toolsBtn = $('toolsBtn');
const toolsModal = $('toolsModal');
const roomsModal = $('roomsModal');
const roomsDate = $('roomsDate');
const roomsSlot = $('roomsSlot');
const roomsBuilding = $('roomsBuilding');
const roomsBody = $('roomsBody');
const roomsSub = $('roomsSub');
const compareModal = $('compareModal');
const compareGroupsEl = $('compareGroups');
const compareSearch = $('compareSearch');
const compareResults = $('compareResults');
const compareBody = $('compareBody');
const printArea = $('printArea');
const ROOM_RE = /^(.+?)\/?\s+(\d{1,3}[A-Z]?|[A-Z])$/;

toolsModal.addEventListener('click', (e) => { if (e.target === toolsModal || e.target.closest('[data-close]')) toolsModal.close(); });
const examsModal = $('examsModal');
const examsBody = $('examsBody');
const examsSub = $('examsSub');

for (const dlg of [roomsModal, compareModal, examsModal]) {
    dlg.addEventListener('click', (e) => { if (e.target.closest('[data-close]')) dlg.close(); });
}
toolsBtn.addEventListener('click', () => toolsModal.showModal());
toolsModal.querySelectorAll('[data-tool]').forEach(btn => btn.addEventListener('click', () => {
    toolsModal.close();
    const tool = btn.dataset.tool;
    if (tool === 'exams') openExams();
    else if (tool === 'rooms') openRooms();
    else if (tool === 'compare') openCompare();
    else if (tool === 'changes') openChanges(currentPlanId);
    else if (tool === 'print') printMonth();
}));

const SESSION_RE = /^\s*sesja/i;
const isSessionMarker = (e) => SESSION_RE.test(e.subject);
const isExam = (e) => !isSessionMarker(e) && (/egzamin|zaliczenie/i.test(e.type || '') || /egzamin|\begz\.|\(e\)\s*$|_e\s*$/i.test(e.subject));

function daysUntil(iso) {
    return Math.round((parseISO(iso) - parseISO(todayISO())) / 86400000);
}

function countdown(iso) {
    const n = daysUntil(iso);
    if (n === 0) return 'dziś';
    if (n === 1) return 'jutro';
    if (n < 0) return n === -1 ? 'wczoraj' : `${-n} dni temu`;
    return `za ${n} ${plural(n, 'dzień', 'dni', 'dni')}`;
}

function dateRangeText(from, to) {
    const a = parseISO(from), b = parseISO(to);
    if (from === to) return `${a.getDate()} ${MONTHS_GEN[a.getMonth()]}`;
    if (a.getMonth() === b.getMonth()) return `${a.getDate()}–${b.getDate()} ${MONTHS_GEN[b.getMonth()]}`;
    return `${a.getDate()} ${MONTHS_SHORT[a.getMonth()]} – ${b.getDate()} ${MONTHS_SHORT[b.getMonth()]}`;
}

function sessionPeriods() {
    const byName = new Map();
    for (const e of allEvents.filter(isSessionMarker)) {
        const name = e.subject.trim().replace(/^./, c => c.toUpperCase());
        const p = byName.get(name) || { name, from: e.date, to: e.date };
        if (e.date < p.from) p.from = e.date;
        if (e.date > p.to) p.to = e.date;
        byName.set(name, p);
    }
    return [...byName.values()].sort((a, b) => a.from.localeCompare(b.from));
}

function renderExams() {
    const today = todayISO();
    const exams = allEvents.filter(isExam);
    const upcoming = exams.filter(e => e.date >= today);
    const past = exams.filter(e => e.date < today);
    const periods = sessionPeriods();
    const sections = [];

    examsSub.textContent = upcoming.length
        ? `Najbliższy egzamin ${countdown(upcoming[0].date)}`
        : (periods.length ? `Sesja: ${dateRangeText(periods[0].from, periods[periods.length - 1].to)}` : 'Na podstawie planu Twojej grupy');

    if (periods.length) {
        sections.push(h('section', { class: 'ex-periods' }, periods.map(p => h('div', { class: 'ex-period' + (p.to < today ? ' done' : '') },
            h('b', null, p.name), h('span', null, dateRangeText(p.from, p.to)),
            h('small', null, p.to < today ? 'zakończona' : p.from <= today ? 'trwa' : countdown(p.from))))));
    }

    const examRow = (e) => h('li', { class: 'ex-item' + (e.date < today ? ' done' : '') },
        h('div', { class: 'ex-date' }, h('b', null, String(parseISO(e.date).getDate())), h('span', null, MONTHS_SHORT[parseISO(e.date).getMonth()])),
        h('div', { class: 'ex-main' },
            h('b', null, e.subject),
            h('span', null, [`${shortDate(e.date)}, ${e.start}–${e.end}`, splitRooms(e.room).main && `sala ${splitRooms(e.room).main}`].filter(Boolean).join(' · '))),
        h('span', { class: 'ex-left' + (daysUntil(e.date) <= 3 && e.date >= today ? ' soon' : '') }, countdown(e.date)));

    sections.push(h('h3', { class: 'ex-h' }, 'Egzaminy'));
    if (exams.length) {
        sections.push(h('ol', { class: 'ex-list' }, upcoming.map(examRow), past.map(examRow)));
    } else {
        sections.push(h('p', { class: 'hint' }, 'W planie tej grupy nie ma jeszcze egzaminów. Wydziały często dopisują je bliżej sesji, a WATplan pokaże je tutaj od razu po pojawieniu się w planie.'));
    }

    const series = new Map();
    for (const e of allEvents) {
        if (isExam(e) || isSessionMarker(e)) continue;
        const key = seriesKey(e);
        const s = series.get(key) || { e, last: e, left: 0 };
        if (e.date > s.last.date || (e.date === s.last.date && e.start > s.last.start)) s.last = e;
        if (eventRange(e)[1] > warsawNow().getTime()) s.left++;
        series.set(key, s);
    }
    const lasts = [...series.values()].sort((a, b) => (a.last.date + a.last.start).localeCompare(b.last.date + b.last.start));
    if (lasts.length) {
        sections.push(h('h3', { class: 'ex-h' }, 'Ostatnie zajęcia z przedmiotów'),
            h('p', { class: 'hint' }, 'Zwykle wtedy jest zaliczenie albo ostatnia szansa na odrobienie.'),
            h('ol', { class: 'ex-lasts' }, lasts.map(s => h('li', { class: s.left === 0 ? 'done' : '' },
                h('span', { class: 'ex-main' }, h('b', null, s.e.subject), h('span', null, typeLabel(s.e.type))),
                h('span', { class: 'ex-when' }, h('b', null, shortDate(s.last.date)), h('span', null, s.left === 0 ? 'zakończone' : `${plural(s.left, 'zostały', 'zostały', 'zostało')} ${s.left}`))))));
    }
    examsBody.replaceChildren(...sections);
}

function openExams() {
    if (!currentPlanId || planEvents.length === 0) { setNotice('Najpierw wybierz grupę z planem', true); return; }
    renderExams();
    examsModal.showModal();
    examsModal.scrollTo(0, 0);
}

function buildingOf(room) {
    for (const part of String(room || '').split(/\s*[;,]\s*/)) {
        const m = ROOM_RE.exec(part.trim());
        if (m) return m[2];
    }
    return '';
}

function defaultRoomsQuery() {
    const now = warsawNow();
    const mins = now.getHours() * 60 + now.getMinutes();
    let date = todayISO();
    let slot = SLOTS.findIndex(([, end]) => toMin(end) > mins);
    if (slot < 0) { date = addDaysISO(date, 1); slot = 0; }
    if (dayIndex(parseISO(date)) === 6) date = addDaysISO(date, 1);
    const mine = (eventsByDate[date] || []).find(e => toMin(e.end) > (date === todayISO() ? mins : 0) && buildingOf(e.room));
    return { date, slot, building: mine ? buildingOf(mine.room) : '' };
}

let roomsRequest = 0;

async function loadRooms() {
    const [from, to] = SLOTS[Number(roomsSlot.value)] || SLOTS[0];
    const params = new URLSearchParams({ date: roomsDate.value, from, to });
    if (roomsBuilding.value) params.set('building', roomsBuilding.value);
    const id = ++roomsRequest;
    roomsBody.replaceChildren(h('div', { class: 'message' }, 'Szukam wolnych sal...'));
    try {
        const data = await fetchJSON('/api/rooms?' + params);
        if (id !== roomsRequest) return;
        const keep = roomsBuilding.value;
        roomsBuilding.replaceChildren(h('option', { value: '' }, 'Wszystkie'),
            ...data.buildings.map(b => h('option', { value: b }, `bud. ${b}`)));
        roomsBuilding.value = data.buildings.includes(keep) ? keep : '';
        roomsSub.textContent = `${shortDate(roomsDate.value)}, ${from}–${to}`;
        if (!data.has_data) {
            roomsBody.replaceChildren(h('div', { class: 'empty-day' }, h('b', null, 'Brak zajęć w planach'),
                'Tego dnia żadna grupa nie ma zajęć, np. weekend albo dzień wolny. Wybierz inny dzień.'));
            return;
        }
        const free = data.free || [];
        const total = free.reduce((n, b) => n + b.rooms.length, 0);
        if (total === 0) {
            roomsBody.replaceChildren(h('div', { class: 'empty-day' }, h('b', null, 'Brak wolnych sal'), 'Spróbuj innego bloku albo budynku.'));
            return;
        }
        const summary = h('p', { class: 'rooms-summary' },
            h('b', null, `${total} ${plural(total, 'wolna sala', 'wolne sale', 'wolnych sal')}`),
            free.length > 1 ? ` w ${free.length} budynkach` : ` w bud. ${free[0].building}`);
        roomsBody.replaceChildren(summary, ...free.map(roomsBuildingCard));
    } catch (err) {
        if (id === roomsRequest) roomsBody.replaceChildren(h('div', { class: 'message error' }, err.message));
    }
}

function freeLabel(r) {
    if (r.free_from <= '07:30' && r.free_until >= '21:30') return 'cały dzień';
    if (r.free_from <= '07:30') return `do ${r.free_until}`;
    if (r.free_until >= '21:30') return `od ${r.free_from}`;
    return `${r.free_from}–${r.free_until}`;
}

function roomsBuildingCard(b) {
    const LIMIT = 12;
    const grid = h('div', { class: 'room-grid' });
    const fill = (all) => grid.replaceChildren(...(all ? b.rooms : b.rooms.slice(0, LIMIT)).map(r =>
        h('div', { class: 'room-chip' }, h('b', null, r.room), h('small', null, freeLabel(r)))));
    fill(false);
    const more = b.rooms.length > LIMIT
        ? h('button', { type: 'button', class: 'link-btn', onclick: (e) => { fill(true); e.currentTarget.remove(); } }, `Pokaż wszystkie (${b.rooms.length})`)
        : null;
    return h('section', { class: 'room-building' },
        h('header', null, h('b', null, `Budynek ${b.building}`), h('span', null, `${b.rooms.length} z ${b.total} wolnych`)),
        grid, more);
}

function openRooms() {
    if (!roomsSlot.options.length) {
        roomsSlot.replaceChildren(...SLOTS.map(([a, b], i) => h('option', { value: String(i) }, `${a}–${b}`)));
    }
    const q = defaultRoomsQuery();
    roomsDate.value = q.date;
    roomsSlot.value = String(q.slot);
    if (q.building && ![...roomsBuilding.options].some(o => o.value === q.building)) {
        roomsBuilding.append(h('option', { value: q.building }, `bud. ${q.building}`));
    }
    roomsBuilding.value = q.building;
    roomsModal.showModal();
    loadRooms();
}

for (const el of [roomsDate, roomsSlot, roomsBuilding]) el.addEventListener('change', loadRooms);

const COMPARE_KEY = 'compareGroups';
const COMPARE_MAX = 4;
const compareCache = {};
let compareIds = [];
let compareWeek = '';

function saveCompare() { safeSet(COMPARE_KEY, JSON.stringify(compareIds)); }

function loadCompareIds() {
    let ids = [];
    try { ids = JSON.parse(safeGet(COMPARE_KEY) || '[]'); } catch (_) { ids = []; }
    ids = (Array.isArray(ids) ? ids : []).filter(x => typeof x === 'string');
    if (currentPlanId && !ids.includes(currentPlanId)) ids.unshift(currentPlanId);
    return ids.slice(0, COMPARE_MAX);
}

async function compareEvents(id) {
    if (!compareCache[id]) {
        compareCache[id] = fetchJSON('/api/plan/' + encodeURIComponent(id)).then(d => {
            const byDate = {};
            for (const e of d.events || []) (byDate[e.date] ||= []).push(e);
            for (const list of Object.values(byDate)) list.sort((a, b) => a.start.localeCompare(b.start));
            return byDate;
        }).catch(err => { delete compareCache[id]; throw err; });
    }
    return compareCache[id];
}

function renderCompareGroups() {
    compareGroupsEl.replaceChildren(...compareIds.map((id, i) => h('span', { class: `cmp-chip c${i}` },
        h('span', { class: 'dot' }), id,
        h('button', { type: 'button', 'aria-label': `Usuń ${id} z porównania`, onclick: () => { compareIds = compareIds.filter(x => x !== id); saveCompare(); renderCompare(); } }, '×'))));
    compareSearch.disabled = compareIds.length >= COMPARE_MAX;
    compareSearch.placeholder = compareIds.length >= COMPARE_MAX ? 'Najwyżej 4 grupy' : 'Dodaj grupę, np. WEL26EY4S0';
}

function renderCompareResults() {
    const q = compareSearch.value.trim();
    if (!q) { compareResults.replaceChildren(); return; }
    const list = WatGroups.filter(q).filter(p => !compareIds.includes(p.id)).slice(0, 8);
    compareResults.replaceChildren(...(list.length ? list.map(p => h('button', {
        type: 'button', class: 'search-result-item', onclick: () => {
            compareIds = [...compareIds, p.id].slice(0, COMPARE_MAX);
            compareSearch.value = '';
            compareResults.replaceChildren();
            saveCompare();
            renderCompare();
        },
    }, p.name)) : [h('div', { class: 'search-empty' }, 'Brak grup')]));
}

compareSearch.addEventListener('input', renderCompareResults);

function slotEvents(events, s) {
    return (events || []).filter(e => slotStart(e.start) <= s && slotEnd(e.end) >= s);
}

async function renderCompare() {
    renderCompareGroups();
    if (compareIds.length === 0) {
        compareBody.replaceChildren(h('div', { class: 'empty-day' }, h('b', null, 'Dodaj grupy'), 'Wpisz kod grupy powyżej, żeby zobaczyć plany obok siebie.'));
        return;
    }
    const week = compareWeek;
    const nav = h('div', { class: 'nav-row' },
        h('button', { type: 'button', class: 'round-btn', 'aria-label': 'Poprzedni tydzień', onclick: () => { compareWeek = addDaysISO(compareWeek, -7); renderCompare(); } }, svg(ICON_LEFT, 20)),
        h('div', { class: 'nav-label' }, h('b', null, weekRange(week)), h('small', null, weekNumber(week))),
        h('button', { type: 'button', class: 'round-btn', 'aria-label': 'Następny tydzień', onclick: () => { compareWeek = addDaysISO(compareWeek, 7); renderCompare(); } }, svg(ICON_RIGHT, 20)));
    compareBody.replaceChildren(nav, h('div', { class: 'message' }, 'Ładowanie planów...'));

    const ids = compareIds.slice();
    let data;
    try {
        data = await Promise.all(ids.map(compareEvents));
    } catch (err) {
        compareBody.replaceChildren(nav, h('div', { class: 'message error' }, err.message));
        return;
    }
    if (week !== compareWeek || ids.join() !== compareIds.join()) return;

    const days = [];
    for (let i = 0; i < 7; i++) {
        const iso = addDaysISO(week, i);
        const perGroup = data.map(byDate => byDate[iso] || []);
        if (i >= 5 && perGroup.every(l => l.length === 0)) continue;
        days.push(compareDay(iso, i, ids, perGroup));
    }
    compareBody.replaceChildren(nav, ...days);
}

function compareDay(iso, dayIdx, ids, perGroup) {
    const dd = parseISO(iso);
    const head = h('header', null, h('b', null, DAY_NAMES[dayIdx]), h('span', null, `${dd.getDate()} ${MONTHS_GEN[dd.getMonth()]}`));
    const all = perGroup.flat();
    if (all.length === 0) {
        return h('section', { class: 'cmp-day' }, head, h('p', { class: 'cmp-free-day' }, 'Wszyscy wolni cały dzień'));
    }
    let lo = SLOTS.length - 1, hi = 0;
    for (const e of all) { lo = Math.min(lo, slotStart(e.start)); hi = Math.max(hi, slotEnd(e.end)); }
    const table = h('div', { class: 'cmp-table', style: `--cols:${ids.length}` });
    table.append(h('span', { class: 'cmp-h' }), ...ids.map((id, i) => h('span', { class: `cmp-h c${i}` }, h('span', { class: 'dot' }), id)));
    let shared = 0;
    for (let s = lo; s <= hi; s++) {
        const cells = perGroup.map(list => slotEvents(list, s));
        const allFree = cells.every(c => c.length === 0);
        if (allFree) shared++;
        table.append(h('span', { class: 'cmp-time' + (allFree ? ' free' : '') }, h('b', null, SLOTS[s][0]), h('small', null, SLOTS[s][1])));
        if (allFree) {
            table.append(h('span', { class: 'cmp-all-free' }, 'wszyscy wolni'));
            continue;
        }
        cells.forEach((list, i) => table.append(list.length
            ? h('span', { class: `cmp-cell c${i}` }, ...list.slice(0, 2).map(e => h('span', { class: 'cmp-ev', title: `${e.subject}, ${typeLabel(e.type)}${e.room ? ', sala ' + e.room : ''}` },
                h('b', { class: 'ab' }, abbreviate(e.subject)), h('b', { class: 'full' }, e.subject),
                h('small', null, [typeShort(e.type), splitRooms(e.room).main].filter(Boolean).join(' · ')))))
            : h('span', { class: 'cmp-cell empty' }, '—')));
    }
    const ends = perGroup.map(l => l.length ? l[l.length - 1].end : null);
    const note = shared ? `${shared} ${plural(shared, 'wspólny wolny blok', 'wspólne wolne bloki', 'wspólnych wolnych bloków')}` : 'brak wspólnego wolnego bloku';
    return h('section', { class: 'cmp-day' }, head, table,
        h('p', { class: 'cmp-note' }, note + (ends.every(Boolean) ? ` · wszyscy kończą o ${ends.sort().pop()}` : '')));
}

function openCompare() {
    compareIds = loadCompareIds();
    compareWeek = mondayOf(selectedDate || todayISO());
    compareSearch.value = '';
    compareResults.replaceChildren();
    compareModal.showModal();
    renderCompare();
}

function printMonth() {
    if (!currentPlanId || allEvents.length === 0) { setNotice('Najpierw wybierz grupę z planem', true); return; }
    const base = parseISO(selectedDate || todayISO());
    const year = base.getFullYear(), month = base.getMonth();
    const first = new Date(year, month, 1);
    const last = new Date(year, month + 1, 0);
    const prefix = `${year}-${pad(month + 1)}`;
    const weekend = Object.keys(eventsByDate).some(d => d.startsWith(prefix) && dayIndex(parseISO(d)) >= 5);
    const cols = weekend ? 7 : 5;
    const grid = h('div', { class: 'pr-grid', style: `--cols:${cols}` });
    for (let i = 0; i < cols; i++) grid.append(h('div', { class: 'pr-head' }, DAY_NAMES[i]));
    let cursor = addDays(first, -dayIndex(first));
    let count = 0;
    while (cursor <= last) {
        const shown = Array.from({ length: cols }, (_, i) => addDays(cursor, i));
        if (!shown.some(d => d.getMonth() === month)) { cursor = addDays(cursor, 7); continue; }
        for (let i = 0; i < 7; i++) {
            const day = addDays(cursor, i);
            if (i >= cols) continue;
            const iso = toISO(day);
            const inMonth = day.getMonth() === month;
            const events = inMonth ? (eventsByDate[iso] || []) : [];
            count += events.length;
            grid.append(h('div', { class: 'pr-day' + (inMonth ? '' : ' out') },
                h('div', { class: 'pr-date' }, inMonth ? String(day.getDate()) : ''),
                ...events.map(e => h('div', { class: 'pr-ev' },
                    h('b', null, `${e.start}–${e.end}`), ' ', e.subject,
                    h('span', null, ` · ${typeShort(e.type)}${e.room ? ' · ' + splitRooms(e.room).main.replace(/ /g, '\u00a0') : ''}`)))));
        }
        cursor = addDays(cursor, 7);
    }
    const name = (allPlans.find(p => p.id === currentPlanId) || {}).name || currentPlanId;
    const hidden = hiddenSubjects(currentPlanId).size;
    printArea.replaceChildren(
        h('header', { class: 'pr-top' },
            h('h1', null, `${name} · ${MONTHS_NOM[month]} ${year}`),
            h('span', null, `${classesWord(count)}${hidden ? ` · bez ${hidden} ukrytych przedmiotów` : ''} · watplan.me`)),
        grid);
    const title = document.title;
    document.title = `Plan ${name} ${MONTHS_NOM[month]} ${year}`;
    window.addEventListener('afterprint', () => { document.title = title; }, { once: true });
    window.print();
}


if (new URLSearchParams(window.location.search).has('changes')) {
    const url = new URL(window.location);
    url.searchParams.delete('changes');
    window.history.replaceState(window.history.state, '', url);
    setTimeout(() => openChanges(currentPlanId), 0);
}


loadPlans();

if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => navigator.serviceWorker.register('/sw.js').catch(() => undefined));
}
