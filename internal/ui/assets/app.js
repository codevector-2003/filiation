// What every screen shares, and the Papers screen. Everything shown comes from
// the library as text, set with textContent, so nothing in a paper's title can
// act as markup.
'use strict';

const DOCS = 'https://github.com/codevector-2003/filiation#documentation';

const state = {
  show: 'all', title: '', collection: 0, favourites: false, labels: [], selected: '', asked: 0, askedPaper: 0, lib: null,
  rows: new Map(), picked: new Set(), anchor: '', paper: null, focus: '',
  sort: '', yearFrom: 0, yearTo: 0, authors: [], venues: [], findAuthor: '', findVenue: '',
  loaded: 0, more: false, loading: false, loadingMore: false,
};
const screens = {}; // each screen's script adds what to do when it is shown

const $ = (id) => document.getElementById(id);

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function button(label, cls, onClick) {
  const b = el('button', cls, label);
  b.type = 'button';
  b.addEventListener('click', onClick);
  return b;
}

async function api(path, method, body) {
  const init = { method: method || 'GET' };
  if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' };
    init.body = JSON.stringify(body);
  }
  const r = await fetch(path, init);
  if (r.status === 204 || r.status === 202) return null;
  const answer = await r.json().catch(() => ({}));
  if (!r.ok) {
    const e = new Error(answer.error || 'Something went wrong.');
    e.busy = r.status === 409 && path !== '/api/fetch';
    throw e;
  }
  return answer;
}

function openOutside(url) {
  if (window.runtime) window.runtime.BrowserOpenURL(url);
  else window.open(url, '_blank', 'noopener');
}

function show(name, arg) {
  document.querySelectorAll('.screen').forEach((s) => { s.hidden = s.id !== 'screen-' + name; });
  document.querySelectorAll('.nav').forEach((b) => { b.classList.toggle('on', b.dataset.screen === name); });
  document.querySelector('.side').hidden = name === 'reader'; // the page needs the room
  if (screens[name]) screens[name](arg);
  markPlace();
}

async function loadLibrary() {
  const lib = await api('/api/library');
  state.lib = lib;
  $('lib-name').textContent = lib.ready ? lib.name : 'No library open';
  $('tb-lib').title = lib.folder ? lib.folder + '. Switch library' : 'Switch library';
  if (!lib.ready) {
    showStart(lib);
    return false;
  }
  showCount(lib.works);
  const inbox = $('count-inbox');
  inbox.hidden = lib.inbox === 0;
  inbox.textContent = lib.inbox;

  renderOrganised();
  return true;
}

function showCount(works) {
  $('count-papers').textContent = works.toLocaleString();
  $('lib-count').textContent = works === 1 ? '1 paper' : works.toLocaleString() + ' papers';
}

// unfiltered: the list is showing the whole library, so its total is the
// library's size. An assistant can grow the library while the window is open.
function unfiltered() {
  return state.show === 'all' && !state.title && !filtersOn() && !state.favourites && state.labels.length === 0;
}

function listQuery(offset) {
  const q = new URLSearchParams({ show: state.show, title: state.title, sort: state.sort });
  if (state.collection) q.set('collection', state.collection);
  if (state.favourites) q.set('favourites', '1');
  state.labels.forEach((l) => q.append('label', l));
  if (state.yearFrom) q.set('year_from', state.yearFrom);
  if (state.yearTo) q.set('year_to', state.yearTo);
  state.authors.forEach((a) => q.append('author', a));
  state.venues.forEach((v) => q.append('venue', v));
  if (state.findAuthor) q.set('find_author', state.findAuthor);
  if (state.findVenue) q.set('find_venue', state.findVenue);
  if (offset) q.set('offset', offset);
  return q;
}

async function loadPapers() {
  const asked = ++state.asked;
  state.loading = true; // the rows on show are about to go, so scrolling must not ask for more of them
  state.loadingMore = false;
  let res;
  try {
    res = await api('/api/papers?' + listQuery(0));
  } catch (e) {
    if (asked === state.asked) {
      state.loading = false;
      $('rows').replaceChildren(el('div', 'note error', e.message));
    }
    return;
  }
  if (asked !== state.asked) return; // a newer filter has been typed since
  state.loading = false;
  renderRows(res);
  renderFilters(res);
}

