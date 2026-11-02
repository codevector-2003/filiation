// The Library screen. Everything shown comes from the library as text, set
// with textContent, so nothing in a paper's title can act as markup.
'use strict';

const DOCS = 'https://github.com/codevector-2003/filiation#documentation';

const state = { show: 'all', title: '', collection: '', selected: '', asked: 0, askedPaper: 0 };

const $ = (id) => document.getElementById(id);

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

async function api(path, method) {
  const r = await fetch(path, { method: method || 'GET' });
  if (r.status === 204) return null;
  const body = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(body.error || 'Something went wrong.');
  return body;
}

async function loadLibrary() {
  const lib = await api('/api/library');
  $('lib-name').textContent = lib.name;
  $('lib-path').textContent = lib.folder || '';
  if (!lib.ready) {
    showNoLibrary(lib);
    return false;
  }
  $('count-papers').textContent = lib.works.toLocaleString();
  const inbox = $('count-inbox');
  inbox.hidden = lib.inbox === 0;
  inbox.textContent = lib.inbox;

  $('collections-head').hidden = lib.collections.length === 0;
  const box = $('collections');
  box.replaceChildren(...lib.collections.map((c) => {
    const b = el('button', 'collection');
    b.type = 'button';
    b.dataset.name = c.name;
    b.append(el('span', '', c.name), el('span', 'count', c.works.toLocaleString()));
    b.addEventListener('click', () => chooseCollection(c.name));
    return b;
  }));
  return true;
}

function showNoLibrary(lib) {
  $('filter').disabled = true;
  document.querySelectorAll('.tab').forEach((t) => { t.disabled = true; });
  const text = lib.problem || 'No library yet. Add your first paper with fil add in a terminal, then open this window again.';
  $('rows').replaceChildren(el('div', lib.problem ? 'note error' : 'note', text));
}

function chooseCollection(name) {
  state.collection = state.collection === name ? '' : name;
  document.querySelectorAll('.collection').forEach((b) => {
    b.classList.toggle('on', b.dataset.name === state.collection);
  });
  loadPapers();
}

async function loadPapers() {
  const asked = ++state.asked;
  const q = new URLSearchParams({ show: state.show, title: state.title, collection: state.collection });
  let res;
  try {
    res = await api('/api/papers?' + q);
  } catch (e) {
    if (asked === state.asked) $('rows').replaceChildren(el('div', 'note error', e.message));
    return;
  }
  if (asked !== state.asked) return; // a newer filter has been typed since
  renderRows(res);
}

function chips(p) {
  const out = [];
  if (p.pdf) out.push(el('span', 'chip pdf', 'PDF'));
  if (p.mismatch) out.push(el('span', 'chip warn', 'Another edition?'));
  if (p.text) out.push(el('span', p.searchable ? 'chip text' : 'chip', p.text));
  if (p.sentences > 0) out.push(el('span', 'chip', p.sentences === 1 ? '1 sentence' : p.sentences + ' sentences'));
  if (p.stub) out.push(el('span', 'chip', 'Not fetched'));
  return out;
}

function renderRows(res) {
  const box = $('rows');
  if (res.papers.length === 0) {
    box.replaceChildren(el('div', 'note', 'No papers match.'));
    $('detail').replaceChildren();
    state.selected = '';
    return;
  }
  const rows = res.papers.map((p) => {
    const b = el('button', 'row');
    b.type = 'button';
    b.dataset.id = p.id;
    const head = el('div', 'row-title');
    if (p.seed) {
      const star = el('span', 'star', '★');
      star.setAttribute('aria-label', 'Added by you');
      head.append(star);
    }
    head.append(el('span', 'title-text', p.title));
    const meta = el('div', 'row-meta');
    if (p.by) meta.append(el('span', '', p.by));
    meta.append(...chips(p));
    b.append(head, meta);
    b.addEventListener('click', () => select(p.id));
    return b;
  });
  box.replaceChildren(...rows);
  if (res.more) box.append(el('div', 'more', 'Showing the first ' + res.papers.length + '. Filter to narrow the list.'));
  box.scrollTop = 0;
  const keep = res.papers.some((p) => p.id === state.selected);
  select(keep ? state.selected : res.papers[0].id);
}

