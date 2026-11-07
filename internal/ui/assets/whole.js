// The whole library on one screen. Thousands of papers are too many for page
// elements, so this one is drawn on a canvas; a paper's title still only ever
// reaches the page through textContent or the canvas's fillText.
'use strict';

const DOT = { seed: '#8A5300', pdf: '#1F5F8B', titled: '#83878E', unnamed: '#C4C7CB' };
// The eight largest citation groups' colours, told apart by lightness as well
// as hue, and the colour of every paper in none of them (D34).
const GROUP_COLOURS = ['#2F6FA3', '#C8702A', '#2A8C82', '#7A5AA6', '#A8445E', '#5E7F1F', '#9A7B12', '#18324D'];
const NO_GROUP = '#C9C6BE';

// The map's own filters, kept per computer. size: what a circle's size says;
// min: the fewest citations, by that measure, a paper needs to stay bright;
// fade or hide what is left out; names: how many of the biggest are named;
// colour: by PDF and seeds, or by citation group; only: one group's anchor.
const MAP_DEFAULTS = { size: 'here', min: 0, from: 0, to: 0, pdf: true, nopdf: true, mine: false, stubs: true, hide: false, names: 12,
  colour: 'state', only: '' };
const mf = { ...MAP_DEFAULTS, mineSet: new Set() };
const STEPS = [0, 1, 2, 3, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 20000, 50000];

const whole = {
  nodes: [], edges: [], key: '', xy: null, degree: null, around: null, byDegree: [], byValue: [],
  all: false, shown: [], match: null, picked: -1, near: null, hover: -1,
  scale: 1, x: 0, y: 0, asked: 0, worker: null, queued: false,
  groupOf: null, groups: [], groupsKey: '',
};

function foldText(s) {
  return s.normalize('NFD').replace(/\p{Mn}/gu, '').toLowerCase();
}

async function loadWhole() {
  const asked = ++whole.asked;
  const note = $('whole-note');
  let g;
  try {
    g = await api('/api/graph');
  } catch (e) {
    if (asked === whole.asked) {
      note.hidden = false;
      note.textContent = e.message;
    }
    return;
  }
  if (asked !== whole.asked) return;
  const key = g.nodes.length + '/' + g.edges.length + '/' + (g.nodes.length ? g.nodes[g.nodes.length - 1].id : '');
  whole.nodes = g.nodes;
  whole.edges = g.edges;
  if (g.nodes.length === 0) {
    whole.key = key;
    whole.xy = null;
    whole.around = null;
    whole.picked = -1;
    note.hidden = false;
    note.textContent = 'Add a paper to see your library here.';
    $('whole-count').textContent = '';
    wholePane();
    drawWhole();
    return;
  }
  indexGraph();
  if (key !== whole.key || !whole.xy) {
    whole.xy = null;
    whole.picked = -1; // positions in the list have moved
    note.hidden = false;
    note.textContent = 'Arranging ' + g.nodes.length.toLocaleString() + ' papers';
    try {
      whole.xy = await arrange(g, asked);
    } catch (e) {
      if (asked === whole.asked) note.textContent = 'The map could not be arranged: ' + e.message;
      return;
    }
    if (asked !== whole.asked) return;
    whole.key = key;
  }
  note.hidden = true;
  await readFilters(asked);
  if (asked !== whole.asked) return;
  await loadMapGroups();
  if (asked !== whole.asked) return;
  setShown();
  fitWhole();
  if (whole.picked >= 0) pickPaper(whole.picked);
  else wholePane();
}

function arrange(g, asked) {
  return new Promise((resolve, reject) => {
    if (whole.worker) whole.worker.terminate();
    const w = new Worker('d3force/layout.js');
    whole.worker = w;
    w.onerror = (e) => {
      w.terminate();
      reject(new Error(e.message || 'the layout stopped'));
    };
    w.onmessage = ({ data }) => {
      if (data.xy) {
        w.terminate();
        resolve(data.xy);
      } else if (asked === whole.asked) {
        $('whole-note').textContent = 'Arranging ' + g.nodes.length.toLocaleString() + ' papers: ' + Math.round(data.progress * 100) + '%';
      }
    };
    w.postMessage({ count: g.nodes.length, edges: g.edges });
  });
}

