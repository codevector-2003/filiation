// The whole library on one screen. Thousands of papers are too many for page
// elements, so this one is drawn on a canvas; a paper's title still only ever
// reaches the page through textContent or the canvas's fillText.
'use strict';

const DOT = { seed: '#8A5300', pdf: '#1F5F8B', titled: '#83878E', unnamed: '#C4C7CB' };
const LABELS = 40;

const whole = {
  nodes: [], edges: [], key: '', xy: null, degree: null, around: null, byDegree: [],
  all: false, shown: [], match: null, picked: -1, near: null, hover: -1,
  scale: 1, x: 0, y: 0, asked: 0, worker: null, queued: false,
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
}

function papersFiltered() {
  return state.show !== 'all' || state.title !== '' || state.collection !== '' || filtersOn();
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
  whole.shown = whole.nodes.map((n, i) => whole.all || whole.degree[i] >= 2 || n.seed || (whole.match !== null && whole.match.has(i)));
  const count = whole.shown.filter(Boolean).length;
  $('whole-count').textContent = 'Showing ' + count.toLocaleString() + ' of ' + whole.nodes.length.toLocaleString();
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
  return Math.min(16, 2 + Math.sqrt(whole.nodes[i].cited) * 1.1) * Math.max(0.8, Math.min(1.8, Math.sqrt(whole.scale)));
}

function dotColour(n) {
  if (n.seed) return DOT.seed;
  if (n.pdf) return DOT.pdf;
  return n.title ? DOT.titled : DOT.unnamed;
}

function bright(i) {
  if (whole.picked >= 0) return i === whole.picked || whole.near.has(i);
  return whole.match === null || whole.match.has(i);
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
    const c = dotColour(whole.nodes[i]);
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
  const first = whole.picked >= 0 ? [whole.picked, ...whole.byDegree] : whole.byDegree;
  let left = LABELS;
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
    parts.push(el('div', 'muted', 'Click a paper to see its links. Bigger means cited by more papers here.'));
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