async function loadMore() {
  if (!state.more || state.loading || state.loadingMore) return;
  state.loadingMore = true;
  const asked = ++state.asked;
  let res;
  try {
    res = await api('/api/papers?' + listQuery(state.loaded));
  } catch (e) {
    if (asked === state.asked) {
      state.loadingMore = false;
      showMore(e.message);
    }
    return;
  }
  if (asked !== state.asked) return;
  state.loadingMore = false;
  addRows(res);
}

// showMore ends the list with what is left. Scrolling loads it, and the button
// does the same for anyone not scrolling.
function showMore(text) {
  document.querySelector('#rows > .more')?.remove();
  if (state.more) $('rows').append(button(text, 'more', loadMore));
}

function addRows(res) {
  res.papers.forEach((p) => state.rows.set(p.id, p));
  $('rows').append(...res.papers.map(rowFor));
  state.loaded += res.papers.length;
  state.more = res.more;
  showMore('Showing ' + state.loaded.toLocaleString() + ' of ' + res.total.toLocaleString() + '. Show more.');
}

function filtersOn() {
  return state.yearFrom + state.yearTo + state.authors.length + state.venues.length > 0 || placeChosen();
}

function changeFilter(change) {
  change();
  loadPapers();
}

function pickOption(name, chosen, list) {
  const label = el('label', 'option');
  const box = el('input');
  box.type = 'checkbox';
  box.checked = chosen.includes(name.name);
  box.addEventListener('change', () => changeFilter(() => {
    const at = list().indexOf(name.name);
    if (at >= 0) list().splice(at, 1);
    else list().push(name.name);
  }));
  const shown = el('span', 'option-name', name.name);
  shown.title = name.name;
  label.append(box, shown, el('span', 'option-count', name.works.toLocaleString()));
  return label;
}

// yearStart is where the chart begins: the oldest few papers share its first
// bar, or one paper from 1619 would squash everything after it.
function yearStart(years) {
  const total = years.reduce((n, y) => n + y.works, 0);
  let seen = 0;
  return years.find((y) => { seen += y.works; return seen > total * 0.02; }).year;
}

// yearStep keeps the chart to about forty bars, however far back a library goes.
function yearStep(from, years) {
  const span = years[years.length - 1].year - from + 1;
  return [1, 2, 5, 10, 20, 50, 100].find((s) => span / s <= 40) || 200;
}

function renderYears(years) {
  const box = $('year-bars');
  if (years.length === 0) {
    box.replaceChildren(el('span', 'muted', 'No years to show.'));
    box.onpointerdown = null;
    return;
  }
  const start = yearStart(years);
  const step = yearStep(start, years);
  const first = Math.floor(start / step) * step;
  const binOf = (y) => Math.max(0, Math.floor((y - first) / step));
  const bins = Array.from({ length: Math.floor((years[years.length - 1].year - first) / step) + 1 }, () => 0);
  years.forEach((y) => { bins[binOf(y.year)] += y.works; });
  const top = Math.max(...bins);
  const bars = bins.map((n, i) => {
    const b = el('div', 'ybar');
    const from = first + i * step;
    b.style.height = (n === 0 ? 0 : Math.max(4, Math.round((100 * n) / top))) + '%';
    const older = i === 0 && years[0].year < first;
    b.title = (older ? 'Up to ' + (from + step - 1) : step === 1 ? from : from + ' to ' + (from + step - 1)) + ': ' + n.toLocaleString();
    return b;
  });
  box.replaceChildren(...bars);
  const mark = (a, z) => bars.forEach((b, i) => { b.classList.toggle('in', a !== null && i >= a && i <= z); });
  mark(filtersYear() ? binOf(state.yearFrom || first) : null, binOf(state.yearTo || first + bins.length * step - 1));
  const at = (e) => {
    const r = box.getBoundingClientRect();
    return Math.min(bins.length - 1, Math.max(0, Math.floor(((e.clientX - r.left) / r.width) * bins.length)));
  };
  box.onpointerdown = (e) => {
    box.setPointerCapture(e.pointerId);
    const start = at(e);
    mark(start, start);
    box.onpointermove = (m) => mark(Math.min(start, at(m)), Math.max(start, at(m)));
    box.onpointerup = (u) => {
      box.onpointermove = null;
      box.onpointerup = null;
      const a = Math.min(start, at(u));
      const z = Math.max(start, at(u));
      changeFilter(() => {
        state.yearFrom = a === 0 ? Math.min(first, years[0].year) : first + a * step;
        state.yearTo = first + z * step + step - 1;
      });
    };
  };
}