function indexGraph() {
  const n = whole.nodes.length;
  whole.degree = new Int32Array(n);
  whole.around = Array.from({ length: n }, () => ({ cites: [], citedBy: [] }));
  whole.edges.forEach(([a, b]) => {
    whole.degree[a]++;
    whole.degree[b]++;
    whole.around[a].cites.push(b);
    whole.around[b].citedBy.push(a);
  });
  whole.byDegree = Array.from({ length: n }, (_, i) => i).sort((a, b) => whole.degree[b] - whole.degree[a]);
  // Your papers and what they link to, for Only my papers.
  mf.mineSet = new Set();
  whole.nodes.forEach((p, i) => {
    if (!p.seed) return;
    mf.mineSet.add(i);
    whole.around[i].cites.forEach((j) => mf.mineSet.add(j));
    whole.around[i].citedBy.forEach((j) => mf.mineSet.add(j));
  });
  sortByValue();
}

// measure is what a paper's size says: citations here, or worldwide.
function measure(i) {
  const n = whole.nodes[i];
  return mf.size === 'world' ? n.world || 0 : n.cited;
}

function sortByValue() {
  whole.byValue = mf.size === 'same' ? whole.byDegree
    : Array.from({ length: whole.nodes.length }, (_, i) => i).sort((a, b) => measure(b) - measure(a));
}

// passes says whether a paper meets the map's filters.
function passes(i) {
  const n = whole.nodes[i];
  if (mf.min > 0 && measure(i) < mf.min) return false;
  if ((mf.from || mf.to) && (!n.year || (mf.from && n.year < mf.from) || (mf.to && n.year > mf.to))) return false;
  if (n.pdf ? !mf.pdf : !mf.nopdf) return false;
  if (!mf.stubs && !n.title) return false;
  if (mf.mine && !mf.mineSet.has(i)) return false;
  const only = onlyGroup();
  if (only >= 0 && whole.groupOf[i] !== only) return false;
  return true;
}

function mapFiltersOn() {
  return [mf.min > 0, mf.from > 0 || mf.to > 0, !mf.pdf || !mf.nopdf, mf.mine, !mf.stubs, onlyGroup() >= 0].filter(Boolean).length;
}

// loadMapGroups reads the citation groups when the map is coloured by them,
// once for each state of the library and detail.
async function loadMapGroups() {
  renderWholeLegend();
  if (mf.colour !== 'group' || whole.nodes.length === 0) return;
  const key = whole.key + '/' + groupDetail();
  if (key === whole.groupsKey && whole.groupOf) return;
  let res;
  try {
    res = await api('/api/groups?members=1&detail=' + groupDetail());
  } catch (e) {
    // Drawn without groups; asked again when the library changes.
    whole.groupOf = null;
    whole.groups = [];
    whole.groupsKey = key;
    return;
  }
  const at = new Map(whole.nodes.map((n, i) => [n.id, i]));
  whole.groupOf = new Int8Array(whole.nodes.length).fill(-1);
  whole.groups = res.groups.filter((g) => !g.dismissed).slice(0, GROUP_COLOURS.length).map((g, k) => {
    g.members.forEach((id) => {
      const i = at.get(id);
      if (i !== undefined) whole.groupOf[i] = k;
    });
    return { anchor: g.anchor, name: g.name, papers: g.papers, colour: GROUP_COLOURS[k] };
  });
  whole.groupsKey = key;
  renderWholeLegend();
  renderMapFilters();
}

// onlyGroup is the place of the one group shown, or -1 for all of them.
function onlyGroup() {
  if (mf.colour !== 'group' || !mf.only || !whole.groupOf) return -1;
  return whole.groups.findIndex((g) => g.anchor === mf.only);
}

function renderWholeLegend() {
  const box = $('whole-legend');
  const key = (cls, text) => {
    const s = el('span');
    s.append(el('i', 'key dot ' + cls), text);
    return s;
  };
  if (mf.colour === 'group') {
    const parts = whole.groups.map((g) => {
      const s = el('span');
      const dot = el('i', 'key dot');
      dot.style.background = g.colour;
      s.append(dot, g.name);
      return s;
    });
    const rest = el('span');
    const dot = el('i', 'key dot');
    dot.style.background = NO_GROUP;
    rest.append(dot, whole.groups.length ? 'Other groups, or none' : 'Finding groups…');
    box.replaceChildren(...parts, rest);
    return;
  }
  box.replaceChildren(key('seed', 'Added by you'), key('pdf', 'PDF in your library'), key('titled', 'No PDF'), key('unnamed', 'Title not fetched'));
}

