'use strict';

const MAPLIBRE_JS = '/vendor/maplibre-gl.mjs?v=0';
const MAPLIBRE_CSS = '/vendor/maplibre-gl.css?v=0';
const MAP_CSS = '/map.css?v=0';
const CAMPUS_JSON = '/campus.json?v=0';
const MAP_STYLES = { light: 'https://tiles.openfreemap.org/styles/positron', dark: 'https://tiles.openfreemap.org/styles/dark' };
const MAP_COLORS = {
    light: { building: '#d9d6cf', target: '#1d2024' },
    dark: { building: '#2b2e33', target: '#ebe5d3' },
};

let campusPromise = null;
let maplibrePromise = null;
let campusMap = null;
let campusTheme = '';
let mapDialog = null;
let mapTarget = '';
let mapMarker = null;

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

function targetFeature(building) {
    return { type: 'Feature', properties: { height: (building.levels || 3) * 4 + 4 }, geometry: { type: 'Polygon', coordinates: [building.outline] } };
}

function placeMarker(maplibre, map, code, building) {
    if (mapMarker) mapMarker.remove();
    const label = h('div', { class: 'map-pin' }, code === 'S' ? '100' : code);
    mapMarker = new maplibre.Marker({ element: label, anchor: 'bottom', offset: [0, -24] }).setLngLat(building.center).addTo(map);
}

function addCampusLayers(map, building, theme) {
    const colors = MAP_COLORS[theme];
    const layers = map.getStyle().layers || [];
    const label = layers.find(l => l.type === 'symbol' && l.layout && l.layout['text-field']);
    if (map.getSource('openmaptiles')) {
        map.addLayer({
            id: 'wp-buildings', type: 'fill-extrusion', source: 'openmaptiles', 'source-layer': 'building', minzoom: 14,
            paint: {
                'fill-extrusion-color': colors.building,
                'fill-extrusion-height': ['coalesce', ['get', 'render_height'], 8],
                'fill-extrusion-base': ['coalesce', ['get', 'render_min_height'], 0],
                'fill-extrusion-opacity': 0.85,
            },
        }, label && label.id);
    }
    map.addSource('wp-target', { type: 'geojson', data: targetFeature(building) });
    map.addLayer({
        id: 'wp-target', type: 'fill-extrusion', source: 'wp-target',
        paint: { 'fill-extrusion-color': colors.target, 'fill-extrusion-height': ['get', 'height'], 'fill-extrusion-opacity': 0.95 },
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
    if (campusMap && campusTheme === theme && campusMap.getSource('wp-target')) {
        campusMap.getSource('wp-target').setData(targetFeature(building));
        placeMarker(await loadMaplibre(), campusMap, code, building);
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
        addCampusLayers(map, current, theme);
        placeMarker(maplibre, map, mapTarget, current);
        map.getContainer().querySelector('.maplibregl-compact-show')?.classList.remove('maplibregl-compact-show');
        flyToBuilding(map, current, false);
    });
    map.on('error', () => {
        if (!loaded && campusMap === map) {
            mapMessage(`Nie udało się wczytać mapy kampusu. ${building.name}: możesz użyć przycisku „Prowadź”.`);
        }
    });
}