function filtersYear() {
  return state.yearFrom > 0 || state.yearTo > 0;
}

function renderFilters(res) {
  renderYears(res.facets.years);
  $('year-from').value = state.yearFrom || '';
  $('year-to').value = state.yearTo || '';
  $('author-options').replaceChildren(...res.facets.authors.map((a) => pickOption(a, state.authors, () => state.authors)));
  $('venue-options').replaceChildren(...res.facets.venues.map((v) => pickOption(v, state.venues, () => state.venues)));

  if (state.lib && unfiltered() && res.total !== state.lib.works) {
    state.lib.works = res.total;
    showCount(res.total);
  } else if (state.lib && res.total > state.lib.works) {
    loadLibrary();
  }
  const all = state.lib ? Math.max(state.lib.works, res.total) : res.total;
  $('list-count').textContent = res.total === all
    ? all.toLocaleString() + (all === 1 ? ' paper' : ' papers')
    : res.total.toLocaleString() + ' of ' + all.toLocaleString() + ' papers';

  const chip = (label, drop) => {
    const b = button(label + ' ×', 'fchip', drop);
    b.title = 'Remove: ' + label;
    return b;
  };
  const chips = [];
  if (state.collection) chips.push(chip(pathOf(state.collection), () => choosePlace(0, false)));
  if (state.favourites) chips.push(chip('Favourites', () => choosePlace(0, false)));
  state.labels.forEach((id) => {
    const l = labelById(id);
    chips.push(chip(l ? l.name : 'A deleted label', () => toggleLabelFilter(id)));
  });
  if (filtersYear()) {
    const label = state.yearFrom && state.yearTo
      ? (state.yearFrom === state.yearTo ? '' + state.yearFrom : state.yearFrom + ' to ' + state.yearTo)
      : state.yearFrom ? 'From ' + state.yearFrom : 'Up to ' + state.yearTo;
    chips.push(chip(label, () => changeFilter(() => { state.yearFrom = 0; state.yearTo = 0; })));
  }
  state.authors.forEach((a) => chips.push(chip(a, () => changeFilter(() => { state.authors = state.authors.filter((o) => o !== a); }))));
  state.venues.forEach((v) => chips.push(chip(v, () => changeFilter(() => { state.venues = state.venues.filter((o) => o !== v); }))));
  if (chips.length > 1) {
    chips.push(button('Clear all', 'link', () => {
      state.collection = 0;
      state.favourites = false;
      state.labels = [];
      markPlace();
      $('find-author').value = '';
      $('find-venue').value = '';
      changeFilter(() => {
        state.yearFrom = 0;
        state.yearTo = 0;
        state.authors = [];
        state.venues = [];
        state.findAuthor = '';
        state.findVenue = '';
      });
    }));
  }
  $('active-filters').replaceChildren(...chips);
  const n = (filtersYear() ? 1 : 0) + state.authors.length + state.venues.length;
  $('filters-toggle').textContent = n > 0 ? 'Filters (' + n + ')' : 'Filters';
}

function chips(p) {
  const out = [];
  if (p.zotero) out.push(el('span', 'chip', 'From Zotero'));
  else if (p.seed) out.push(el('span', 'chip', 'Added by you'));
  if (p.zotero === 'unmatched') out.push(el('span', 'chip warn', 'Not matched'));
  if (p.pdf) out.push(el('span', 'chip pdf', 'PDF'));
  if (p.mismatch) out.push(el('span', 'chip warn', 'Another edition?'));
  if (p.text) out.push(el('span', p.searchable ? 'chip text' : 'chip', p.text));
  if (p.sentences > 0) out.push(el('span', 'chip', p.sentences === 1 ? '1 sentence' : p.sentences + ' sentences'));
  if (p.stub) out.push(el('span', 'chip', 'Not fetched'));
  out.push(...p.labels.map(labelChip).filter(Boolean));
  return out;
}

