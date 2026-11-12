(function () {
    var theme = 'auto';
    try { theme = (JSON.parse(localStorage.getItem('settings') || '{}').theme) || localStorage.getItem('theme') || 'auto'; } catch (e) {}
    if (theme === 'auto') theme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    document.documentElement.setAttribute('data-theme', theme);
})();
