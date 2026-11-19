'use strict';

(() => {
    const appURL = document.body.dataset.app || '/';
    const planHref = (id) => `${appURL}?plan=${encodeURIComponent(id)}`;
    const openPlan = (plan) => {
        window.location.href = plan ? planHref(plan.id) : appURL;
    };
    const plural = (n, one, few, many) => {
        if (n === 1) return one;
        const t = n % 10, h = n % 100;
        return t >= 2 && t <= 4 && (h < 12 || h > 14) ? few : many;
    };

    const ready = WatGroups.load().catch(() => []);

    const favList = document.querySelector('[data-fav-list]');
    const favEmpty = document.querySelector('[data-fav-empty]');
    const favCount = document.querySelector('[data-fav-count]');
    const ARROW = '<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h14M13 6l6 6-6 6"/></svg>';
    const renderFavs = () => {
        if (!favList) return;
        const n = WatGroups.renderFavorites(favList, { href: (fav) => planHref(fav.id), removable: true });
        favList.querySelectorAll('a.fav-chip').forEach((a) => a.insertAdjacentHTML('beforeend', ARROW));
        favList.hidden = n === 0;
        if (favEmpty) favEmpty.hidden = n > 0;
        if (favCount) favCount.textContent = n ? String(n) : '';
    };
    renderFavs();
    WatGroups.onFavoritesChange(renderFavs);

    document.querySelectorAll('.group-form').forEach((form) => {
        const input = form.querySelector('input');
        const results = form.querySelector('.group-results');

        const show = async () => {
            await ready;
            if (!input.value.trim()) { results.hidden = true; return; }
            WatGroups.render(results, input.value, { onSelect: openPlan, limit: 40 });
            results.hidden = false;
        };

        input.addEventListener('input', show);
        input.addEventListener('focus', () => { if (input.value.trim()) show(); });
        input.addEventListener('keydown', (e) => { if (e.key === 'Escape') results.hidden = true; });
        document.addEventListener('click', (e) => { if (!form.contains(e.target)) results.hidden = true; });

        form.addEventListener('submit', async (e) => {
            e.preventDefault();
            if (!input.value.trim()) { openPlan(null); return; }
            await ready;
            const plan = WatGroups.resolve(input.value);
            if (plan) { openPlan(plan); return; }
            await show();
            input.focus();
        });

        if (input.dataset.phLong && 'ResizeObserver' in window) {
            const ctx = document.createElement('canvas').getContext('2d');
            const fit = () => {
                const cs = getComputedStyle(input);
                const ps = getComputedStyle(input, '::placeholder');
                ctx.font = `${ps.fontWeight || cs.fontWeight} ${ps.fontSize || cs.fontSize} ${ps.fontFamily || cs.fontFamily}`;
                const room = input.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight) - 8;
                input.placeholder = ctx.measureText(input.dataset.phLong).width <= room ? input.dataset.phLong : input.dataset.phShort;
            };
            new ResizeObserver(fit).observe(input);
            if (document.fonts) document.fonts.ready.then(fit);
        }
    });

    const semPick = document.querySelector('[data-sem-pick]');
    if (semPick) {
        const readSettings = () => { try { return JSON.parse(localStorage.getItem('settings') || '{}') || {}; } catch (_) { return {}; } };
        const summerStart = (year) => { const d = new Date(year, 1, 22); d.setDate(d.getDate() - ((d.getDay() + 6) % 7)); return d; };
        const today = new Date();
        const fallback = today.getMonth() >= 7 || today < summerStart(today.getFullYear()) ? 'zima' : 'lato';
        const current = () => { const v = readSettings().semester; return v === 'zima' || v === 'lato' ? v : fallback; };
        const sync = () => semPick.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.semester === current())));
        semPick.addEventListener('click', (e) => {
            const btn = e.target.closest('button[data-semester]');
            if (!btn) return;
            const settings = readSettings();
            settings.semester = btn.dataset.semester === fallback ? 'auto' : btn.dataset.semester;
            try { localStorage.setItem('settings', JSON.stringify(settings)); } catch (_) { }
            sync();
        });
        sync();
    }

    const status = document.querySelector('[data-refresh-status]');
    const refresh = document.querySelector('[data-refresh]');
    const groupsWord = (n) => `${n} ${plural(n, 'grupa', 'grupy', 'grup')}`;
    const clock = (d) => `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
    if (status) ready.then((plans) => { if (plans.length) status.textContent = groupsWord(plans.length); });
    if (refresh && status) {
        refresh.addEventListener('click', async () => {
            if (refresh.disabled) return;
            refresh.disabled = true;
            refresh.classList.add('spin');
            status.textContent = 'Odświeżam…';
            try {
                const res = await fetch('/api/plans', { cache: 'no-store' });
                if (!res.ok) throw new Error(String(res.status));
                const plans = await res.json();
                status.textContent = `${groupsWord(plans.length)} · ${clock(new Date())}`;
            } catch (_) {
                status.textContent = 'Nie udało się odświeżyć';
            }
            setTimeout(() => { refresh.disabled = false; refresh.classList.remove('spin'); }, 600);
        });
    }

    const sem = document.querySelector('[data-sem]');
    if (sem) {
        const DAY = 86400000;
        const parse = (v) => { const [y, m, d] = v.split('-').map(Number); return new Date(y, m - 1, d); };
        const short = (v) => { const [, m, d] = v.split('-'); return `${d}.${m}`; };
        const out = sem.querySelector('[data-sem-pct]');
        const fill = sem.querySelector('[data-sem-fill]');
        const dates = sem.querySelectorAll('.sem-dates span');
        let frame = 0;
        const show = (info, animate) => {
            const today = new Date();
            const start = parse(info.start);
            const span = (parse(info.end) - start) / DAY + 1;
            const weeks = info.weeks.map(parse);
            const total = weeks.length;
            const week = Math.min(Math.max(weeks.filter((w) => w <= today).length, 1), total);
            const pct = Math.round(Math.min(Math.max((today - start) / DAY, 0), span) / span * 100);
            const left = total - week;
            if (info.name) sem.querySelector('#sem-h').textContent = `${info.name} ${info.years}`;
            if (dates.length === 3) { dates[0].textContent = short(info.start); dates[2].textContent = short(info.end); }
            sem.querySelector('[data-sem-weeks]').textContent = today < start ? `${total} tyg.` : `${week} / ${total} tyg.`;
            sem.querySelector('[data-sem-left]').textContent = today < start ? `start ${short(info.start)}`
                : pct >= 100 ? 'zajęcia zakończone'
                    : left > 0 ? `${plural(left, 'został', 'zostały', 'zostało')} ${left} ${plural(left, 'tydzień', 'tygodnie', 'tygodni')}` : 'ostatni tydzień';
            cancelAnimationFrame(frame);
            if (!animate || window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
                out.textContent = String(pct);
                fill.style.width = `${pct}%`;
                return;
            }
            const t0 = performance.now() + 250;
            const step = (t) => {
                const k = Math.min(Math.max((t - t0) / 1100, 0), 1);
                const e = 1 - Math.pow(1 - k, 3);
                out.textContent = String(Math.round(pct * e));
                fill.style.width = `${pct * e}%`;
                if (k < 1) frame = requestAnimationFrame(step);
            };
            frame = requestAnimationFrame(step);
        };
        show({ start: sem.dataset.start, end: sem.dataset.end, weeks: sem.dataset.weeks.split(',') }, true);
        fetch('/api/semester')
            .then((res) => (res.ok ? res.json() : null))
            .then((info) => { if (info && info.weeks && info.weeks.length) show(info, false); })
            .catch(() => undefined);
    }
})();

if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => navigator.serviceWorker.register('/sw.js').catch(() => undefined));
}