// rowFor draws one paper: a box to pick it among several, its star, and the
// paper itself, which opens it.
function rowFor(p) {
  const row = el('div', 'row paper-row');
  row.dataset.id = p.id;
  const box = el('input', 'pick');
  box.type = 'checkbox';
  box.tabIndex = -1;
  box.setAttribute('aria-label', 'Select: ' + p.title);
  box.addEventListener('click', (e) => {
    e.stopPropagation();
    togglePick(p.id);
  });
  const main = el('button', 'row-main');
  main.type = 'button';
  const head = el('div', 'row-title');
  head.append(el('span', 'title-text', p.title));
  const meta = el('div', 'row-meta');
  if (p.by) meta.append(el('span', '', p.by));
  meta.append(...chips(p));
  main.append(head, meta);
  main.addEventListener('click', (e) => clickRow(p.id, e));
  row.append(box, starButton(p), main);
  row.addEventListener('contextmenu', (e) => {
    e.preventDefault();
    rowMenu(p.id, e.clientX, e.clientY);
  });
  row.addEventListener('pointerdown', (e) => dragPapers(e, p.id));
  markRow(row);
  return row;
}

function renderRows(res) {
  const box = $('rows');
  state.loaded = 0;
  state.more = false;
  state.rows = new Map();
  // A paper opened from elsewhere, a citation in the Reader say, stays on show
  // whether or not the list as filtered holds it.
  const focus = state.focus;
  state.focus = '';
  if (res.papers.length === 0) {
    const empty = state.lib && state.lib.works === 0 && !state.title && state.show === 'all' && !filtersOn();
    box.replaceChildren(el('div', 'note', empty ? 'Your library is empty. Add your first paper to begin.' : emptyPlace()));
    if (focus) return;
    $('detail').replaceChildren();
    state.selected = '';
    state.picked.clear();
    showPicked();
    return;
  }
  box.replaceChildren();
  addRows(res);
  box.scrollTop = 0;
  [...state.picked].forEach((id) => { if (!state.rows.has(id)) state.picked.delete(id); });
  if (state.picked.size > 1) {
    showPicked();
    return;
  }
  if (focus) return;
  const keep = res.papers.some((p) => p.id === state.selected);
  select(keep ? state.selected : res.papers[0].id);
}

// openPaper shows the paper id on the Papers screen, from anywhere.
function openPaper(id) {
  state.focus = id;
  if (document.querySelector('#screen-papers').hidden) show('papers');
  else state.focus = '';
  select(id);
}

function paperRows() {
  return [...document.querySelectorAll('#rows .paper-row')];
}

// move goes up or down the list; with Shift it picks the papers it passes.
function move(by, extend) {
  const rows = paperRows();
  const at = rows.findIndex((r) => r.dataset.id === state.selected);
  const next = rows[at + by];
  if (!next) return;
  next.querySelector('.row-main').focus();
  if (extend) {
    if (state.picked.size === 0) state.picked.add(state.selected);
    if (state.picked.has(next.dataset.id)) state.picked.delete(state.selected);
    else state.picked.add(next.dataset.id);
    state.selected = next.dataset.id;
    showPicked();
    return;
  }
  select(next.dataset.id);
}