function papersFiltered() {
  return state.show !== 'all' || state.title !== '' || filtersOn();
}

// readFilters brightens what the Papers list is filtered to, and fades the rest.
async function readFilters(asked) {
  whole.match = null;
  $('whole-filter').hidden = true;
  if (!papersFiltered()) return;
  const res = await api('/api/papers/ids?' + listQuery(0)).catch(() => null);
  if (!res || asked !== whole.asked) return;
  const at = new Map(whole.nodes.map((n, i) => [n.id, i]));
  whole.match = new Set(res.ids.map((id) => at.get(id)).filter((i) => i !== undefined));
  $('whole-filter-text').textContent = 'Your Papers filters: ' + whole.match.size.toLocaleString() + ' bright';
  $('whole-filter').hidden = false;
}

function setShown() {
  whole.shown = whole.nodes.map((n, i) => (whole.all || whole.degree[i] >= 2 || n.seed || (whole.match !== null && whole.match.has(i)))
    && (!mf.hide || passes(i)));
  let count = 0;
  whole.shown.forEach((on, i) => { if (on && passes(i)) count++; });
  const text = 'Showing ' + count.toLocaleString() + ' of ' + whole.nodes.length.toLocaleString();
  $('whole-count').textContent = text;
  if ($('mf-count')) $('mf-count').textContent = text + ' papers';
  const n = mapFiltersOn();
  $('whole-filters').textContent = n ? 'Filters (' + n + ')' : 'Filters';
  $('whole-filters').classList.toggle('on', n > 0 || !$('mf-panel').hidden);
}

function fitWhole() {
  const box = $('whole-view').getBoundingClientRect();
  if (!whole.xy || box.width === 0) return;
  let left = Infinity;
  let right = -Infinity;
  let top = Infinity;
  let bottom = -Infinity;
  whole.shown.forEach((on, i) => {
    if (!on) return;
    left = Math.min(left, whole.xy[2 * i]);
    right = Math.max(right, whole.xy[2 * i]);
    top = Math.min(top, whole.xy[2 * i + 1]);
    bottom = Math.max(bottom, whole.xy[2 * i + 1]);
  });
  if (left === Infinity) return;
  whole.scale = Math.min(2, (box.width - 80) / Math.max(1, right - left), (box.height - 120) / Math.max(1, bottom - top));
  whole.x = box.width / 2 - (whole.scale * (left + right)) / 2;
  whole.y = (box.height - 40) / 2 - (whole.scale * (top + bottom)) / 2;
  drawWhole();
}

function dotSize(i) {
  const zoom = Math.max(0.8, Math.min(1.8, Math.sqrt(whole.scale)));
  if (mf.size === 'same') return 3.5 * zoom;
  const r = mf.size === 'world' ? 2 + Math.sqrt(measure(i)) * 0.22 : 2 + Math.sqrt(measure(i)) * 1.1;
  return Math.min(22, r) * zoom;
}

function dotColour(n, i) {
  if (mf.colour === 'group' && whole.groupOf) {
    const k = whole.groupOf[i];
    return k >= 0 ? whole.groups[k].colour : NO_GROUP;
  }
  if (n.seed) return DOT.seed;
  if (n.pdf) return DOT.pdf;
  return n.title ? DOT.titled : DOT.unnamed;
}

function bright(i) {
  if (whole.picked >= 0) return i === whole.picked || whole.near.has(i);
  return (whole.match === null || whole.match.has(i)) && passes(i);
}

function drawWhole() {
  if (whole.queued) return;
  whole.queued = true;
  requestAnimationFrame(() => {
    whole.queued = false;
    paintWhole();
  });
}

