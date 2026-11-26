(() => {
    const input = document.getElementById('groupFind');
    const out = document.getElementById('findResults');
    if (!input || !out) return;
    const ids = [...new Set([...document.querySelectorAll('.group-links:not(#findResults) a')].map(a => a.textContent))];
    const norm = (s) => s.toUpperCase().replace(/[^A-Z0-9]/g, '');
    const link = (id) => '/plan/' + encodeURIComponent(id);
    let matches = [];
    const show = () => {
        const q = norm(input.value);
        matches = q ? ids.filter(id => norm(id).includes(q)).sort((a, b) => norm(b).startsWith(q) - norm(a).startsWith(q) || a.localeCompare(b)) : [];
        const items = matches.slice(0, 24).map(id => {
            const a = document.createElement('a');
            a.href = link(id);
            a.textContent = id;
            const li = document.createElement('li');
            li.append(a);
            return li;
        });
        if (q && !matches.length) {
            const li = document.createElement('li');
            li.className = 'find-empty';
            li.textContent = 'Nie ma grupy o takim kodzie.';
            items.push(li);
        } else if (matches.length > items.length) {
            const li = document.createElement('li');
            li.className = 'find-empty';
            li.textContent = `i ${matches.length - items.length} więcej, wpisz dłuższy fragment`;
            items.push(li);
        }
        out.replaceChildren(...items);
        out.hidden = !q;
    };
    input.addEventListener('input', show);
    input.form.addEventListener('submit', (e) => {
        e.preventDefault();
        show();
        const q = norm(input.value);
        const pick = matches.find(id => norm(id) === q) || matches[0];
        if (pick) location.href = link(pick);
    });
})();
