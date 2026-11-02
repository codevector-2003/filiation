// The Citation map: a paper at the centre, what it cites or what cites it
// around it. The place of each paper says nothing but "linked": rings, not a
// measured layout, so that a page of titles stays readable.
'use strict';

const NODE_W = 180;
const NODE_H = 76;

const mapState = { id: '', direction: 'cites', steps: '1', x: 0, y: 0, scale: 1, nodes: null, center: null, selected: '', asked: 0 };

screens.map = (id) => {
  if (id) mapState.id = id;
  else if (!mapState.id && state.selected) mapState.id = state.selected;
  loadMap();
};

async function loadMap() {
  const asked = ++mapState.asked;
  const q = new URLSearchParams({ id: mapState.id, direction: mapState.direction, steps: mapState.steps });
  try {
    const m = await api('/api/map?' + q);
    if (asked === mapState.asked) renderMap(m);
  } catch (e) {
    if (asked === mapState.asked) $('link-pane').replaceChildren(el('div', 'error', e.message));
  }
}

// place puts each paper on its ring; a second step fans out around its parent.
function place(nodes) {
  const at = {};
  const first = nodes.filter((n) => n.step === 1);
  const r1 = 240;
  // Every other paper sits further out, so that neighbours never overlap.
  const stagger = first.length > 8 ? 1.35 : 1;
  first.forEach((n, i) => {
    const a = (2 * Math.PI * i) / Math.max(1, first.length) - Math.PI / 2;
    const r = i % 2 ? r1 * stagger : r1;
    at[n.id] = { x: r * 1.15 * Math.cos(a), y: r * 0.8 * Math.sin(a), a };
  });
  const groups = {};
  nodes.filter((n) => n.step === 2).forEach((n) => { (groups[n.parent] = groups[n.parent] || []).push(n); });
  const r2 = r1 * stagger + 260;
  for (const [parent, kids] of Object.entries(groups)) {
    kids.forEach((n, i) => {
      const a = at[parent].a + (i - (kids.length - 1) / 2) * 0.17;
      at[n.id] = { x: r2 * Math.cos(a), y: r2 * 0.78 * Math.sin(a), a };
    });
  }
  return at;
}

function nodeButton(n, pos, center) {
  const b = el('button', 'node ' + (center ? 'centre' : n.pdf ? 'has-pdf' : 'no-pdf'));
  b.type = 'button';
  b.style.left = pos.x - NODE_W / 2 + 'px';
  b.style.top = pos.y - NODE_H / 2 + 'px';
  b.dataset.id = n.id;
  b.append(el('span', 'node-title', (center ? '★ ' : '') + n.title), el('span', 'node-by', n.by || (n.stub ? 'Not fetched yet' : '')));
  if (!center) b.addEventListener('click', () => selectLink(n));
  return b;
}

function line(from, to, id) {
  const l = document.createElementNS('http://www.w3.org/2000/svg', 'line');
  l.setAttribute('x1', from.x);
  l.setAttribute('y1', from.y);
  l.setAttribute('x2', to.x);
  l.setAttribute('y2', to.y);
  l.dataset.id = id;
  return l;
}

function renderMap(m) {
  const plane = $('map-plane');
  plane.querySelectorAll('.node').forEach((n) => n.remove());
  const lines = $('map-lines');
  lines.replaceChildren();
  mapState.nodes = m.nodes;
  mapState.center = m.center;
  mapState.selected = '';
  if (!m.center) {
    $('map-title').textContent = '';
    $('map-count').textContent = '';
    $('link-pane').replaceChildren(el('div', 'muted', 'Add a paper to see its map.'));
    return;
  }
  mapState.id = m.center.id;
  $('map-title').textContent = m.center.title;
  const at = place(m.nodes);
  at[m.center.id] = { x: 0, y: 0 };
  for (const n of m.nodes) lines.append(line(at[n.parent], at[n.id], n.id));
  for (const n of m.nodes) plane.append(nodeButton(n, at[n.id], false));
  plane.append(nodeButton(m.center, at[m.center.id], true));
  const shown = m.nodes.filter((n) => n.step === 1).length;
  $('map-empty').hidden = m.nodes.length > 0;
  $('map-empty').textContent = mapState.direction === 'cites' ? 'No references are known for this paper.' : 'Nothing in your library cites this paper.';
  $('map-count').textContent = 'Showing ' + shown + ' of ' + m.total + '. Scroll to zoom, drag to move.';
  fitMap(at);
  renderCentre(m);
}

// fitMap scales the map to the view, and centres it.
function fitMap(at) {
  const view = $('map-view').getBoundingClientRect();
  const pts = Object.values(at);
  const w = Math.max(...pts.map((p) => Math.abs(p.x))) * 2 + NODE_W + 40;
  const h = Math.max(...pts.map((p) => Math.abs(p.y))) * 2 + NODE_H + 80;
  // Never so small that titles cannot be read: past that, drag and zoom.
  mapState.scale = Math.max(0.7, Math.min(1, view.width / w, view.height / h));
  mapState.x = view.width / 2;
  mapState.y = view.height / 2;
  applyView();
}

function applyView() {
  $('map-plane').style.transform = 'translate(' + mapState.x + 'px,' + mapState.y + 'px) scale(' + mapState.scale + ')';
}

