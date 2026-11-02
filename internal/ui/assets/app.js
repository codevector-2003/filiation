// What every screen shares, and the Papers screen. Everything shown comes from
// the library as text, set with textContent, so nothing in a paper's title can
// act as markup.
'use strict';

const DOCS = 'https://github.com/codevector-2003/filiation#documentation';

const state = { show: 'all', title: '', collection: '', selected: '', asked: 0, askedPaper: 0, lib: null };
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
  document.querySelectorAll('.tab, .nav:not([data-screen="papers"]), #fetch-pdfs, #add-paper').forEach((t) => { t.disabled = true; });
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
    const empty = state.lib && state.lib.works === 0 && !state.title && !state.collection && state.show === 'all';
    box.replaceChildren(el('div', 'note', empty ? 'Your library is empty. Add your first paper to begin.' : 'No papers match.'));
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
