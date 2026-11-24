'use strict';

const MAPLIBRE_JS = '/vendor/maplibre-gl.mjs?v=0';
const MAPLIBRE_CSS = '/vendor/maplibre-gl.css?v=0';
const MAP_CSS = '/map.css?v=0';
const CAMPUS_JSON = '/campus.json?v=0';
const MAP_STYLES = { light: 'https://tiles.openfreemap.org/styles/positron', dark: 'https://tiles.openfreemap.org/styles/dark' };
const FACULTY_COLORS = {
    WCY: '#f5a623', WEL: '#a24bdb', WIM: '#7ccf3a', WML: '#3fd7b4', WTC: '#3b8fe0', WIG: '#ea3f8a',
    IOE: '#f0727a', WLO: '#8a5a32', WCBKT: '#3a3d42', SJO: '#2f4a8a', AW: '#5f9a3c', SWF: '#e5484d',
};
const MAP_COLORS = {
    light: { other: '#cfcac0', target: '#1d2024' },
    dark: { other: '#5a5e66', target: '#ebe5d3' },
};

let campusPromise = null;
let maplibrePromise = null;
let campusMap = null;
let campusTheme = '';
let mapDialog = null;
let mapTarget = '';
let mapMarkers = [];

function mapTheme() {
    return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
}

function loadCampus() {
    campusPromise ||= fetchJSON(CAMPUS_JSON).catch((err) => { campusPromise = null; throw err; });
    return campusPromise;
}

function loadMaplibre() {
    if (!document.querySelector('link[data-maplibre]')) {
        document.head.append(h('link', { rel: 'stylesheet', href: MAPLIBRE_CSS, 'data-maplibre': true }));
    }
    maplibrePromise ||= import(MAPLIBRE_JS).catch((err) => { maplibrePromise = null; throw err; });
    return maplibrePromise;
}

function directionsURL(building) {
    const [lon, lat] = building.center;
    const apple = /iP(hone|ad|od)/.test(navigator.userAgent) || (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1);
    return apple
        ? `https://maps.apple.com/?daddr=${lat},${lon}&dirflg=w`
        : `https://www.google.com/maps/dir/?api=1&destination=${lat},${lon}&travelmode=walking`;
}

function ensureMapDialog() {
    if (mapDialog) return mapDialog;
    document.head.append(h('link', { rel: 'stylesheet', href: MAP_CSS }));
    mapDialog = h('dialog', { id: 'mapModal', class: 'fullscreen map-modal', 'aria-labelledby': 'mapTitle' },
        h('div', { class: 'series-head' },
            h('button', { class: 'round-btn', type: 'button', 'data-close': true, 'aria-label': 'Wróć' }, svg(ICON_LEFT, 20)),
            h('div', { class: 'series-title' }, h('h2', { id: 'mapTitle' }), h('p', { id: 'mapSub' }))),
        h('div', { class: 'map-frame', id: 'mapFrame' }, h('div', { id: 'campusMap', class: 'campus-map' })),
        h('div', { class: 'map-actions' },
            h('a', { id: 'mapGo', class: 'primary-btn', target: '_blank', rel: 'noopener' }, 'Prowadź'),
            h('p', { class: 'hint', id: 'mapNote' })));
    mapDialog.addEventListener('click', (e) => { if (e.target.closest('[data-close]')) mapDialog.close(); });
    document.body.append(mapDialog);
    return mapDialog;
}

function mapMessage(text) {
    $('mapFrame').replaceChildren(h('div', { class: 'map-message' }, text));
    campusMap = null;
}

function campusFeatures(campus, code, theme) {
    const target = campus.buildings[code];
    const seen = new Set();
    const features = [];
    for (const [key, b] of Object.entries(campus.buildings)) {
        const id = b.center.join(',');
        if (seen.has(id)) continue;
        seen.add(id);
        const selected = target && id === target.center.join(',');
        features.push({
            type: 'Feature',
            properties: {
                color: selected ? MAP_COLORS[theme].target : (FACULTY_COLORS[b.faculty] || MAP_COLORS[theme].other),
                height: (b.levels || 3) * 3.6,
                selected,
            },
            geometry: { type: 'Polygon', coordinates: [b.outline] },
        });
    }
    return { type: 'FeatureCollection', features };
}

function buildingLabels(campus) {
    const byCenter = new Map();
    for (const [code, b] of Object.entries(campus.buildings)) {
        const id = b.center.join(',');
        const shown = code === 'S' ? '100' : code === 'SWF' ? 'WF' : code;
        const current = byCenter.get(id);
        if (!current || shown.length < current.label.length) byCenter.set(id, { label: shown, center: b.center });
    }
    return [...byCenter.values()];
}

