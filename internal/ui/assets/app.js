// What every screen shares, and the Papers screen. Everything shown comes from
// the library as text, set with textContent, so nothing in a paper's title can
// act as markup.
'use strict';

const DOCS = 'https://github.com/codevector-2003/filiation#documentation';

const state = {
  show: 'all', title: '', collection: '', selected: '', asked: 0, askedPaper: 0, lib: null,
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
}

async function loadLibrary() {
  const lib = await api('/api/library');
  state.lib = lib;
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
    b.classList.toggle('on', c.name === state.collection);
    b.append(el('span', '', c.name), el('span', 'count', c.works.toLocaleString()));
    b.addEventListener('click', () => chooseCollection(c.name));
    return b;
  }));
  return true;
}

function showNoLibrary(lib) {
  $('filter').disabled = true;
  document.querySelectorAll('.tab, .nav:not([data-screen="papers"]), #fetch-pdfs, #add-paper, #sort, #filters-toggle').forEach((t) => { t.disabled = true; });
  const text = lib.problem || 'No library is open.';
  $('rows').replaceChildren(el('div', 'note error', text));
}

function chooseCollection(name) {
  state.collection = state.collection === name ? '' : name;
  document.querySelectorAll('.collection').forEach((b) => {
    b.classList.toggle('on', b.dataset.name === state.collection);
  });
  show('papers');
}

function listQuery(offset) {
  const q = new URLSearchParams({ show: state.show, title: state.title, collection: state.collection, sort: state.sort });
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
  $('rows').append(...res.papers.map(rowFor));
  state.loaded += res.papers.length;
  state.more = res.more;
  showMore('Showing ' + state.loaded.toLocaleString() + ' of ' + res.total.toLocaleString() + '. Show more.');
}

function filtersOn() {
  return state.yearFrom + state.yearTo + state.authors.length + state.venues.length > 0;
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

  const all = state.lib ? state.lib.works : res.total;
  $('list-count').textContent = res.total === all
    ? all.toLocaleString() + (all === 1 ? ' paper' : ' papers')
    : res.total.toLocaleString() + ' of ' + all.toLocaleString() + ' papers';

  const chip = (label, drop) => {
    const b = button(label + ' ×', 'fchip', drop);
    b.title = 'Remove: ' + label;
    return b;
  };
  const chips = [];
  if (state.collection) chips.push(chip(state.collection, () => chooseCollection(state.collection)));
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
      state.collection = '';
      document.querySelectorAll('.collection').forEach((b) => { b.classList.remove('on'); });
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
  if (p.pdf) out.push(el('span', 'chip pdf', 'PDF'));
  if (p.mismatch) out.push(el('span', 'chip warn', 'Another edition?'));
  if (p.text) out.push(el('span', p.searchable ? 'chip text' : 'chip', p.text));
  if (p.sentences > 0) out.push(el('span', 'chip', p.sentences === 1 ? '1 sentence' : p.sentences + ' sentences'));
  if (p.stub) out.push(el('span', 'chip', 'Not fetched'));
  return out;
}

function rowFor(p) {
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
}

function renderRows(res) {
  const box = $('rows');
  state.loaded = 0;
  state.more = false;
  if (res.papers.length === 0) {
    const empty = state.lib && state.lib.works === 0 && !state.title && !state.collection && state.show === 'all' && !filtersOn();
    box.replaceChildren(el('div', 'note', empty ? 'Your library is empty. Add your first paper to begin.' : 'No papers match.'));
    $('detail').replaceChildren();
    state.selected = '';
    return;
  }
  box.replaceChildren();
  addRows(res);
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

  const parts = [head, actions];
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
    if (p.sentences > p.sample.length) {
      box.append(el('div', 'muted', 'The first ' + p.sample.length + ' of ' + p.sentences + '. The citation map shows each one.'));
    }
    parts.push(box);
  }
  $('detail').replaceChildren(...parts);
  $('detail').scrollTop = 0;
}

// Long work runs one job at a time; the sidebar shows it on every screen.
const job = { timer: 0, run: 0, listeners: [] };

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
  const box = $('job');
  if (!j.running && !j.summary && !j.error) {
    box.hidden = true;
    return;
  }
  box.hidden = false;
  $('job-title').textContent = j.kind === 'import' ? 'Filing PDFs' : 'Fetching PDFs';
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

document.querySelectorAll('.nav').forEach((b) => {
  b.addEventListener('click', () => show(b.dataset.screen));
});

$('rows').addEventListener('keydown', (e) => {
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault();
    move(e.key === 'ArrowDown' ? 1 : -1);
  }
});

$('help').addEventListener('click', () => openOutside(DOCS));
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