async function select(id) {
  state.selected = id;
  state.anchor = id;
  state.picked.clear();
  showPicked();
  const asked = ++state.askedPaper;
  try {
    const p = await api('/api/papers/' + encodeURIComponent(id));
    if (asked === state.askedPaper && state.picked.size < 2) renderPaper(p);
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
  state.paper = p;
  const head = el('div', 'head');
  const top = el('div', 'head-top');
  top.append(el('span', 'origin', p.origin || ''), favouriteButton(p));
  head.append(top);
  head.append(el('h1', '', p.title));
  if (p.authors) head.append(el('span', 'authors', p.authors));
  if (p.meta) head.append(el('span', 'meta', p.meta));

  const message = el('div', 'why', p.pdfNote || p.textNote || '');
  const act = (label, primary, path) => button(label, primary ? 'btn primary' : 'btn', async () => {
    try {
      await api('/api/papers/' + encodeURIComponent(p.id) + path, 'POST');
    } catch (e) {
      message.className = 'error';
      message.textContent = e.message;
    }
  });
  const actions = el('div', 'actions');
  if (p.pdf) actions.append(button('Read', 'btn primary', () => openReader(p.id, 1, [])));
  if (p.pdf) actions.append(act('Open PDF', false, '/open'));
  if (!p.pdf && !p.local) {
    const get = button('Download PDF', p.downloadable ? 'btn primary' : 'btn', () => downloadPDFs([p.id]));
    get.disabled = !p.downloadable || !!(job.last && job.last.running);
    if (!p.downloadable) get.title = p.pdfNote || 'No legal open-access copy is known';
    // A PDF the person has: first when there is nothing to download.
    const mine = button('Add your PDF…', p.downloadable ? 'btn' : 'btn primary', () => attachPDF(p, '', false));
    mine.title = 'File a copy you have under this paper. Your file is copied, not moved.';
    if (p.downloadable) actions.append(get, mine);
    else actions.append(mine, get);
  }
  if (!p.local) actions.append(button('Bring in papers around this…', 'btn', () => aroundDialog(p)));
  if (p.folder) actions.append(act('Show in folder', false, '/folder'));
  actions.append(button('Citation map', 'btn', () => show('map', p.id)));
  if (p.web) actions.append(button('Website', 'btn', () => openOutside(p.web)));
  if (p.pages > 0) actions.append(button('Search inside', 'btn', () => show('search', { id: p.id, title: p.title })));

  const stats = el('div', 'stats');
  stats.append(
    stat(p.cites, p.cites === 1 ? 'work it cites' : 'works it cites'),
    p.pages > 0 ? stat(p.pages, p.pages === 1 ? 'page, searchable' : 'pages, searchable')
      : stat(p.citedBy, p.citedBy === 1 ? 'paper here cites it' : 'papers here cite it'),
    stat(p.sentences, p.sentences === 1 ? 'citation with its sentence' : 'citations with their sentence'));

  const parts = [head, actions, ...filedIn(p)];
  if (p.local) parts.push(matchBox(p));
  parts.push(message); // always there: a failed action reports into it

  // The abstract says what the paper is about. Where it cites its references
  // is on the citation map, sentence by sentence.
  const about = el('section', 'abstract');
  about.append(el('h2', '', 'ABSTRACT'));
  if (p.abstract) about.append(el('p', '', p.abstract));
  else about.append(el('p', 'muted', p.local ? 'Your own document has no abstract here.' : 'OpenAlex has no abstract for this paper.'));
  parts.push(about, stats);
  $('detail').replaceChildren(...parts);
  $('detail').scrollTop = 0;
}

// Long work runs one job at a time; the sidebar shows it on every screen.
const job = { timer: 0, hide: 0, run: 0, listeners: [], ticks: [], last: null };

async function pollJob() {
  let j;
  try {
    j = await api('/api/job');
  } catch (e) {
    return;
  }
  renderJob(j);
  if (j.running) {
    clearTimeout(job.timer);
    job.timer = setTimeout(pollJob, 500);
  } else if (j.run !== job.run) {
    job.run = j.run;
    await loadLibrary();
    loadPapers();
    job.listeners.forEach((f) => f(j));
  }
}

function renderJob(j) {
  job.last = j;
  job.ticks.forEach((f) => f(j));
  const box = $('job');
  clearTimeout(job.hide);
  if (!j.running && !j.summary && !j.error) {
    box.hidden = true;
    return;
  }
  box.hidden = false;
  // What a finished job did stays a while, then makes room; an error stays.
  if (!j.running && !j.error) job.hide = setTimeout(() => { box.hidden = true; }, 12000);
  $('job-title').textContent = { import: 'Filing PDFs', zotero: 'Bringing in Zotero', expand: 'Following citations',
    download: 'Downloading', around: 'Bringing in papers' }[j.kind] || 'Fetching PDFs';
  box.title = j.error || (j.running ? j.note : j.summary) || '';
  $('job-count').textContent = j.running && j.total > 0 ? j.done + ' of ' + j.total : '';
  $('job-fill').style.width = j.running ? (j.total > 0 ? Math.round((100 * j.done) / j.total) : 100) + '%' : '100%';
  $('job-note').textContent = j.error || (j.running ? j.note : j.summary) || '';
  $('job-note').classList.toggle('error', !!j.error);
  $('job-stop').hidden = !j.running;
  document.querySelectorAll('#fetch-pdfs').forEach((b) => { b.disabled = j.running; });
}

async function startJob(path, body) {
  await api(path, 'POST', body);
  pollJob();
}

let filterTimer;
$('filter').addEventListener('input', (e) => {
  clearTimeout(filterTimer);
  filterTimer = setTimeout(() => {
    state.title = e.target.value.trim();
    loadPapers();
  }, 150);
});

document.querySelectorAll('.tab[data-show]').forEach((t) => {
  t.addEventListener('click', () => {
    state.show = t.dataset.show;
    document.querySelectorAll('.tab[data-show]').forEach((o) => {
      o.classList.toggle('on', o === t);
      o.setAttribute('aria-pressed', o === t ? 'true' : 'false');
    });
    loadPapers();
  });
});

$('sort').addEventListener('change', (e) => changeFilter(() => { state.sort = e.target.value; }));

$('filters-toggle').addEventListener('click', () => {
  const panel = $('filters');
  panel.hidden = !panel.hidden;
  $('filters-toggle').setAttribute('aria-expanded', panel.hidden ? 'false' : 'true');
});

function readYears() {
  const from = Number($('year-from').value) || 0;
  const to = Number($('year-to').value) || 0;
  changeFilter(() => {
    state.yearFrom = from && to ? Math.min(from, to) : from;
    state.yearTo = from && to ? Math.max(from, to) : to;
  });
}
$('year-from').addEventListener('change', readYears);
$('year-to').addEventListener('change', readYears);

[['find-author', 'findAuthor'], ['find-venue', 'findVenue']].forEach(([id, key]) => {
  $(id).addEventListener('input', (e) => {
    clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {
      state[key] = e.target.value.trim();
      loadPapers();
    }, 150);
  });
});

