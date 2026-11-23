'use strict';

function moreDialog(id, title, sub) {
    let dlg = $(id);
    if (dlg) return dlg;
    dlg = h('dialog', { id, class: 'fullscreen' },
        h('div', { class: 'series-head' },
            h('button', { class: 'round-btn', type: 'button', 'data-close': true, 'aria-label': 'Wróć' }, svg(ICON_LEFT, 20)),
            h('div', { class: 'series-title' }, h('h2', null, title), h('p', { id: id + 'Sub' }, sub))),
        h('div', { class: 'series-body', id: id + 'Body' }));
    dlg.addEventListener('click', (e) => { if (e.target.closest('[data-close]')) dlg.close(); });
    nameDialog(dlg);
    document.body.append(dlg);
    return dlg;
}

function showDialog(dlg) {
    if (!dlg.open) dlg.showModal();
    dlg.scrollTo(0, 0);
}

const field = (label, input, attrs) => h('label', attrs, h('span', null, label), input);

function extrasForm() {
    const weekday = h('select', { id: 'extraWeekday' }, DAY_NAMES.map((name, i) => h('option', { value: String(i) }, name)));
    const repeat = h('select', { id: 'extraRepeat', onchange: syncExtraForm },
        h('option', { value: 'weekly' }, 'Co tydzień'), h('option', { value: 'once' }, 'Jednorazowo'));
    return h('form', { id: 'extraForm', class: 'tool-filters extra-form', novalidate: true, onsubmit: addExtra },
        field('Nazwa', h('input', { id: 'extraName', maxlength: '80', autocomplete: 'off', placeholder: 'np. Lektorat angielskiego' }), { class: 'wide' }),
        field('Powtarzanie', repeat),
        field('Dzień', weekday, { id: 'extraWeekdayLabel' }),
        field('Data', h('input', { type: 'date', id: 'extraDate' }), { id: 'extraDateLabel', hidden: true }),
        field('Od', h('input', { type: 'time', id: 'extraStart', value: '08:00' })),
        field('Do', h('input', { type: 'time', id: 'extraEnd', value: '09:35' })),
        field('Sala (opcjonalnie)', h('input', { id: 'extraRoom', maxlength: '40', autocomplete: 'off' }), { class: 'wide' }),
        h('p', { class: 'hint error wide', id: 'extraError', role: 'alert', hidden: true }),
        h('button', { class: 'primary-btn wide', type: 'submit' }, 'Dodaj'));
}

function syncExtraForm() {
    const once = $('extraRepeat').value === 'once';
    $('extraWeekdayLabel').hidden = once;
    $('extraDateLabel').hidden = !once;
}

function describeExtra(x) {
    const when = x.repeat === 'once' ? longDate(x.date) : `co tydzień, ${DAY_NAMES[Number(x.weekday)].toLowerCase()}`;
    return `${when}, ${x.start}–${x.end}${x.room ? ', sala ' + x.room : ''}`;
}

function renderExtras() {
    const list = extrasFor(currentPlanId);
    const clashes = new Map();
    for (const e of allEvents) if (e._extra && e._clash.length) clashes.set(e._extra, (clashes.get(e._extra) || 0) + 1);
    $('extraList').replaceChildren(list.length ? h('div', { class: 'extra-list' }, list.map(x => h('div', { class: 'extra-item' },
        h('div', null, h('b', null, x.subject), h('small', null, describeExtra(x)),
            clashes.get(x.id) ? h('small', { class: 'clash' }, `Kolizje z planem: ${clashes.get(x.id)}`) : null),
        h('button', { type: 'button', class: 'link-btn', 'aria-label': `Usuń ${x.subject}`, onclick: () => removeExtra(x.id) }, 'Usuń'),
    ))) : h('p', { class: 'hint' }, 'Nie masz jeszcze własnych zajęć w tej grupie.'));
}

function openExtras() {
    if (!currentPlanId) return;
    const dlg = moreDialog('extrasModal', 'Moje zajęcia', 'Pokazują się w planie razem z zajęciami grupy');
    const body = $('extrasModalBody');
    if (!body.firstChild) {
        body.append(extrasForm(), h('div', { id: 'extraList' }),
            h('p', { class: 'hint tool-note' }, 'Własne zajęcia są zapisane tylko na tym urządzeniu. Nie trafiają do subskrypcji kalendarza.'));
    }
    $('extraError').hidden = true;
    syncExtraForm();
    if (!$('extraDate').value) $('extraDate').value = selectedDate || todayISO();
    renderExtras();
    showDialog(dlg);
}

