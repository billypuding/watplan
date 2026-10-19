const DAY_NAMES = ['Poniedziałek', 'Wtorek', 'Środa', 'Czwartek', 'Piątek', 'Sobota', 'Niedziela'];
const SLOT_LABELS = [
    '08:00 - 09:35', '09:50 - 11:25', '11:40 - 13:15', '13:30 - 15:05',
    '16:00 - 17:35', '17:50 - 19:25', '19:40 - 21:15',
];
const START_ROW = { '08:00': 2, '09:50': 3, '11:40': 4, '13:30': 5, '16:00': 6, '17:50': 7, '19:40': 8 };
const END_ROW_LINE = { '09:35': 3, '11:25': 4, '13:15': 5, '15:05': 6, '17:35': 7, '19:25': 8, '21:15': 9 };
const WORKDAYS = 5;
const WEEKDAYS = 7;
const NOTICE_MS = 6000;
const TICK_MS = 15000;
const SETTINGS_KEY = 'settings';

const planSearch = document.getElementById('planSearch');
const searchResults = document.getElementById('searchResults');
const weekSelector = document.getElementById('weekSelector');
const monthLabel = document.getElementById('monthLabel');
const prevWeekBtn = document.getElementById('prevWeekBtn');
const nextWeekBtn = document.getElementById('nextWeekBtn');
const refreshButton = document.getElementById('refreshButton');
const settingsBtn = document.getElementById('settingsBtn');
const settingsModal = document.getElementById('settingsModal');
const optLive = document.getElementById('optLive');
const optDimPast = document.getElementById('optDimPast');
const downloadIcsBtn = document.getElementById('downloadIcs');
const grid = document.getElementById('calendar-grid');
const fetchedLabel = document.getElementById('fetched');
const modifiedLabel = document.getElementById('modified');
const notice = document.getElementById('notice');

let eventsByWeek = {};
let allEvents = [];
let loadCounter = 0;
let noticeTimer = null;
let allPlans = [];
let currentPlanId = '';
let monthCursor = ''; // 'YYYY-MM'

/* ---------- Settings ---------- */

const defaults = {
    theme: localStorage.getItem('theme') || 'auto', // migrates the old key
    view: 'week',
    live: true,
    dimPast: true,
};

function loadSettings() {
    try {
        return { ...defaults, ...JSON.parse(localStorage.getItem(SETTINGS_KEY) || '{}') };
    } catch (_) {
        return { ...defaults };
    }
}

const settings = loadSettings();

function saveSettings() {
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(settings));
}

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
        if (key === 'view') renderCurrent();
    });
});

optLive.addEventListener('change', () => { settings.live = optLive.checked; saveSettings(); tickLive(); });
optDimPast.addEventListener('change', () => { settings.dimPast = optDimPast.checked; saveSettings(); tickLive(); });
settingsBtn.addEventListener('click', () => settingsModal.showModal());
settingsModal.addEventListener('click', (e) => { if (e.target === settingsModal) settingsModal.close(); });

applyTheme();
syncSettingsUI();

/* ---------- Date helpers ---------- */

function pad(n) { return String(n).padStart(2, '0'); }