function dots(ctx, w, h, wanted, alpha) {
  const groups = {};
  whole.shown.forEach((on, i) => {
    if (!on || bright(i) !== wanted) return;
    const x = whole.xy[2 * i] * whole.scale + whole.x;
    const y = whole.xy[2 * i + 1] * whole.scale + whole.y;
    if (x < -20 || y < -20 || x > w + 20 || y > h + 20) return;
    const c = dotColour(whole.nodes[i], i);
    (groups[c] = groups[c] || new Path2D()).moveTo(x + dotSize(i), y);
    groups[c].arc(x, y, dotSize(i), 0, 2 * Math.PI);
  });
  ctx.globalAlpha = alpha;
  for (const [colour, path] of Object.entries(groups)) {
    ctx.fillStyle = colour;
    ctx.fill(path);
  }
  ctx.globalAlpha = 1;
}

function strokeLinks(ctx, colour, width, only) {
  ctx.beginPath();
  whole.edges.forEach(([a, b]) => {
    if (!whole.shown[a] || !whole.shown[b] || !only(a, b)) return;
    ctx.moveTo(whole.xy[2 * a] * whole.scale + whole.x, whole.xy[2 * a + 1] * whole.scale + whole.y);
    ctx.lineTo(whole.xy[2 * b] * whole.scale + whole.x, whole.xy[2 * b + 1] * whole.scale + whole.y);
  });
  ctx.strokeStyle = colour;
  ctx.lineWidth = width;
  ctx.stroke();
}

function labels(ctx, w, h) {
  ctx.font = '12px Inter, sans-serif';
  ctx.textBaseline = 'middle';
  ctx.lineJoin = 'round';
  const taken = [];
  const first = whole.picked >= 0 ? [whole.picked, ...whole.byValue] : whole.byValue;
  let left = mf.names + (whole.picked >= 0 ? 1 : 0);
  for (const i of first) {
    if (left === 0) break;
    const n = whole.nodes[i];
    if (!whole.shown[i] || !n.title || !bright(i)) continue;
    const x = whole.xy[2 * i] * whole.scale + whole.x;
    const y = whole.xy[2 * i + 1] * whole.scale + whole.y;
    if (x < 0 || y < 0 || x > w || y > h) continue;
    const text = n.title.length > 36 ? n.title.slice(0, 36) + '…' : n.title;
    const box = { x: x + dotSize(i) + 3, y: y - 8, w: ctx.measureText(text).width, h: 16 };
    if (taken.some((t) => box.x < t.x + t.w + 6 && t.x < box.x + box.w + 6 && box.y < t.y + t.h && t.y < box.y + box.h)) continue;
    taken.push(box);
    left--;
    ctx.lineWidth = 3;
    ctx.strokeStyle = 'rgba(243, 242, 238, 0.9)';
    ctx.strokeText(text, box.x, y);
    ctx.fillStyle = '#1D1F23';
    ctx.fillText(text, box.x, y);
  }
}

function paintWhole() {
  const canvas = $('whole-canvas');
  const box = $('whole-view').getBoundingClientRect();
  if (box.width === 0) return;
  const ratio = window.devicePixelRatio || 1;
  const pw = Math.round(box.width * ratio);
  const ph = Math.round(box.height * ratio);
  if (canvas.width !== pw || canvas.height !== ph) {
    canvas.width = pw;
    canvas.height = ph;
  }
  const ctx = canvas.getContext('2d');
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  ctx.clearRect(0, 0, box.width, box.height);
  if (!whole.xy) return;
  strokeLinks(ctx, 'rgba(90, 90, 85, 0.14)', 0.7, () => true);
  dots(ctx, box.width, box.height, false, 0.18);
  if (whole.picked >= 0) strokeLinks(ctx, '#1F5F8B', 1.4, (a, b) => a === whole.picked || b === whole.picked);
  dots(ctx, box.width, box.height, true, 1);
  if (whole.picked >= 0 || whole.hover >= 0) {
    const i = whole.picked >= 0 ? whole.picked : whole.hover;
    ctx.beginPath();
    ctx.arc(whole.xy[2 * i] * whole.scale + whole.x, whole.xy[2 * i + 1] * whole.scale + whole.y, dotSize(i) + 3, 0, 2 * Math.PI);
    ctx.strokeStyle = '#1D1F23';
    ctx.lineWidth = 1.5;
    ctx.stroke();
  }
  labels(ctx, box.width, box.height);
}

