'use strict';

(() => {
    const appURL = document.body.dataset.app || '/';
    const openPlan = (plan) => {
        window.location.href = plan ? `${appURL}?plan=${encodeURIComponent(plan.id)}` : appURL;
    };

    const ready = WatGroups.load().catch(() => []);

    const favHref = (fav) => `${appURL}?plan=${encodeURIComponent(fav.id)}`;
    const renderFavs = () => {
        document.querySelectorAll('[data-favs]').forEach((box) => {
            box.hidden = WatGroups.renderFavorites(box.querySelector('.fav-list'), { href: favHref }) === 0;
        });
    };
    renderFavs();
    WatGroups.onFavoritesChange(renderFavs);

    document.querySelectorAll('.group-form').forEach((form) => {
        const input = form.querySelector('input');
        const results = form.querySelector('.group-results');
        const fill = form.dataset.select === 'fill';

        const announce = (plan) => form.dispatchEvent(new CustomEvent('watplan:group', { detail: plan, bubbles: true }));
        const choose = (plan) => {
            if (!fill) { openPlan(plan); return; }
            input.value = plan.id;
            results.hidden = true;
            announce(plan);
        };

        const show = async () => {
            await ready;
            if (!input.value.trim()) { results.hidden = true; return; }
            WatGroups.render(results, input.value, { onSelect: choose, limit: 40 });
            results.hidden = false;
        };

        input.addEventListener('input', async () => {
            await show();
            const exact = WatGroups.list().find(p => p.id.toLowerCase() === input.value.trim().toLowerCase());
            if (exact) announce(exact);
        });
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
    });

    ready.then(() => {
        const faculties = WatGroups.faculties();
        if (!faculties.length) return;
        document.querySelectorAll('[data-faculties]').forEach((node) => {
            node.textContent = `Teraz w aplikacji są plany wydziałów: ${faculties.join(', ')}. Część wydziałów bywa chwilowo niedostępna do odświeżenia; w aplikacji zawsze widać, kiedy plan pobrano.`;
        });
    });
})();
