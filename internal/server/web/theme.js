(function () {
    var theme = 'auto';
    try { theme = (JSON.parse(localStorage.getItem('settings') || '{}').theme) || localStorage.getItem('theme') || 'auto'; } catch (e) {}
    if (theme === 'auto') theme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    document.documentElement.setAttribute('data-theme', theme);
    try {
        var stored = JSON.parse(localStorage.getItem('settings') || '{}') || {};
        var params = new URLSearchParams(location.search);
        var view = stored.v === 3 ? stored.view : 'auto';
        var fromUrl = { day: 'agenda', dzien: 'agenda', agenda: 'agenda', week: 'week', month: 'month' }[params.get('view')];
        if (fromUrl) view = fromUrl;
        if (view === 'auto' || ['agenda', 'week', 'month'].indexOf(view) < 0) view = window.matchMedia('(min-width: 900px)').matches ? 'week' : 'agenda';
        if (params.get('plan') || localStorage.getItem('selectedPlan')) {
            document.documentElement.setAttribute('data-loading', '');
            if (view === 'week') document.documentElement.setAttribute('data-wide', '');
        }
    } catch (e) {}
})();