$('rows').addEventListener('scroll', (e) => {
  const box = e.target;
  if (box.scrollHeight - box.scrollTop - box.clientHeight < 400) loadMore();
});

document.querySelectorAll('.nav[data-screen]').forEach((b) => {
  b.addEventListener('click', () => {
    // Papers is the whole library: it leaves a collection or Favourites.
    if (b.dataset.screen === 'papers' && (state.collection || state.favourites)) {
      choosePlace(0, false);
      return;
    }
    show(b.dataset.screen);
  });
});

$('rows').addEventListener('keydown', (e) => {
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault();
    move(e.key === 'ArrowDown' ? 1 : -1, e.shiftKey);
  } else if (e.key === 'a' && (e.ctrlKey || e.metaKey)) {
    e.preventDefault();
    pickAll();
  } else if (e.key === 'Escape' && state.picked.size > 1) {
    select(state.selected);
  } else if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
    const row = e.target.closest('.paper-row');
    if (!row) return;
    e.preventDefault();
    const r = row.getBoundingClientRect();
    rowMenu(row.dataset.id, r.left + 40, r.top + r.height / 2);
  }
});

$('job-stop').addEventListener('click', () => api('/api/job/stop', 'POST'));
$('fetch-pdfs').addEventListener('click', () => {
  startJob('/api/fetch').catch((e) => renderJob({ error: e.message }));
});

screens.papers = () => { loadPapers(); };

// Add a paper.
function resetAdd() {
  $('add-input').value = '';
  $('add-error').textContent = '';
  $('add-offers').replaceChildren();
  $('add-go').disabled = false;
}

async function addPaper(body) {
  $('add-error').textContent = '';
  $('add-go').disabled = true;
  try {
    const res = await api('/api/add', 'POST', body);
    if (res.offers) {
      $('add-offers').replaceChildren(el('div', 'muted', 'Several papers match. Which one?'), ...res.offers.map((o) => {
        const b = el('button', 'offer');
        b.type = 'button';
        b.append(el('span', 'title-text', o.title), el('span', 'muted', o.by));
        b.addEventListener('click', () => addPaper({ candidate: o.index }));
        return b;
      }));
      return;
    }
    $('add-dialog').close();
    state.selected = res.id;
    await loadLibrary();
    loadPapers();
  } catch (e) {
    $('add-error').textContent = e.message;
  } finally {
    $('add-go').disabled = false;
  }
}

$('add-paper').addEventListener('click', () => {
  resetAdd();
  $('add-dialog').showModal();
  $('add-input').focus();
});
$('add-cancel').addEventListener('click', () => $('add-dialog').close());
$('add-form').addEventListener('submit', (e) => {
  e.preventDefault();
  const input = $('add-input').value.trim();
  if (input) addPaper({ input });
});
