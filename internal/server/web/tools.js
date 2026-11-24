'use strict';

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
const examsModal = $('examsModal');
const examsBody = $('examsBody');
const examsSub = $('examsSub');

for (const dlg of [roomsModal, compareModal, examsModal]) {
    dlg.addEventListener('click', (e) => { if (e.target.closest('[data-close]')) dlg.close(); });
}

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
        if (id === roomsRequest) roomsBody.replaceChildren(h('div', { class: 'message error' }, errorText(err)));
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
        h('header', null, h('b', null, `Budynek ${b.building}`), h('span', null, `${b.rooms.length} z ${b.total} wolnych`),
            MAP_BUILDINGS.has(b.building) ? h('button', { type: 'button', class: 'link-btn map-link', onclick: () => lazyTool(MAP_JS, 'openCampusMap', b.building) }, 'Mapa') : null),
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
        compareBody.replaceChildren(nav, h('div', { class: 'message error' }, errorText(err)));
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