function move(by) {
  const rows = [...document.querySelectorAll('.row')];
  const at = rows.findIndex((r) => r.dataset.id === state.selected);
  const next = rows[at + by];
  if (next) {
    next.focus();
    select(next.dataset.id);
  }
}

async function select(id) {
  state.selected = id;
  document.querySelectorAll('.row').forEach((r) => {
    const on = r.dataset.id === id;
    r.classList.toggle('on', on);
    if (on) r.setAttribute('aria-current', 'true');
    else r.removeAttribute('aria-current');
  });
  const asked = ++state.askedPaper;
  try {
    const p = await api('/api/papers/' + encodeURIComponent(id));
    if (asked === state.askedPaper) renderPaper(p);
  } catch (e) {
    if (asked === state.askedPaper) $('detail').replaceChildren(el('div', 'error', e.message));
  }
}

function stat(n, label) {
  const s = el('div', 'stat');
  s.append(el('b', '', n.toLocaleString()), el('span', '', label));
  return s;
}

function renderPaper(p) {
  const head = el('div', 'head');
  if (p.origin) head.append(el('span', 'origin', (p.seed ? '★ ' : '') + p.origin));
  head.append(el('h1', '', p.title));
  if (p.authors) head.append(el('span', 'authors', p.authors));
  if (p.meta) head.append(el('span', 'meta', p.meta));

  const message = el('div', 'why', p.pdfNote || p.textNote || '');
  const act = (label, primary, path) => {
    const b = el('button', primary ? 'btn primary' : 'btn', label);
    b.type = 'button';
    b.addEventListener('click', async () => {
      try {
        await api('/api/papers/' + encodeURIComponent(p.id) + path, 'POST');
      } catch (e) {
        message.className = 'error';
        message.textContent = e.message;
      }
    });
    return b;
  };
  const actions = el('div', 'actions');
  if (p.pdf) actions.append(act('Open PDF', true, '/open'));
  if (p.folder) actions.append(act('Show in folder', false, '/folder'));

  const stats = el('div', 'stats');
  stats.append(
    stat(p.cites, p.cites === 1 ? 'work it cites' : 'works it cites'),
    p.pages > 0 ? stat(p.pages, p.pages === 1 ? 'page, searchable' : 'pages, searchable')
      : stat(p.citedBy, p.citedBy === 1 ? 'paper here cites it' : 'papers here cite it'),
    stat(p.sentences, p.sentences === 1 ? 'citation with its sentence' : 'citations with their sentence'));

  const parts = [head];
  if (actions.childElementCount > 0) parts.push(actions);
  parts.push(message, stats); // always there: a failed action reports into it

  if (p.sample.length > 0) {
    const box = el('div', 'sentences');
    box.append(el('h2', '', 'WHERE IT CITES ITS REFERENCES'));
    for (const s of p.sample) {
      const fig = el('figure');
      const cap = el('figcaption', '', 'p. ' + s.page + ' · cites ');
      cap.append(el('span', '', s.cites));
      fig.append(el('blockquote', '', '“' + s.text + '”'), cap);
      box.append(fig);
    }
    parts.push(box);
  }
  $('detail').replaceChildren(...parts);
  $('detail').scrollTop = 0;
}

let filterTimer;
$('filter').addEventListener('input', (e) => {
  clearTimeout(filterTimer);
  filterTimer = setTimeout(() => {
    state.title = e.target.value.trim();
    loadPapers();
  }, 150);
});

document.querySelectorAll('.tab').forEach((t) => {
  t.addEventListener('click', () => {
    state.show = t.dataset.show;
    document.querySelectorAll('.tab').forEach((o) => {
      o.classList.toggle('on', o === t);
      o.setAttribute('aria-pressed', o === t ? 'true' : 'false');
    });
    loadPapers();
  });
});

$('rows').addEventListener('keydown', (e) => {
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault();
    move(e.key === 'ArrowDown' ? 1 : -1);
  }
});

$('help').addEventListener('click', () => {
  if (window.runtime) window.runtime.BrowserOpenURL(DOCS);
  else window.open(DOCS, '_blank', 'noopener');
});

loadLibrary()
  .then((ready) => { if (ready) return loadPapers(); })
  .catch((e) => { $('rows').replaceChildren(el('div', 'note error', e.message)); });