function toISO(date) {
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function parseISO(str) {
    const [y, m, d] = str.split('-').map(Number);
    return new Date(y, m - 1, d);
}

function addDays(date, n) {
    const result = new Date(date);
    result.setDate(result.getDate() + n);
    return result;
}

function dayIndex(date) { return (date.getDay() + 6) % 7; }

function getMonday(str) {
    const date = parseISO(str);
    return toISO(addDays(date, -dayIndex(date)));
}

// "Now" in Warsaw, as a Date whose local fields show Warsaw wall-clock time.
// Event times below are built the same way, so the two can be compared directly.
function warsawNow() {
    return new Date(new Date().toLocaleString('en-US', { timeZone: 'Europe/Warsaw' }));
}

function eventRange(event) {
    const [y, m, d] = event.date.split('-').map(Number);
    const [sh, sm] = event.start.split(':').map(Number);
    const [eh, em] = event.end.split(':').map(Number);
    return [new Date(y, m - 1, d, sh, sm).getTime(), new Date(y, m - 1, d, eh, em).getTime()];
}

function formatMinutes(mins) {
    if (mins < 60) return `${mins} min`;
    const h = Math.floor(mins / 60);
    const m = mins % 60;
    return m ? `${h} h ${m} min` : `${h} h`;
}

function textColorFor(hex) {
    const match = /^#([0-9a-f]{6})$/i.exec(hex || '');
    if (!match) return '#1e293b';
    const n = parseInt(match[1], 16);
    const brightness = 0.299 * (n >> 16) + 0.587 * ((n >> 8) & 255) + 0.114 * (n & 255);
    return brightness > 150 ? '#1e293b' : '#ffffff';
}

/* ---------- UI helpers ---------- */

function setNotice(text, isError) {
    clearTimeout(noticeTimer);
    notice.textContent = text;
    notice.classList.toggle('error', Boolean(isError));
    if (text) noticeTimer = setTimeout(() => setNotice(''), NOTICE_MS);
}

function showGridMessage(text, isError) {
    grid.classList.remove('month-grid');
    const message = document.createElement('div');
    message.className = isError ? 'grid-message error' : 'grid-message';
    message.textContent = text;
    grid.replaceChildren(message);
}

async function fetchJSON(url, options) {
    const response = await fetch(url, options);
    let body = null;
    try { body = await response.json(); } catch (_) { body = null; }
    if (!response.ok) {
        throw new Error((body && body.error) || `Błąd serwera (${response.status})`);
    }
    return body;
}

/* ---------- Plans + search ---------- */

// Groups are shown under a faculty. Uses plan.faculty from plans.json when present,
// otherwise falls back to the first three letters of the id (WEL, WMT, ...).
function facultyOf(plan) {
    return plan.faculty || plan.id.slice(0, 3).toUpperCase();
}

async function loadPlans() {
    try {
        allPlans = await fetchJSON('/api/plans');

        const urlParams = new URLSearchParams(window.location.search);
        const targetPlan = urlParams.get('plan') || localStorage.getItem('selectedPlan');

        if (targetPlan) {
            const found = allPlans.find(p => p.id === targetPlan);
            if (found) {
                currentPlanId = found.id;
                planSearch.value = found.name;
                localStorage.setItem('selectedPlan', currentPlanId);
            }
        }

        if (currentPlanId) {
            await loadPlan();
        } else {
            showGridMessage('Wpisz nazwę grupy i wybierz ją z listy, aby zobaczyć plan', false);
        }
    } catch (err) {
        showGridMessage(err.message, true);
    }
}

function renderSearchResults() {
    const query = planSearch.value.trim().toLowerCase();
    searchResults.replaceChildren();

    const filtered = allPlans.filter(p =>
        !query || p.name.toLowerCase().includes(query) || p.id.toLowerCase().includes(query)
    );

    if (filtered.length === 0) {
        const div = document.createElement('div');
        div.className = 'search-result-item';
        div.textContent = 'Nie znaleziono grupy';
        searchResults.appendChild(div);
    } else {
        const byFaculty = {};
        for (const plan of filtered) (byFaculty[facultyOf(plan)] ||= []).push(plan);

        for (const faculty of Object.keys(byFaculty).sort()) {
            const title = document.createElement('div');
            title.className = 'search-group-title';
            title.textContent = faculty;
            searchResults.appendChild(title);

            for (const plan of byFaculty[faculty]) {
                const item = document.createElement('div');
                item.className = 'search-result-item';
                item.textContent = plan.name;
                item.addEventListener('click', () => selectPlan(plan.id, plan.name));
                searchResults.appendChild(item);
            }
        }
    }
    searchResults.classList.remove('hidden');
}

planSearch.addEventListener('input', renderSearchResults);
planSearch.addEventListener('focus', renderSearchResults);

document.addEventListener('click', (e) => {
    if (!planSearch.contains(e.target) && !searchResults.contains(e.target)) {
        searchResults.classList.add('hidden');
    }
});

function selectPlan(id, name) {
    currentPlanId = id;
    planSearch.value = name;
    searchResults.classList.add('hidden');
    localStorage.setItem('selectedPlan', currentPlanId);

    const url = new URL(window.location);
    url.searchParams.set('plan', currentPlanId);
    window.history.replaceState({}, '', url);

    loadPlan();
}

async function loadPlan() {
    if (!currentPlanId) return;
    const requestId = ++loadCounter;
    showGridMessage('Ładowanie...', false);

    try {
        const data = await fetchJSON('/api/plan/' + encodeURIComponent(currentPlanId));
        if (requestId !== loadCounter) return;

        fetchedLabel.textContent = data.fetched_at || 'Brak';
        modifiedLabel.textContent = data.modified_at || 'Brak';
        allEvents = data.events || [];
        eventsByWeek = groupByWeek(allEvents);
        monthCursor = pickInitialMonth();
        populateWeekSelector();
    } catch (err) {
        if (requestId !== loadCounter) return;
        allEvents = [];
        eventsByWeek = {};
        fetchedLabel.textContent = '-';
        modifiedLabel.textContent = '-';
        weekSelector.replaceChildren(new Option('Brak danych', ''));
        updateNavButtons();
        showGridMessage(err.message, true);
    }
}

function groupByWeek(events) {
    const grouped = {};
    for (const event of events) {
        const monday = getMonday(event.date);
        (grouped[monday] ||= []).push(event);
    }
    return grouped;
}

/* ---------- Week view ---------- */

function populateWeekSelector() {
    const mondays = Object.keys(eventsByWeek).sort();
    if (mondays.length === 0) {
        weekSelector.replaceChildren(new Option('Brak zajęć', ''));
        updateNavButtons();
        showGridMessage('Brak zajęć w planie', false);
        return;
    }

    weekSelector.replaceChildren(...mondays.map((monday, i) => {
        return new Option(`Tydzień ${i + 1}/${mondays.length}`, monday);
    }));

    weekSelector.value = pickInitialWeek(mondays);
    renderCurrent();
}

function pickInitialWeek(mondays) {
    const currentMonday = getMonday(toISO(warsawNow()));
    return mondays.find(monday => monday >= currentMonday) || mondays[mondays.length - 1];
}

function renderCurrent() {
    const isMonth = settings.view === 'month';
    weekSelector.classList.toggle('hidden', isMonth);
    monthLabel.classList.toggle('hidden', !isMonth);

    if (allEvents.length === 0) { updateNavButtons(); return; }

    if (isMonth) renderMonth();
    else renderWeek(weekSelector.value);
    updateNavButtons();
    tickLive();
}

function updateNavButtons() {
    if (settings.view === 'month') {
        const months = monthKeys();
        const idx = months.indexOf(monthCursor);
        prevWeekBtn.disabled = idx <= 0;
        nextWeekBtn.disabled = idx === -1 || idx >= months.length - 1;
        return;
    }
    const idx = weekSelector.selectedIndex;
    const maxIdx = weekSelector.options.length - 1;
    prevWeekBtn.disabled = idx <= 0 || maxIdx < 0;
    nextWeekBtn.disabled = idx >= maxIdx || maxIdx < 0;
}

function renderWeek(monday) {
    grid.classList.remove('month-grid');
    grid.replaceChildren();
    if (!monday) return;

    const events = eventsByWeek[monday] || [];
    const days = events.some(e => dayIndex(parseISO(e.date)) >= WORKDAYS) ? WEEKDAYS : WORKDAYS;
    grid.style.setProperty('--days', days);

    for (let row = 2; row <= 8; row++) {
        for (let col = 2; col <= days + 1; col++) {
            grid.appendChild(createCell('cell-bg', row, col));
        }
    }

    grid.appendChild(createCell('header-cell', 1, 1, 'Godzina'));
    const start = parseISO(monday);
    for (let i = 0; i < days; i++) {
        grid.appendChild(createDayHeader(addDays(start, i), i));
    }

    SLOT_LABELS.forEach((label, i) => {
        grid.appendChild(createCell('time-cell', i + 2, 1, label));
    });

    for (const event of events) {
        const card = createEventCard(event);
        if (card) grid.appendChild(card);
    }
}

function createCell(className, row, col, text = '') {
    const cell = document.createElement('div');
    cell.className = className;
    cell.style.gridRow = row;
    cell.style.gridColumn = col;
    cell.textContent = text;
    return cell;
}

function createSpan(className, text) {
    const span = document.createElement('span');
    if (className) span.className = className;
    span.textContent = text;
    return span;
}

function createDayHeader(date, index) {
    const cell = createCell('header-cell', 1, index + 2, DAY_NAMES[index]);
    if (toISO(date) === toISO(warsawNow())) cell.classList.add('today');
    const small = document.createElement('small');
    small.textContent = `${pad(date.getDate())}.${pad(date.getMonth() + 1)}`;
    cell.appendChild(small);
    return cell;
}

function createEventCard(event) {
    const rowStart = START_ROW[event.start];
    const rowEnd = END_ROW_LINE[event.end];
    if (!rowStart || !rowEnd) return null;

    const column = dayIndex(parseISO(event.date)) + 2;
    const card = createCell('event-card', `${rowStart} / ${rowEnd}`, column);

    card.style.backgroundColor = event.color || '#e2e8f0';
    card.style.color = textColorFor(event.color);

    const [start, end] = eventRange(event);
    card.dataset.start = start;
    card.dataset.end = end;

    const body = document.createElement('div');
    body.className = 'event-body';
    body.appendChild(createSpan('event-subject', event.subject));
    if (event.type) body.appendChild(createSpan('event-type', event.type));
    if (event.room) body.appendChild(createSpan('', `Sala: ${event.room}`));
    if (event.teacher) body.appendChild(createSpan('event-teacher', event.teacher));
    body.appendChild(createSpan('event-remaining', ''));
    card.appendChild(body);
    return card;
}

/* ---------- Month view ---------- */

function monthKeys() {
    const set = new Set(allEvents.map(e => e.date.slice(0, 7)));
    return [...set].sort();
}

function pickInitialMonth() {
    const months = monthKeys();
    const current = toISO(warsawNow()).slice(0, 7);
    return months.includes(current) ? current : (months.find(m => m > current) || months[months.length - 1] || current);
}

function renderMonth() {
    grid.replaceChildren();
    grid.classList.add('month-grid');

    const [year, month] = monthCursor.split('-').map(Number);
    monthLabel.textContent = new Date(year, month - 1, 1).toLocaleDateString('pl-PL', { month: 'long', year: 'numeric' });

    DAY_NAMES.forEach(name => {
        const head = document.createElement('div');
        head.className = 'header-cell';
        head.textContent = name.slice(0, 3);
        grid.appendChild(head);
    });

    const first = new Date(year, month - 1, 1);
    const cursor = addDays(first, -dayIndex(first));
    const todayISO = toISO(warsawNow());
    const byDate = {};
    for (const e of allEvents) (byDate[e.date] ||= []).push(e);

    // 6 rows cover every possible month layout
    for (let i = 0; i < 42; i++) {
        const day = addDays(cursor, i);
        const iso = toISO(day);
        const cell = document.createElement('div');
        cell.className = 'month-cell';
        if (day.getMonth() !== month - 1) cell.classList.add('outside');
        if (iso === todayISO) cell.classList.add('today');
        cell.appendChild(createSpan('month-num', String(day.getDate())));

        const events = (byDate[iso] || []).slice().sort((a, b) => a.start.localeCompare(b.start));
        for (const e of events) {
            const pill = createSpan('month-pill', `${e.start} ${e.subject}`);
            pill.style.backgroundColor = e.color || '#e2e8f0';
            pill.style.color = textColorFor(e.color);
            pill.title = `${e.start}-${e.end} ${e.subject}${e.room ? ', sala ' + e.room : ''}`;
            const [s, en] = eventRange(e);
            pill.dataset.start = s;
            pill.dataset.end = en;
            cell.appendChild(pill);
        }
        grid.appendChild(cell);
    }
}

/* ---------- Live progress ---------- */

function tickLive() {
    const now = warsawNow().getTime();

    document.querySelectorAll('.event-card[data-start]').forEach(card => {
        const start = Number(card.dataset.start);
        const end = Number(card.dataset.end);
        const live = settings.live && now >= start && now < end;
        const past = settings.dimPast && now >= end;

        card.classList.toggle('live', live);
        card.classList.toggle('past', past);

        if (live) {
            const progress = ((now - start) / (end - start)) * 100;
            card.style.setProperty('--progress', progress.toFixed(2) + '%');
            const mins = Math.ceil((end - now) / 60000);
            card.querySelector('.event-remaining').textContent = `Zostało ${formatMinutes(mins)}`;
        } else {
            card.style.removeProperty('--progress');
        }
    });

    document.querySelectorAll('.month-pill[data-start]').forEach(pill => {
        const now2 = now;
        pill.classList.toggle('live', settings.live && now2 >= Number(pill.dataset.start) && now2 < Number(pill.dataset.end));
    });
}

setInterval(tickLive, TICK_MS);
document.addEventListener('visibilitychange', () => { if (!document.hidden) tickLive(); });

/* ---------- Navigation ---------- */

function stepPeriod(direction) {
    if (settings.view === 'month') {
        const months = monthKeys();
        const next = months[months.indexOf(monthCursor) + direction];
        if (!next) return;
        monthCursor = next;
    } else {
        const next = weekSelector.selectedIndex + direction;
        if (next < 0 || next >= weekSelector.options.length) return;
        weekSelector.selectedIndex = next;
    }
    renderCurrent();
}

weekSelector.addEventListener('change', renderCurrent);
prevWeekBtn.addEventListener('click', () => stepPeriod(-1));
nextWeekBtn.addEventListener('click', () => stepPeriod(1));

document.addEventListener('keydown', (e) => {
    if (e.target.matches('input, select, textarea') || settingsModal.open) return;
    if (e.key === 'ArrowLeft') stepPeriod(-1);
    if (e.key === 'ArrowRight') stepPeriod(1);
});

/* ---------- Refresh ---------- */

async function refreshServer() {
    if (!currentPlanId) return;
    const originalText = refreshButton.textContent;
    refreshButton.textContent = 'Odświeżanie...';
    refreshButton.disabled = true;

    try {
        await fetchJSON('/api/refresh?plan=' + encodeURIComponent(currentPlanId), { method: 'POST' });
        await loadPlan();
        setNotice('Plan odświeżony', false);
    } catch (err) {
        setNotice(err.message, true);
    } finally {
        refreshButton.textContent = originalText;
        refreshButton.disabled = false;
    }
}
refreshButton.addEventListener('click', refreshServer);

/* ---------- iCalendar export ---------- */

function icsEscape(text) {
    return String(text).replace(/\\/g, '\\\\').replace(/;/g, '\\;').replace(/,/g, '\\,').replace(/\r?\n/g, '\\n');
}

function icsTime(date, time) {
    return date.replace(/-/g, '') + 'T' + time.replace(':', '') + '00';
}

function buildICS() {
    const planName = (allPlans.find(p => p.id === currentPlanId) || {}).name || currentPlanId;
    const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d+/, '');
    const lines = [
        'BEGIN:VCALENDAR',
        'VERSION:2.0',
        'PRODID:-//WATplan//PL',
        'CALSCALE:GREGORIAN',
        'X-WR-CALNAME:' + icsEscape('WAT ' + planName),
        'X-WR-TIMEZONE:Europe/Warsaw',
        'BEGIN:VTIMEZONE',
        'TZID:Europe/Warsaw',
        'BEGIN:STANDARD',
        'DTSTART:19701025T030000',
        'TZOFFSETFROM:+0200',
        'TZOFFSETTO:+0100',
        'RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU',
        'END:STANDARD',
        'BEGIN:DAYLIGHT',
        'DTSTART:19700329T020000',
        'TZOFFSETFROM:+0100',
        'TZOFFSETTO:+0200',
        'RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU',
        'END:DAYLIGHT',
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