function refreshExtras() {
    indexEvents(planEvents);
    render();
    renderExtras();
}

function removeExtra(id) {
    saveExtras(currentPlanId, extrasFor(currentPlanId).filter(x => x.id !== id));
    refreshExtras();
}

function addExtra(ev) {
    ev.preventDefault();
    const subject = $('extraName').value.trim();
    const start = $('extraStart').value;
    const end = $('extraEnd').value;
    const repeat = $('extraRepeat').value;
    const date = $('extraDate').value;
    let problem = '';
    if (!subject) problem = 'Podaj nazwę zajęć.';
    else if (!start || !end) problem = 'Podaj godzinę początku i końca.';
    else if (toMin(end) <= toMin(start)) problem = 'Koniec musi być później niż początek.';
    else if (repeat === 'once' && !date) problem = 'Wybierz datę.';
    const error = $('extraError');
    error.textContent = problem;
    error.hidden = !problem;
    if (problem) return;
    const list = extrasFor(currentPlanId);
    list.push({
        id: `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`,
        subject: subject.slice(0, 80), start, end, repeat,
        weekday: repeat === 'once' ? dayIndex(parseISO(date)) : Number($('extraWeekday').value),
        date: repeat === 'once' ? date : '',
        room: $('extraRoom').value.trim().slice(0, 40),
    });
    saveExtras(currentPlanId, list);
    $('extraName').value = '';
    $('extraRoom').value = '';
    refreshExtras();
    setNotice('Dodano własne zajęcia', false);
}

const hoursText = (mins) => {
    const hours = Math.round(mins / 6) / 10;
    return `${String(hours).replace('.', ',')} h`;
};

function planStats(events) {
    const today = todayISO();
    const subjects = new Map();
    const weekdays = Array(7).fill(0);
    const days = new Map();
    let total = 0, done = 0;
    for (const e of events) {
        const mins = toMin(e.end) - toMin(e.start);
        total += mins;
        if (e.date < today) done += mins;
        const s = subjects.get(e.subject) || { subject: e.subject, color: e.color, mins: 0, types: new Map() };
        s.mins += mins;
        s.types.set(typeLabel(e.type), (s.types.get(typeLabel(e.type)) || 0) + 1);
        subjects.set(e.subject, s);
        weekdays[dayIndex(parseISO(e.date))] += mins;
        const d = days.get(e.date) || { date: e.date, mins: 0, first: e.start, last: e.end };
        d.mins += mins;
        if (e.start < d.first) d.first = e.start;
        if (e.end > d.last) d.last = e.end;
        days.set(e.date, d);
    }
    const dayList = [...days.values()];
    const span = (d) => toMin(d.last) - toMin(d.first);
    return {
        total, done, count: events.length,
        subjects: [...subjects.values()].sort((a, b) => b.mins - a.mins),
        weekdays,
        days: dayList.length,
        busiest: dayList.reduce((a, d) => (!a || d.mins > a.mins ? d : a), null),
        longest: dayList.reduce((a, d) => (!a || span(d) > span(a) ? d : a), null),
        earliest: dayList.reduce((a, d) => (!a || d.first < a ? d.first : a), ''),
        latest: dayList.reduce((a, d) => (!a || d.last > a ? d.last : a), ''),
    };
}

const statTile = (value, label) => h('div', { class: 'stat' }, h('b', null, value), h('small', null, label));

function bar(label, mins, max, detail, color) {
    const row = h('div', { class: color ? 'stat-row tinted' : 'stat-row' },
        h('div', { class: 'stat-label' }, h('span', null, label), h('span', { class: 'sub' }, detail)),
        h('div', { class: 'stat-track' }, h('div', { class: 'stat-fill', style: `width:${max ? Math.max(2, Math.round(mins / max * 100)) : 0}%` })));
    return color ? tint(row, color) : row;
}