function placeLabels(maplibre, map, campus, code) {
    for (const m of mapMarkers) m.remove();
    mapMarkers = [];
    const target = campus.buildings[code];
    const targetId = target ? target.center.join(',') : '';
    for (const { label, center } of buildingLabels(campus)) {
        const selected = center.join(',') === targetId;
        const el = h('div', { class: selected ? 'map-pin sel' : 'map-pin' }, label);
        const marker = new maplibre.Marker({ element: el, anchor: 'bottom', offset: [0, selected ? -26 : -14] }).setLngLat(center).addTo(map);
        mapMarkers.push(marker);
    }
}

function addCampusLayers(map, campus, code, theme) {
    const layers = map.getStyle().layers || [];
    for (const l of layers) {
        if (l.type === 'fill-extrusion') map.removeLayer(l.id);
    }
    map.setLight({ anchor: 'map', position: [1.4, 200, 35], color: '#ffffff', intensity: theme === 'dark' ? 0.55 : 0.4 });
    map.addSource('wp-campus', { type: 'geojson', data: campusFeatures(campus, code, theme) });
    map.addLayer({
        id: 'wp-campus', type: 'fill-extrusion', source: 'wp-campus',
        paint: {
            'fill-extrusion-color': ['get', 'color'],
            'fill-extrusion-height': ['get', 'height'],
            'fill-extrusion-opacity': 0.92,
            'fill-extrusion-vertical-gradient': true,
        },
    });
}

function flyToBuilding(map, building, instant) {
    const camera = { center: building.center, zoom: 17.2, pitch: 58, bearing: -28 };
    if (instant) map.jumpTo(camera);
    else map.flyTo({ ...camera, duration: 1400, essential: true });
}

async function openCampusMap(code) {
    const dlg = ensureMapDialog();
    mapTarget = code;
    $('mapTitle').textContent = buildingLabel(code);
    $('mapSub').textContent = 'Kampus WAT';
    $('mapNote').textContent = '';
    if (!dlg.open) dlg.showModal();
    let campus;
    try {
        campus = await loadCampus();
    } catch (_) {
        mapMessage('Nie udało się wczytać mapy. Sprawdź połączenie i spróbuj ponownie.');
        return;
    }
    const building = campus.buildings[code];
    if (!building || mapTarget !== code) return;
    $('mapSub').textContent = building.name === buildingLabel(code) ? 'Kampus WAT' : building.name;
    $('mapGo').href = directionsURL(building);
    if (!navigator.onLine) {
        mapMessage(`Mapa kampusu potrzebuje internetu. ${building.name}: użyj przycisku „Prowadź”, gdy wróci połączenie.`);
        return;
    }
    const theme = mapTheme();
    if (campusMap && campusTheme === theme && campusMap.getSource('wp-campus')) {
        campusMap.getSource('wp-campus').setData(campusFeatures(campus, code, theme));
        placeLabels(await loadMaplibre(), campusMap, campus, code);
        flyToBuilding(campusMap, building, false);
        return;
    }
    if (campusMap) { campusMap.remove(); campusMap = null; }
    if (!$('campusMap')) $('mapFrame').replaceChildren(h('div', { id: 'campusMap', class: 'campus-map' }));
    let maplibre;
    try {
        maplibre = await loadMaplibre();
    } catch (_) {
        mapMessage('Nie udało się wczytać mapy. Sprawdź połączenie i spróbuj ponownie.');
        return;
    }
    if (mapTarget !== code) return;
    let map;
    try {
        map = new maplibre.Map({
            container: 'campusMap', style: MAP_STYLES[theme], center: campus.center, zoom: 15.6, pitch: 45, bearing: -28,
            maxBounds: campus.bounds, minZoom: 13.5, maxZoom: 18.6, maxPitch: 70,
            attributionControl: false,
        });
    } catch (_) {
        mapMessage(`Ta przeglądarka nie potrafi wyświetlić mapy 3D. ${building.name}: użyj przycisku „Prowadź”.`);
        return;
    }
    campusMap = map;
    campusTheme = theme;
    let loaded = false;
    map.addControl(new maplibre.AttributionControl({ compact: true, customAttribution: campus.attribution }));
    map.on('load', () => {
        loaded = true;
        const current = campus.buildings[mapTarget] || building;
        addCampusLayers(map, campus, mapTarget, theme);
        placeLabels(maplibre, map, campus, mapTarget);
        map.getContainer().querySelector('.maplibregl-compact-show')?.classList.remove('maplibregl-compact-show');
        flyToBuilding(map, current, false);
    });
    map.on('error', () => {
        if (!loaded && campusMap === map) {
            mapMessage(`Nie udało się wczytać mapy kampusu. ${building.name}: możesz użyć przycisku „Prowadź”.`);
        }
    });
}
