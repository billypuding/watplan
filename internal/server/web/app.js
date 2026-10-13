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

const planSelector = document.getElementById('planSelector');
const weekSelector = document.getElementById('weekSelector');
const refreshButton = document.getElementById('refreshButton');
const grid = document.getElementById('calendar-grid');
const fetchedLabel = document.getElementById('fetched');
const modifiedLabel = document.getElementById('modified');
const notice = document.getElementById('notice');

let eventsByWeek = {};
let loadCounter = 0;
let noticeTimer = null;

function pad(n) {
    return String(n).padStart(2, '0');
}

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

function dayIndex(date) {
    return (date.getDay() + 6) % 7;
}

function getMonday(str) {
    const date = parseISO(str);
    return toISO(addDays(date, -dayIndex(date)));
}

function textColorFor(hex) {
    const match = /^#([0-9a-f]{6})$/i.exec(hex || '');
    if (!match) return '#1e293b';
    const n = parseInt(match[1], 16);
    const brightness = 0.299 * (n >> 16) + 0.587 * ((n >> 8) & 255) + 0.114 * (n & 255);
    return brightness > 150 ? '#1e293b' : '#ffffff';
}

function setNotice(text, isError) {
    clearTimeout(noticeTimer);
    notice.textContent = text;
    notice.classList.toggle('error', Boolean(isError));
    if (text) noticeTimer = setTimeout(() => setNotice(''), NOTICE_MS);
}

function showGridMessage(text, isError) {
    const message = document.createElement('div');
    message.className = isError ? 'grid-message error' : 'grid-message';
    message.textContent = text;
    grid.replaceChildren(message);
}

async function fetchJSON(url, options) {
    const response = await fetch(url, options);
    let body = null;
    try {
        body = await response.json();
    } catch (_) {
        body = null;
    }
    if (!response.ok) {
        throw new Error((body && body.error) || `Błąd serwera (${response.status})`);
    }
    return body;
}

async function loadPlans() {
    try {
        const plans = await fetchJSON('/api/plans');
        planSelector.replaceChildren(...plans.map(p => new Option(p.name, p.id)));
        await loadPlan();
    } catch (err) {
        showGridMessage(err.message, true);
    }
}

async function loadPlan() {
    const requestId = ++loadCounter;
    showGridMessage('Ładowanie...', false);

    try {
        const data = await fetchJSON('/api/plan/' + encodeURIComponent(planSelector.value));
        if (requestId !== loadCounter) return;

        fetchedLabel.textContent = data.fetched_at || 'Brak';
        modifiedLabel.textContent = data.modified_at || 'Brak';
        eventsByWeek = groupByWeek(data.events || []);
        populateWeekSelector();
    } catch (err) {
        if (requestId !== loadCounter) return;

        eventsByWeek = {};
        fetchedLabel.textContent = '-';
        modifiedLabel.textContent = '-';
        weekSelector.replaceChildren(new Option('Brak danych', ''));
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

function populateWeekSelector() {
    const mondays = Object.keys(eventsByWeek).sort();
    if (mondays.length === 0) {
        weekSelector.replaceChildren(new Option('Brak zajęć', ''));
        showGridMessage('Brak zajęć w planie', false);
        return;
    }

    weekSelector.replaceChildren(...mondays.map((monday, i) => {
        const sunday = toISO(addDays(parseISO(monday), 6));
        return new Option(`Tydzień ${i + 1}/${mondays.length} (${monday} do ${sunday})`, monday);
    }));

    weekSelector.value = pickInitialWeek(mondays);
    renderSelectedWeek();
}

function pickInitialWeek(mondays) {
    const currentMonday = getMonday(toISO(new Date()));
    return mondays.find(monday => monday >= currentMonday) || mondays[mondays.length - 1];
}

function renderSelectedWeek() {
    renderWeek(weekSelector.value);
}

function renderWeek(monday) {
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
    const small = document.createElement('small');
    small.textContent = `${pad(date.getDate())}.${pad(date.getMonth() + 1)}`;
    cell.appendChild(small);
    return cell;
}

function createEventCard(event) {
    const rowStart = START_ROW[event.start];
    const rowEnd = END_ROW_LINE[event.end];
    if (!rowStart || !rowEnd) {
        console.warn('Pominięto zajęcia spoza siatki godzin:', event);
        return null;
    }

    const column = dayIndex(parseISO(event.date)) + 2;
    const card = createCell('event-card', `${rowStart} / ${rowEnd}`, column);
    card.style.backgroundColor = event.color || '#e2e8f0';
    card.style.color = textColorFor(event.color);

    card.appendChild(createSpan('event-subject', event.subject));
    if (event.type) card.appendChild(createSpan('event-type', event.type));
    if (event.room) card.appendChild(createSpan('', `Sala: ${event.room}`));
    if (event.teacher) card.appendChild(createSpan('event-teacher', event.teacher));
    return card;
}

async function refreshServer() {
    refreshButton.textContent = 'Odświeżanie...';
    refreshButton.disabled = true;

    try {
        await fetchJSON('/api/refresh', { method: 'POST' });
        await loadPlan();
        setNotice('Plan odświeżony', false);
    } catch (err) {
        setNotice(err.message, true);
    } finally {
        refreshButton.textContent = 'Odśwież z serwera';
        refreshButton.disabled = false;
    }
}

planSelector.addEventListener('change', loadPlan);
weekSelector.addEventListener('change', renderSelectedWeek);
refreshButton.addEventListener('click', refreshServer);

loadPlans();