function openStats() {
    const events = allEvents.filter(e => !e._extra);
    if (!currentPlanId || !events.length) { setNotice('Najpierw wybierz grupę z planem', true); return; }
    const s = planStats(events);
    const dlg = moreDialog('statsModal', 'Statystyki planu', '');
    $('statsModalSub').textContent = `${currentPlanId}, semestr ${currentSemester() === 'zima' ? 'zimowy' : 'letni'}`;
    const pct = s.total ? Math.round(s.done / s.total * 100) : 0;
    const maxSubject = s.subjects[0] ? s.subjects[0].mins : 0;
    const maxDay = Math.max(...s.weekdays);
    const dayLabel = (d) => `${shortDate(d.date)}, ${d.first}–${d.last}`;
    $('statsModalBody').replaceChildren(
        h('div', { class: 'stat-grid' },
            statTile(hoursText(s.total), `${classesWord(s.count)} w semestrze`),
            statTile(`${pct}%`, `za tobą (${hoursText(s.done)})`),
            statTile(String(s.days), plural(s.days, 'dzień z zajęciami', 'dni z zajęciami', 'dni z zajęciami')),
            statTile(`${s.earliest}–${s.latest}`, 'najwcześniej i najpóźniej')),
        h('div', { class: 'stat-progress', role: 'img', 'aria-label': `Semestr za tobą w ${pct}%` }, h('div', { style: `width:${pct}%` })),
        h('h3', { class: 'stat-title' }, 'Przedmioty'),
        h('div', { class: 'stat-bars' }, s.subjects.map(x => bar(x.subject, x.mins, maxSubject,
            `${hoursText(x.mins)} · ${[...x.types].map(([t, n]) => `${t.toLowerCase()} ${n}`).join(', ')}`, x.color))),
        h('h3', { class: 'stat-title' }, 'Dni tygodnia'),
        h('div', { class: 'stat-bars' }, s.weekdays.map((mins, i) => (i < 5 || mins) ? bar(DAY_NAMES[i], mins, maxDay, hoursText(mins)) : null)),
        h('h3', { class: 'stat-title' }, 'Rekordy'),
        h('div', { class: 'stat-grid' },
            statTile(hoursText(s.busiest.mins), `najwięcej zajęć: ${dayLabel(s.busiest)}`),
            statTile(hoursText(toMin(s.longest.last) - toMin(s.longest.first)), `najdłuższy dzień na uczelni: ${dayLabel(s.longest)}`)),
        h('p', { class: 'hint tool-note' }, 'Bez ukrytych przedmiotów i własnych zajęć. Godziny zegarowe.'));
    showDialog(dlg);
}

function planLink() {
    return `${location.origin}/plan/${encodeURIComponent(currentPlanId)}`;
}

async function copyLink(url) {
    try {
        await navigator.clipboard.writeText(url);
        setNotice('Skopiowano link', false);
    } catch (_) {
        const input = $('shareLink');
        input.focus();
        input.select();
        setNotice('Zaznaczono link, skopiuj go ręcznie', false);
    }
}

function openShare() {
    if (!currentPlanId) return;
    const url = planLink();
    const title = `Plan zajęć ${currentPlanId} – WATplan`;
    const dlg = moreDialog('shareModal', 'Udostępnij plan', '');
    $('shareModalSub').textContent = currentPlanId;
    $('shareModalBody').replaceChildren(
        h('img', { class: 'share-qr', src: `/plan/${encodeURIComponent(currentPlanId)}/qr.svg`, width: '220', height: '220', alt: `Kod QR z linkiem do planu ${currentPlanId}` }),
        h('p', { class: 'hint share-hint' }, 'Zeskanuj aparatem telefonu albo wyślij link.'),
        h('input', { class: 'share-link', id: 'shareLink', readonly: true, value: url, 'aria-label': 'Link do planu', onfocus: (e) => e.target.select() }),
        h('div', { class: 'share-actions' },
            h('button', { class: 'primary-btn', type: 'button', onclick: () => copyLink(url) }, 'Kopiuj link'),
            navigator.share ? h('button', { class: 'link-btn', type: 'button', onclick: () => navigator.share({ title, url }).catch(() => undefined) }, 'Wyślij…') : null));
    showDialog(dlg);
}

function openInstall() {
    const dlg = moreDialog('installModal', 'Zainstaluj aplikację', 'WATplan na ekranie początkowym');
    $('installModalBody').replaceChildren(
        h('ol', { class: 'install-steps' },
            h('li', null, 'Otwórz watplan.me w Safari.'),
            h('li', null, 'Stuknij „Udostępnij” na dolnym pasku.'),
            h('li', null, 'Wybierz „Do ekranu początkowego” i potwierdź „Dodaj”.')),
        h('p', { class: 'hint tool-note' }, 'Plan otworzy się jak aplikacja, także bez internetu, z ostatnio zapisanymi danymi.'));
    showDialog(dlg);
}