function renderCentre(m) {
  const box = $('link-pane');
  const parts = [el('h2', 'pane-head', 'THIS PAPER'), el('div', 'link-title', m.center.title)];
  if (m.center.by) parts.push(el('div', 'muted', m.center.by));
  parts.push(el('div', 'muted', 'Cites ' + m.cites + ' · cited by ' + m.citedBy + ' here'));
  if (m.cites === 0 && m.center.stub) parts.push(el('div', 'why', 'Not fetched yet, so its references are unknown.'));
  parts.push(pathFinder());
  box.replaceChildren(...parts);
}

async function selectLink(n) {
  mapState.selected = n.id;
  document.querySelectorAll('.node').forEach((b) => b.classList.toggle('picked', b.dataset.id === n.id));
  document.querySelectorAll('#map-lines line').forEach((l) => l.classList.toggle('picked', l.dataset.id === n.id));
  const cites = mapState.direction === 'cites';
  const parent = n.step === 1 ? mapState.center : mapState.nodes.find((o) => o.id === n.parent);
  const from = cites ? parent : n;
  const to = cites ? n : parent;

  const box = el('div', 'link-body');
  box.append(el('h2', 'pane-head', 'SELECTED LINK'), el('div', 'link-lead', short(from.title) + ' cites'),
    el('div', 'link-title', to.title), el('div', 'muted', [to.by, to.pdf ? 'PDF in your library' : ''].filter(Boolean).join(' · ')));
  const sentences = el('div', 'link-sentences');
  box.append(sentences);
  box.append(button('Show in Papers', 'btn primary wide', () => openInPapers(n.id)));
  box.append(button('Centre the map on it', 'btn wide', () => {
    mapState.id = n.id;
    loadMap();
  }));
  box.append(pathFinder());
  $('link-pane').replaceChildren(box);

  try {
    const q = new URLSearchParams({ from: from.id, to: to.id });
    const res = await api('/api/link?' + q);
    if (mapState.selected !== n.id) return;
    if (res.sentences.length === 0) {
      sentences.append(el('div', 'muted', 'No sentence found for this citation.'));
    }
    for (const s of res.sentences.slice(0, 3)) {
      const fig = el('figure');
      fig.append(el('blockquote', '', '“' + s.text + '”'), el('figcaption', '', 'The sentence it is cited in · page ' + s.page));
      sentences.append(fig);
    }
  } catch (e) {
    sentences.append(el('div', 'error', e.message));
  }
}

function short(title) {
  return title.length > 60 ? title.slice(0, 60) + '…' : title;
}

function openInPapers(id) {
  state.selected = id;
  state.title = '';
  $('filter').value = '';
  show('papers');
  select(id);
}

// pathFinder finds the chain of citations from the selected paper to another.
function pathFinder() {
  const box = el('form', 'path-form');
  const input = el('input');
  input.placeholder = 'Find a path to another paper';
  input.setAttribute('aria-label', 'Find a path to another paper: its title or DOI');
  const out = el('div', 'path-out');
  box.append(input, out);
  box.addEventListener('submit', async (e) => {
    e.preventDefault();
    const to = input.value.trim();
    if (!to) return;
    const from = mapState.selected || mapState.id;
    out.replaceChildren(el('div', 'muted', 'Looking…'));
    try {
      const res = await api('/api/path?' + new URLSearchParams({ from, to }));
      if (!res.found) {
        out.replaceChildren(el('div', 'muted', 'No chain of citations joins them in your library.'));
        return;
      }
      out.replaceChildren(...res.steps.map((s, i) => {
        const row = el('div', 'path-step');
        row.append(button(short(s.title), 'link', () => openInPapers(s.id)));
        if (i < res.steps.length - 1) row.append(el('div', 'muted', s.cites ? 'cites ↓' : 'cited by ↓'));
        return row;
      }));
    } catch (err) {
      out.replaceChildren(el('div', 'error', err.message));
    }
  });
  return box;
}

document.querySelectorAll('[data-direction]').forEach((b) => {
  b.addEventListener('click', () => {
    mapState.direction = b.dataset.direction;
    document.querySelectorAll('[data-direction]').forEach((o) => {
      o.classList.toggle('on', o === b);
      o.setAttribute('aria-pressed', o === b ? 'true' : 'false');
    });
    loadMap();
  });
});

$('steps').addEventListener('change', (e) => {
  mapState.steps = e.target.value;
  loadMap();
});

$('map-export').addEventListener('click', () => {
  api('/api/export', 'POST').catch((e) => { $('link-pane').append(el('div', 'error', e.message)); });
});

// Zoom about the pointer; drag the background to move.
$('map-view').addEventListener('wheel', (e) => {
  e.preventDefault();
  const view = $('map-view').getBoundingClientRect();
  const px = e.clientX - view.left;
  const py = e.clientY - view.top;
  const next = Math.min(2.5, Math.max(0.2, mapState.scale * (e.deltaY < 0 ? 1.1 : 0.9)));
  mapState.x = px - ((px - mapState.x) * next) / mapState.scale;
  mapState.y = py - ((py - mapState.y) * next) / mapState.scale;
  mapState.scale = next;
  applyView();
}, { passive: false });

$('map-view').addEventListener('pointerdown', (e) => {
  if (e.target.closest('.node, .legend')) return;
  const view = $('map-view');
  const start = { x: e.clientX - mapState.x, y: e.clientY - mapState.y };
  view.setPointerCapture(e.pointerId);
  const moveTo = (m) => {
    mapState.x = m.clientX - start.x;
    mapState.y = m.clientY - start.y;
    applyView();
  };
  const end = () => {
    view.removeEventListener('pointermove', moveTo);
    view.removeEventListener('pointerup', end);
  };
  view.addEventListener('pointermove', moveTo);
  view.addEventListener('pointerup', end);
});