function paperAt(mx, my) {
  let best = -1;
  let gap = Infinity;
  whole.shown.forEach((on, i) => {
    if (!on) return;
    const d = Math.hypot(whole.xy[2 * i] * whole.scale + whole.x - mx, whole.xy[2 * i + 1] * whole.scale + whole.y - my);
    if (d <= dotSize(i) + 4 && d < gap) {
      best = i;
      gap = d;
    }
  });
  return best;
}

function wholePane() {
  const known = whole.nodes.filter((n) => n.refs).length;
  const parts = [el('h2', 'pane-head', 'WHOLE LIBRARY')];
  if (whole.nodes.length > 0) {
    parts.push(el('div', 'link-lead', whole.nodes.length.toLocaleString() + ' papers and ' + whole.edges.length.toLocaleString() + ' citations.'));
    parts.push(el('div', 'muted', 'Click a paper to see its links. ' + { here: 'Bigger means cited by more papers here.',
      world: 'Bigger means cited more worldwide, by the count OpenAlex keeps.', same: 'Every paper is drawn the same size.' }[mf.size]));
    if (known < whole.nodes.length) {
      parts.push(el('div', 'why', 'Reference lists are known for ' + known.toLocaleString() + ' of these, so citations between the others are not drawn yet.'));
    }
  }
  $('link-pane').replaceChildren(...parts);
}

function pickPaper(i) {
  whole.picked = i;
  whole.hover = -1;
  $('whole-tip').hidden = true;
  const n = whole.nodes[i];
  const links = whole.around[i];
  whole.near = new Set([...links.cites, ...links.citedBy]);
  mapState.selected = n.id;
  const box = el('div', 'link-body');
  box.append(el('h2', 'pane-head', 'SELECTED PAPER'), el('div', 'link-title', n.title || 'Title not fetched yet'));
  if (n.by) box.append(el('div', 'muted', n.by));
  box.append(el('div', 'muted', 'Cites ' + links.cites.length + ' · cited by ' + links.citedBy.length + ' here'));
  if (n.stub) box.append(el('div', 'why', 'Not fetched yet, so its references are unknown.'));
  box.append(button('Show in Papers', 'btn primary wide', () => openInPapers(n.id)));
  box.append(button('Open map around this paper', 'btn wide', () => show('map', n.id)));
  box.append(pathFinder());
  $('link-pane').replaceChildren(box);
  drawWhole();
}

function dropPick() {
  whole.picked = -1;
  whole.near = null;
  mapState.selected = '';
  wholePane();
  drawWhole();
}

function goToPaper(i) {
  if (!whole.shown[i]) {
    whole.all = true;
    $('whole-all').checked = true;
    setShown();
  }
  const box = $('whole-view').getBoundingClientRect();
  whole.scale = Math.max(whole.scale, 1.2);
  whole.x = box.width / 2 - whole.xy[2 * i] * whole.scale;
  whole.y = box.height / 2 - whole.xy[2 * i + 1] * whole.scale;
  $('whole-found').replaceChildren();
  $('whole-found').hidden = true;
  pickPaper(i);
}

function findPapers(text) {
  const words = foldText(text).split(/\s+/).filter(Boolean);
  const box = $('whole-found');
  if (words.length === 0) {
    box.replaceChildren();
    box.hidden = true;
    return [];
  }
  const found = whole.byDegree.filter((i) => {
    const title = foldText(whole.nodes[i].title);
    return words.every((w) => title.includes(w));
  }).slice(0, 6);
  box.replaceChildren(...(found.length ? found.map((i) => button(whole.nodes[i].title, 'found', () => goToPaper(i)))
    : [el('div', 'muted', 'No paper with that title.')]));
  box.hidden = false;
  return found;
}

$('whole-find').addEventListener('input', (e) => { findPapers(e.target.value); });
$('whole-find').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    e.preventDefault();
    const found = findPapers(e.target.value);
    if (found.length > 0) goToPaper(found[0]);
  } else if (e.key === 'Escape') {
    e.target.value = '';
    findPapers('');
  }
});

$('whole-all').addEventListener('change', (e) => {
  whole.all = e.target.checked;
  if (!whole.xy) return;
  setShown();
  if (whole.picked >= 0 && !whole.shown[whole.picked]) dropPick();
  fitWhole();
});

$('whole-filter-off').addEventListener('click', () => {
  whole.match = null;
  $('whole-filter').hidden = true;
  setShown();
  drawWhole();
});

