'use strict';

const WatGroups = (() => {
    const FAV_KEY = 'favorites';
    const STAR = '<svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" aria-hidden="true"><path d="m12 3 2.7 5.6 6.1.8-4.5 4.2 1.1 6.1L12 16.8l-5.4 2.9 1.1-6.1-4.5-4.2 6.1-.8z"/></svg>';
    let plans = [];
    let pending = null;
    const listeners = new Set();

    const facultyOf = (plan) => plan.faculty || plan.id.slice(0, 3).toUpperCase();
    const normalize = (query) => String(query || '').trim().toLowerCase().replace(/\s+/g, '');

    function load() {
        if (!pending) {
            pending = fetch('/api/plans')
                .then(async (response) => {
                    let body = null;
                    try { body = await response.json(); } catch (_) { body = null; }
                    if (!response.ok) throw new Error((body && body.error) || `Błąd serwera (${response.status})`);
                    plans = body || [];
                    return plans;
                })
                .catch((err) => { pending = null; throw err; });
        }
        return pending;
    }

    function filter(query) {
        const q = normalize(query);
        if (!q) return plans.slice();
        return plans.filter(p => p.name.toLowerCase().includes(q) || p.id.toLowerCase().includes(q));
    }

    function resolve(query) {
        const q = normalize(query);
        if (!q) return null;
        const exact = plans.find(p => p.id.toLowerCase() === q || p.name.toLowerCase() === q);
        if (exact) return exact;
        const matches = filter(q);
        return matches.length === 1 ? matches[0] : null;
    }

    function faculties() {
        return [...new Set(plans.map(facultyOf))].sort();
    }

    function favorites() {
        try {
            const list = JSON.parse(localStorage.getItem(FAV_KEY) || '[]');
            return Array.isArray(list) ? list.filter(f => f && typeof f.id === 'string') : [];
        } catch (_) {
            return [];
        }
    }

    function saveFavorites(list) {
        try { localStorage.setItem(FAV_KEY, JSON.stringify(list)); } catch (_) { }
        listeners.forEach(fn => fn(list));
    }

    const isFavorite = (id) => favorites().some(f => f.id === id);

    function toggleFavorite(plan) {
        const list = favorites();
        const next = list.some(f => f.id === plan.id)
            ? list.filter(f => f.id !== plan.id)
            : [...list, { id: plan.id, name: plan.name || plan.id }];
        saveFavorites(next);
        return next.some(f => f.id === plan.id);
    }

    const removeFavorite = (id) => saveFavorites(favorites().filter(f => f.id !== id));
    const onFavoritesChange = (fn) => listeners.add(fn);

    function starButton(plan) {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'fav-star';
        btn.innerHTML = STAR;
        const sync = () => {
            const on = isFavorite(plan.id);
            btn.setAttribute('aria-pressed', String(on));
            btn.setAttribute('aria-label', on ? `Usuń ${plan.name} z ulubionych` : `Dodaj ${plan.name} do ulubionych`);
            btn.title = on ? 'Usuń z ulubionych' : 'Dodaj do ulubionych';
        };
        sync();
        btn.addEventListener('click', (e) => { e.stopPropagation(); toggleFavorite(plan); sync(); });
        return btn;
    }

    function render(container, query, { currentId = '', onSelect, limit = Infinity } = {}) {
        const matches = filter(query);
        if (matches.length === 0) {
            const empty = document.createElement('div');
            empty.className = 'search-empty';
            empty.textContent = plans.length
                ? 'Nie znaleziono grupy. Sprawdź kod, np. WEL26EY4S0, albo wpisz jego początek.'
                : 'Ładowanie listy grup...';
            container.replaceChildren(empty);
            return 0;
        }
        const byFaculty = {};
        for (const plan of matches.slice(0, limit)) (byFaculty[facultyOf(plan)] ||= []).push(plan);
        const nodes = [];
        for (const faculty of Object.keys(byFaculty).sort()) {
            const title = document.createElement('div');
            title.className = 'search-group-title';
            title.textContent = faculty;
            nodes.push(title);
            for (const plan of byFaculty[faculty]) {
                const row = document.createElement('div');
                row.className = 'search-row';
                const item = document.createElement('button');
                item.type = 'button';
                item.className = 'search-result-item' + (plan.id === currentId ? ' current' : '');
                item.textContent = plan.name;
                item.addEventListener('click', () => onSelect && onSelect(plan));
                row.append(item, starButton(plan));
                nodes.push(row);
            }
        }
        container.replaceChildren(...nodes);
        return matches.length;
    }

    function renderFavorites(container, { currentId = '', href, onSelect, removable = false } = {}) {
        const list = favorites();
        container.hidden = list.length === 0;
        const nodes = list.map((fav) => {
            const chip = document.createElement(href ? 'a' : 'button');
            chip.className = 'fav-chip' + (fav.id === currentId ? ' current' : '');
            chip.textContent = fav.name;
            if (href) {
                chip.href = href(fav);
            } else {
                chip.type = 'button';
                chip.setAttribute('aria-pressed', String(fav.id === currentId));
                chip.addEventListener('click', () => onSelect && onSelect(fav));
            }
            if (!removable) return chip;
            const wrap = document.createElement('span');
            wrap.className = 'fav-item';
            const del = document.createElement('button');
            del.type = 'button';
            del.className = 'fav-remove';
            del.setAttribute('aria-label', `Usuń ${fav.name} z ulubionych`);
            del.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg>';
            del.addEventListener('click', () => removeFavorite(fav.id));
            wrap.append(chip, del);
            return wrap;
        });
        container.replaceChildren(...nodes);
        return list.length;
    }

    return {
        load, filter, resolve, render, faculties, facultyOf, list: () => plans.slice(),
        favorites, isFavorite, toggleFavorite, removeFavorite, onFavoritesChange, renderFavorites, starButton,
    };
})();
