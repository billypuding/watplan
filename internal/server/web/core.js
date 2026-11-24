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

const $ = (id) => document.getElementById(id);

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

const OFFLINE_TEXT = 'Brak połączenia z internetem. Sprawdź sieć i spróbuj ponownie.';
const OFFLINE_PLAN_TEXT = 'Brak połączenia z internetem, a tego planu nie ma jeszcze zapisanego na tym urządzeniu. Otwórz go ponownie, gdy wróci sieć.';

const isNetworkError = (err) => !navigator.onLine || err instanceof TypeError;

function errorText(err, fallback = 'Coś poszło nie tak. Spróbuj ponownie.') {
    if (isNetworkError(err)) return OFFLINE_TEXT;
    return (err && err.message) || fallback;
}

async function fetchJSON(url, options) {
    const response = await fetch(url, options);
    let body = null;
    try { body = await response.json(); } catch (_) { body = null; }
    if (!response.ok) throw new Error((body && body.error) || `Błąd serwera (${response.status})`);
    return body;
}

const MAP_BUILDINGS = new Set('5|K|S|10|13|14|15|16|17|19|20|21|22|23|24|25|26|28|29|34|35|36|38|39|41|42|45|46|47|49|50|52|53|54|55|56|57|58|59|60|61|62|63|64|65|66|67|68|69|70|71|72|74|75|80|100|114|121|125|126|135|136|144|158|23A|63A|AW 1|AW 2|AW 3|AW 4|AW 5|AW 7|SWF'.split('|'));
const MAP_JS = '/map.js?v=0';

function roomBuilding(room) {
    for (const part of String(room || '').split(/\s*[;,]\s*/)) {
        const text = part.trim();
        const dorm = /\bAW\s*(\d)\b/i.exec(text);
        if (dorm) return 'AW ' + dorm[1];
        const m = /(?:\s|\/)\s*(\d{1,3}[A-Z]?|[A-Z])$/.exec(text);
        if (m) return m[1].replace(/^0+(?=\d)/, '');
    }
    return '';
}

function eventBuilding(e) {
    if (!e) return '';
    const code = roomBuilding(e.room);
    if (code) return code;
    return /wychowanie fizyczne/i.test(e.subject || '') ? 'SWF' : '';
}

function buildingLabel(code) {
    if (code === 'S') return 'Budynek 100';
    if (code === 'K') return 'Klub WAT';
    if (code === 'SWF') return 'Studium WF';
    if (code.startsWith('AW ')) return 'Akademik ' + code;
    return 'Budynek ' + code;
}