const canvasPoint = (e) => {
  const box = $('whole-view').getBoundingClientRect();
  return { x: e.clientX - box.left, y: e.clientY - box.top };
};

$('whole-canvas').addEventListener('wheel', (e) => {
  e.preventDefault();
  const p = canvasPoint(e);
  const next = Math.min(12, Math.max(0.02, whole.scale * (e.deltaY < 0 ? 1.15 : 0.87)));
  whole.x = p.x - ((p.x - whole.x) * next) / whole.scale;
  whole.y = p.y - ((p.y - whole.y) * next) / whole.scale;
  whole.scale = next;
  drawWhole();
}, { passive: false });

$('whole-canvas').addEventListener('pointerdown', (e) => {
  if (!whole.xy) return;
  const c = $('whole-canvas');
  const start = { x: e.clientX, y: e.clientY, ox: whole.x, oy: whole.y };
  let dragged = false;
  c.setPointerCapture(e.pointerId);
  const moveTo = (m) => {
    dragged = dragged || Math.hypot(m.clientX - start.x, m.clientY - start.y) > 4;
    if (!dragged) return;
    whole.x = start.ox + m.clientX - start.x;
    whole.y = start.oy + m.clientY - start.y;
    drawWhole();
  };
  const end = (u) => {
    c.removeEventListener('pointermove', moveTo);
    c.removeEventListener('pointerup', end);
    if (dragged) return;
    const p = canvasPoint(u);
    const i = paperAt(p.x, p.y);
    if (i >= 0) pickPaper(i);
    else if (whole.picked >= 0) dropPick();
  };
  c.addEventListener('pointermove', moveTo);
  c.addEventListener('pointerup', end);
});

$('whole-canvas').addEventListener('pointermove', (e) => {
  if (!whole.xy || e.buttons !== 0) return;
  const p = canvasPoint(e);
  const i = paperAt(p.x, p.y);
  if (i === whole.hover) return;
  whole.hover = i;
  const tip = $('whole-tip');
  tip.hidden = i < 0;
  $('whole-canvas').style.cursor = i < 0 ? '' : 'pointer';
  if (i >= 0) {
    const n = whole.nodes[i];
    tip.textContent = (n.title || 'Title not fetched yet') + (n.by ? ' · ' + n.by : '');
    tip.style.left = Math.min(p.x + 12, $('whole-view').clientWidth - 300) + 'px';
    tip.style.top = p.y + 14 + 'px';
  }
  drawWhole();
});

new ResizeObserver(drawWhole).observe($('whole-view'));

// The filters panel.
function mapFilter(change) {
  change();
  try {
    const keep = {};
    Object.keys(MAP_DEFAULTS).forEach((k) => { keep[k] = mf[k]; });
    localStorage.setItem('fil.map', JSON.stringify(keep));
  } catch (e) {
    // Only a convenience: the map opens with the defaults next time.
  }
  renderMapFilters();
  if (!whole.xy) return;
  if (mf.colour === 'group' && whole.groupsKey !== whole.key + '/' + groupDetail()) {
    loadMapGroups().then(() => mapFilter(() => {}));
  }
  renderWholeLegend();
  sortByValue();
  setShown();
  if (whole.picked >= 0 && !whole.shown[whole.picked]) dropPick();
  else if (whole.picked < 0) wholePane();
  drawWhole();
}

function renderMapFilters() {
  const panel = $('mf-panel');
  if (panel.hidden) return;
  const seg = (options, now, set) => {
    const box = el('div', 'seg');
    options.forEach(([value, label]) => {
      const b = button(label, value === now ? 'on' : '', () => mapFilter(() => set(value)));
      b.setAttribute('aria-pressed', value === now ? 'true' : 'false');
      box.append(b);
    });
    return box;
  };
  const check = (label, on, set) => {
    const l = el('label', 'mf-check');
    const b = el('input');
    b.type = 'checkbox';
    b.checked = on;
    b.addEventListener('change', () => mapFilter(() => set(b.checked)));
    l.append(b, el('span', '', label));
    return l;
  };
  const head = (text) => el('div', 'mf-head', text);

  let top = 0;
  whole.nodes.forEach((_, i) => { top = Math.max(top, measure(i)); });
  const steps = STEPS.filter((v, k) => k === 0 || v <= top);
  const slider = el('input');
  slider.type = 'range';
  slider.min = '0';
  slider.max = String(steps.length - 1);
  slider.value = String(Math.max(0, steps.findIndex((v) => v >= mf.min)));
  slider.setAttribute('aria-label', 'Cited at least');
  const shown = el('b', 'mf-num', mf.min.toLocaleString());
  slider.addEventListener('input', () => { shown.textContent = steps[Number(slider.value)].toLocaleString(); });
  slider.addEventListener('change', () => mapFilter(() => { mf.min = steps[Number(slider.value)]; }));
  const least = el('div', 'mf-row');
  least.append(el('span', 'mf-label', 'Cited at least'), slider, shown);

  const number = (value, label, set) => {
    const y = el('input', 'mf-year');
    y.type = 'number';
    y.placeholder = label;
    y.value = value || '';
    y.setAttribute('aria-label', label);
    y.addEventListener('change', () => mapFilter(() => set(Number(y.value) || 0)));
    return y;
  };
  const years = el('div', 'mf-row');
  years.append(el('span', 'mf-label', 'Years'), number(mf.from, 'From', (v) => { mf.from = v; }), el('span', 'muted', 'to'),
    number(mf.to, 'To', (v) => { mf.to = v; }));

  const names = number(mf.names, 'Names', (v) => { mf.names = Math.max(0, Math.min(200, v)); });
  names.value = String(mf.names);
  const nameRow = el('div', 'mf-row');
  nameRow.append(el('span', '', 'Name the'), names, el('span', '', 'biggest papers'));

  const count = el('span', 'muted');
  count.id = 'mf-count';
  const foot = el('div', 'mf-foot');
  foot.append(count, button('Reset', 'btn small', () => mapFilter(() => Object.assign(mf, MAP_DEFAULTS))));

  const colours = [head('COLOUR'), seg([['state', 'PDF and seeds'], ['group', 'Group']], mf.colour, (v) => { mf.colour = v; mf.only = ''; })];
  if (mf.colour === 'group') {
    const only = onlyGroup();
    whole.groups.forEach((g, k) => {
      const b = button('', 'mf-group' + (only === k ? ' on' : ''), () => mapFilter(() => { mf.only = only === k ? '' : g.anchor; }));
      const dot = el('i', 'key dot');
      dot.style.background = g.colour;
      b.append(dot, el('span', 'mf-group-name', g.name), el('span', 'muted', g.papers.toLocaleString()));
      b.setAttribute('aria-pressed', only === k ? 'true' : 'false');
      b.title = only === k ? 'Show every group' : 'Show only this group';
      colours.push(b);
    });
    colours.push(el('div', 'muted mf-note', whole.groups.length
      ? 'The eight largest groups get a colour. Click one to show only it.'
      : 'No groups found yet. The Suggested groups screen says why.'));
  }

  panel.replaceChildren(
    ...colours,
    head('SIZE BY'), seg([['here', 'Here'], ['world', 'Worldwide'], ['same', 'Same size']], mf.size, (v) => { mf.size = v; mf.min = 0; }),
    head('SHOW'), least, years,
    check('Papers with a PDF', mf.pdf, (v) => { mf.pdf = v; }),
    check('Papers without one', mf.nopdf, (v) => { mf.nopdf = v; }),
    check('Only my papers and what they link to', mf.mine, (v) => { mf.mine = v; }),
    check('Papers not yet fetched', mf.stubs, (v) => { mf.stubs = v; }),
    head('PAPERS LEFT OUT'), seg([[false, 'Faded'], [true, 'Hidden']], mf.hide, (v) => { mf.hide = v; }),
    head('NAMES'), nameRow, foot);
  if (whole.xy) setShown();
}

// toggleMapFilters opens or closes the panel; open forces it either way.
function toggleMapFilters(open) {
  const panel = $('mf-panel');
  panel.hidden = open === undefined ? !panel.hidden : !open;
  $('whole-filters').setAttribute('aria-expanded', panel.hidden ? 'false' : 'true');
  renderMapFilters();
  if (whole.xy) setShown();
}

try {
  Object.assign(mf, JSON.parse(localStorage.getItem('fil.map')) || {});
} catch (e) {
  // The defaults, then.
}
$('whole-filters').addEventListener('click', () => toggleMapFilters());
