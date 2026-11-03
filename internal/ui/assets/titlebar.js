// The title bar: the menus, the library switcher, the running job and the
// window's buttons (D30). The menus come from the server, which builds macOS's
// menu bar from the same list, so each command is run here by its ID however
// it was asked for: a click, a shortcut, or the Mac's menu bar.
'use strict';

const bar = {
  menus: [], open: '', hot: -1, keys: new Map(), chord: '', platform: 'web', version: '',
  history: [], ahead: [], replaying: false,
};
const ISSUES = 'https://github.com/codevector-2003/filiation/issues/new';
const LICENCE = 'https://github.com/codevector-2003/filiation/blob/main/LICENSE';

// The platform decides what the title bar draws: Windows gets the window's
// buttons, macOS leaves room for its own and its menu bar takes the menus.
async function platform() {
  if (!window.runtime) return 'web';
  try {
    return (await window.runtime.Environment()).platform;
  } catch (e) {
    return 'web';
  }
}

const mac = () => bar.platform === 'darwin';

// Panes and zoom are kept per computer, as a convenience: losing them costs
// nothing.
function pref(key) {
  try {
    return localStorage.getItem('fil.' + key) || '';
  } catch (e) {
    return '';
  }
}
function setPref(key, value) {
  try {
    if (value) localStorage.setItem('fil.' + key, value);
    else localStorage.removeItem('fil.' + key);
  } catch (e) {
    // The pane opens as it was made next time.
  }
}

// keyText writes a shortcut the way the system does: Ctrl+Shift+N, or ⇧⌘N.
function keyText(keys) {
  if (!keys) return '';
  const parts = keys.split('+');
  const key = parts.pop();
  const name = { left: mac() ? '←' : 'Left', right: mac() ? '→' : 'Right', '=': mac() ? '=' : '=' }[key] || key.toUpperCase();
  if (mac()) {
    const sym = { cmdorctrl: '⌘', shift: '⇧', optionoralt: '⌥', ctrl: '⌃' };
    return parts.map((p) => sym[p]).join('') + name;
  }
  const word = { cmdorctrl: 'Ctrl', shift: 'Shift', optionoralt: 'Alt', ctrl: 'Ctrl' };
  return [...parts.map((p) => word[p]), name].join('+');
}

// keyOf is a key press as keys are written in the menus.
function keyOf(e) {
  let key = e.key.toLowerCase();
  if (key === 'arrowleft') key = 'left';
  if (key === 'arrowright') key = 'right';
  if (key === '+') key = '='; // Ctrl and the + key, without Shift
  if (e.code && /^Digit\d$/.test(e.code)) key = e.code.slice(5);
  const parts = [];
  if (mac() ? e.metaKey : e.ctrlKey) parts.push('cmdorctrl');
  if (mac() && e.ctrlKey) parts.push('ctrl');
  if (e.altKey) parts.push('optionoralt');
  if (e.shiftKey) parts.push('shift');
  return [...parts, key].join('+');
}

function sortedKeys(keys) {
  const parts = keys.split('+');
  const key = parts.pop();
  const order = ['cmdorctrl', 'ctrl', 'optionoralt', 'shift'];
  return [...order.filter((o) => parts.includes(o)), key].join('+');
}

async function loadMenus() {
  const m = await api('/api/menus');
  bar.menus = m.menus;
  bar.version = m.version || '';
  bar.keys.clear();
  m.menus.forEach((menu) => menu.items.forEach((it) => {
    if (it.keys && !it.edit) bar.keys.set(sortedKeys(it.keys), it.id);
  }));
  $('tb-menus').replaceChildren(...m.menus.map((menu, i) => {
    const b = el('button', 'tb-menu');
    b.type = 'button';
    b.setAttribute('role', 'menuitem');
    b.setAttribute('aria-haspopup', 'menu');
    // Alt shows which letter opens each menu, as Windows does.
    const u = el('u', '', menu.name[0]);
    b.append(u, document.createTextNode(menu.name.slice(1)));
    b.addEventListener('click', () => (bar.open === menu.name ? closeMenus() : openMenu(i, false)));
    b.addEventListener('mouseenter', () => { if (bar.open && bar.open !== menu.name) openMenu(i, false); });
    return b;
  }));
}

const selectedPaper = () => (state.selected && state.paper && state.paper.id === state.selected ? state.paper : null);
const onScreen = () => (document.querySelector('.screen:not([hidden])') || { id: '' }).id.replace('screen-', '');
const ready = () => !!(state.lib && state.lib.ready);
const running = () => !!(job.last && job.last.running);

// enabled says whether a command can run now; a disabled one is greyed.
function enabled(id) {
  const p = selectedPaper();
  const folder = ready() && !!state.lib.folder;
  switch (id) {
    case 'lib.new': case 'lib.open': case 'lib.recent': case 'app.quit':
    case 'view.zoomIn': case 'view.zoomOut': case 'view.zoomReset': case 'view.fullscreen':
      return true;
    case 'lib.close': return ready();
    case 'lib.reveal': case 'lib.scan': case 'export.graphml': case 'pdfs.import': return folder;
    case 'lib.move': return ready() && !running();
    case 'graph.expand': case 'pdfs.fetch': case 'zotero': return ready() && !running();
    case 'job.stop': return running();
    case 'go.back': return bar.history.length > 0;
    case 'go.forward': return bar.ahead.length > 0;
    case 'go.path': case 'paper.map': case 'paper.favourite': case 'paper.collect': case 'paper.label':
    case 'paper.cite': case 'paper.copyId':
      return !!p;
    case 'paper.read': case 'paper.open': return !!p && p.pdf;
    case 'paper.download': return !!p && !running() && (state.picked.size > 1 || (!p.pdf && p.downloadable));
    case 'paper.around': return !!p && !p.local && !running();
    case 'paper.attach': return !!p && !p.pdf && !p.local;
    case 'paper.folder': return !!p && p.folder;
    case 'view.details': return onScreen() === 'papers';
  }
  if (id.startsWith('edit.') || id.startsWith('help.')) return true;
  return ready();
}

// ticked marks what is on: the screen shown, the panes open.
function ticked(id) {
  const screen = onScreen();
  switch (id) {
    case 'view.papers': return screen === 'papers' && !state.favourites;
    case 'view.favourites': return screen === 'papers' && state.favourites;
    case 'view.search': return screen === 'search';
    case 'view.map': return screen === 'map';
    case 'view.inbox': return screen === 'inbox';
    case 'view.assistants': return screen === 'assistants';
    case 'view.sidebar': return !document.querySelector('.side').classList.contains('closed');
    case 'view.details': return !$('detail').classList.contains('closed');
    case 'view.mapFilters': return screen === 'map' && !$('mf-panel').hidden;
  }
  return false;
}

function itemButton(it, onRun) {
  const b = el('button', 'tb-item');
  b.type = 'button';
  b.setAttribute('role', 'menuitem');
  b.dataset.id = it.id;
  b.disabled = !enabled(it.id);
  b.append(el('span', 'tb-tick', ticked(it.id) ? '✓' : ''), el('span', '', it.label));
  b.append(el('span', 'tb-keys', it.sub ? '›' : it.shown || keyText(it.keys)));
  b.addEventListener('click', (e) => {
    e.stopPropagation();
    if (it.sub) {
      openSub(it, b);
      return;
    }
    closeMenus();
    onRun(it.id);
  });
  b.addEventListener('mouseenter', () => {
    hotItem([...$('tb-panel').querySelectorAll('.tb-item')].indexOf(b));
    if (it.sub && !b.disabled) openSub(it, b);
    else if ($('tb-sub').dataset.from !== it.id) $('tb-sub').hidden = true;
  });
  return b;
}

function openMenu(i, keyboard) {
  const menu = bar.menus[i];
  const anchor = $('tb-menus').children[i];
  bar.open = menu.name;
  [...$('tb-menus').children].forEach((b, j) => b.classList.toggle('on', j === i));
  const panel = $('tb-panel');
  panel.replaceChildren(...menu.items.map((it) => (it.id ? itemButton(it, run) : el('div', 'tb-sep'))));
  const r = anchor.getBoundingClientRect();
  panel.style.left = r.left + 'px';
  panel.style.top = r.bottom + 2 + 'px';
  panel.hidden = false;
  $('tb-sub').hidden = true;
  bar.hot = -1;
  if (keyboard) hotItem(nextItem(-1, 1));
}

function closeMenus() {
  bar.open = '';
  bar.hot = -1;
  [...$('tb-menus').children].forEach((b) => b.classList.remove('on'));
  $('tb-panel').hidden = true;
  $('tb-sub').hidden = true;
  $('titlebar').classList.remove('keys');
  closeSwitcher();
}

function hotItem(i) {
  bar.hot = i;
  $('tb-panel').querySelectorAll('.tb-item').forEach((b, j) => b.classList.toggle('hot', j === i));
}

function nextItem(from, by) {
  const items = [...$('tb-panel').querySelectorAll('.tb-item')];
  for (let k = 1; k <= items.length; k++) {
    const i = (from + by * k + items.length * 2) % items.length;
    if (!items[i].disabled) return i;
  }
  return -1;
}

// openSub shows Open Recent beside its item.
async function openSub(it, anchor) {
  const sub = $('tb-sub');
  sub.dataset.from = it.id;
  const r = anchor.getBoundingClientRect();
  sub.style.left = r.right - 4 + 'px';
  sub.style.top = r.top - 4 + 'px';
  sub.replaceChildren(el('div', 'tb-head', 'LOADING'));
  sub.hidden = false;
  let res;
  try {
    res = await api('/api/libraries');
  } catch (e) {
    sub.replaceChildren(el('div', 'tb-head', e.message));
    return;
  }
  if (sub.dataset.from !== it.id || sub.hidden) return;
  const others = res.libraries.filter((l) => !l.open);
  if (others.length === 0) {
    sub.replaceChildren(el('div', 'tb-head', 'NO OTHER LIBRARIES'));
    return;
  }
  sub.replaceChildren(...others.map((l) => libraryItem(l)));
}

// libraryItem is a library in a list: its name, and its folder beneath.
function libraryItem(l) {
  const b = el('button', 'tb-item two');
  b.type = 'button';
  b.setAttribute('role', 'menuitem');
  const two = el('span', 'tb-two');
  two.append(el('span', '', l.name), el('span', 'tb-path', l.folder));
  b.append(el('span', 'tb-tick', l.open ? '✓' : ''), two);
  if (!l.found) {
    b.append(el('span', 'tb-gone', 'Not found'));
    b.title = 'This folder is gone. Remove it from the list?';
  }
  b.addEventListener('click', (e) => {
    e.stopPropagation();
    closeMenus();
    if (l.open) return;
    if (!l.found) forgetLibrary(l);
    else switchLibrary('/api/libraries/open', { folder: l.folder }, l.name);
  });
  return b;
}

// The library switcher, in the middle of the title bar.
async function openSwitcher() {
  closeMenus();
  const panel = $('tb-panel');
  const anchor = $('tb-lib');
  anchor.setAttribute('aria-expanded', 'true');
  bar.open = 'switcher';
  const r = anchor.getBoundingClientRect();
  panel.style.left = Math.max(8, r.left + r.width / 2 - 190) + 'px';
  panel.style.top = r.bottom + 4 + 'px';
  panel.style.width = '380px';
  panel.replaceChildren(el('div', 'tb-head', 'LIBRARIES'));
  panel.hidden = false;
  let res;
  try {
    res = await api('/api/libraries');
  } catch (e) {
    panel.append(el('div', 'tb-head', e.message));
    return;
  }
  if (bar.open !== 'switcher') return;
  const action = (label, keys, fn) => {
    const b = el('button', 'tb-item');
    b.type = 'button';
    b.append(el('span', 'tb-tick', ''), el('span', '', label), el('span', 'tb-keys', keyText(keys)));
    b.addEventListener('click', (e) => {
      e.stopPropagation();
      closeMenus();
      fn();
    });
    return b;
  };
  panel.replaceChildren(el('div', 'tb-head', 'LIBRARIES'), ...res.libraries.map(libraryItem), el('div', 'tb-sep'),
    action('New Library…', 'cmdorctrl+shift+n', newLibrary), action('Open Library Folder…', 'cmdorctrl+o', openLibraryFolder));
}

function closeSwitcher() {
  $('tb-lib').setAttribute('aria-expanded', 'false');
  $('tb-panel').style.width = '';
}

$('tb-lib').addEventListener('click', (e) => {
  e.stopPropagation();
  if (bar.open === 'switcher') closeMenus();
  else openSwitcher();
});
document.addEventListener('pointerdown', (e) => {
  if (!bar.open) return;
  if ($('tb-panel').contains(e.target) || $('tb-sub').contains(e.target) || $('tb-menus').contains(e.target) || $('tb-lib').contains(e.target)) return;
  closeMenus();
}, true);
window.addEventListener('blur', closeMenus);
window.addEventListener('resize', closeMenus);

// modal shows a small dialog: a title, what to say, and buttons. Each
// button's run may return false to keep the dialog open.
function modal(title, parts, actions) {
  const d = $('modal');
  const form = el('form');
  form.method = 'dialog';
  form.append(el('h2', '', title), ...parts);
  const row = el('div', 'dialog-actions');
  actions.forEach((a) => {
    const b = el('button', a.primary ? 'btn primary' : 'btn', a.label);
    b.type = a.primary ? 'submit' : 'button';
    b.addEventListener('click', async (e) => {
      e.preventDefault();
      b.disabled = true;
      try {
        if (a.run && (await a.run()) === false) return;
        d.close();
      } finally {
        b.disabled = false;
      }
    });
    row.append(b);
  });
  form.append(row);
  d.replaceChildren(form);
  d.showModal();
  const first = form.querySelector('input');
  if (first) first.focus();
  return d;
}

const para = (text) => el('p', '', text);
const cancel = { label: 'Cancel' };

function facts(rows) {
  const dl = el('dl', 'facts');
  rows.forEach(([k, v]) => dl.append(el('dt', '', k), el('dd', '', v)));
  return dl;
}

async function pickFolder(title) {
  const res = await api('/api/libraries/pick', 'POST', { title });
  return res ? res.folder : '';
}

// switchLibrary asks the server to change library. A running job is stopped
// only once the person agrees. The page then starts again, so nothing of the
// last library's screens is left on show.
async function switchLibrary(path, body, name) {
  const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const answer = await r.json().catch(() => ({}));
  if (r.status === 409 && answer.job) {
    const what = $('job-title').textContent || 'Something';
    const done = answer.job.total > 0 ? ' (' + answer.job.done + ' of ' + answer.job.total + ')' : '';
    modal(name ? 'Switch to ' + name + '?' : 'Stop what is running?', [
      para(what + ' is running in ' + state.lib.name + done + '. Switching stops it. What was finished is kept, and running it again carries on from there.'),
    ], [{ label: 'Keep working here' }, { label: 'Stop and switch', primary: true, run: () => switchLibrary(path, { ...body, stop: true }, name) }]);
    return false;
  }
  if (!r.ok) {
    modal('That did not work', [el('div', 'error', answer.error || 'Something went wrong.')], [{ label: 'OK', primary: true }]);
    return false;
  }
  location.reload();
  return true;
}

function newLibrary() {
  let parent = '';
  const name = el('input');
  name.type = 'text';
  name.value = 'New Library';
  const shown = el('span', 'folder');
  const error = el('div', 'error');
  const where = async () => {
    if (!parent) parent = (await api('/api/libraries')).home;
    shown.title = parent + (parent.includes('\\') ? '\\' : '/') + name.value.trim();
    // Marked left to right: the box is drawn right to left to keep the end in view.
    shown.textContent = '‎' + shown.title + '‎';
  };
  name.addEventListener('input', where);
  const browse = button('Change…', 'btn', async () => {
    try {
      const f = await pickFolder('Choose where the library folder goes');
      if (f) parent = f;
      where();
    } catch (e) {
      error.textContent = e.message;
    }
  });
  const nameField = el('label', 'field');
  nameField.append(el('span', '', 'Name'), name);
  const folderField = el('div', 'field');
  const row = el('div', 'folder-row');
  row.append(shown, browse);
  folderField.append(el('span', '', 'Folder'), row);
  modal('New library', [
    para('A library is one folder: your papers as readable PDFs in papers/, with the citation map beside them. Each library has its own papers, collections and labels.'),
    nameField, folderField, error,
  ], [cancel, {
    label: 'Create and open', primary: true, run: async () => {
      error.textContent = '';
      $('modal').close();
      const ok = await switchLibrary('/api/libraries/new', { name: name.value, parent }, name.value.trim());
      return ok;
    },
  }]);
  name.select();
  where().catch((e) => { error.textContent = e.message; });
}

async function openLibraryFolder() {
  try {
    const f = await pickFolder('Open a library folder');
    if (f) switchLibrary('/api/libraries/open', { folder: f }, '');
  } catch (e) {
    tell(e.message);
  }
}

function forgetLibrary(l) {
  modal('Remove ' + l.name + ' from the list?', [
    para(l.folder + ' is not there any more. Removing it only takes it off this list; nothing is deleted. If the folder comes back, open it again with Open Library Folder.'),
  ], [cancel, {
    label: 'Remove', primary: true, run: async () => {
      await api('/api/libraries/forget', 'POST', { folder: l.folder });
      if (!ready()) showStart(state.lib);
    },
  }]);
}

function closeLibrary() {
  switchLibrary('/api/libraries/close', {}, '');
}

function moveLibrary() {
  modal('Move ' + state.lib.name, [
    para('Choose the folder to move it into. It moves as ' + state.lib.name + ' inside the folder you choose, on the same drive, and opens again from there. Close any assistant using it first.'),
  ], [cancel, {
    label: 'Choose a folder…', primary: true, run: async () => {
      const f = await pickFolder('Move the library into');
      if (!f) return false;
      $('modal').close();
      return switchLibrary('/api/libraries/move', { parent: f }, '');
    },
  }]);
}

// The start screen: no library is open.
async function showStart(lib) {
  document.querySelector('.side').hidden = true;
  document.querySelectorAll('.screen').forEach((s) => { s.hidden = s.id !== 'screen-start'; });
  $('lib-count').textContent = '';
  const problem = $('start-problem');
  problem.hidden = !lib.problem;
  problem.textContent = lib.problem || '';
  let res = { libraries: [] };
  try {
    res = await api('/api/libraries');
  } catch (e) {
    problem.hidden = false;
    problem.textContent = e.message;
  }
  const rows = res.libraries.map((l) => {
    const row = el('div', 'start-row');
    if (l.found) row.append(button(l.name, 'link start-link', () => switchLibrary('/api/libraries/open', { folder: l.folder }, l.name)));
    else row.append(el('span', 'muted', l.name));
    row.append(el('span', 'muted', l.found ? l.folder : l.folder + ', not found'));
    if (!l.found) row.append(button('Remove', 'link', () => forgetLibrary(l)));
    return row;
  });
  $('start-recent').replaceChildren(...(rows.length ? rows : [el('span', 'muted', 'None yet.')]));
}
$('start-new').addEventListener('click', newLibrary);
$('start-open').addEventListener('click', openLibraryFolder);

// Library menu.
async function expandGraph() {
  const stats = await api('/api/library/stats');
  const n = el('input');
  n.type = 'number';
  n.min = '1';
  n.max = '100000';
  n.value = stats.maxNodes;
  const field = el('label', 'field');
  field.append(el('span', '', 'Papers to fetch, at most'), n);
  const error = el('div', 'error');
  modal('Expand the citation graph', [
    para('Follows references outward from everything in ' + stats.name + ', most cited first, and stops once this many papers are fetched. OpenAlex fetches 100 papers for one of its 1,000 free daily credits. Run it again to carry on.'),
    field, error,
  ], [cancel, {
    label: 'Expand', primary: true, run: async () => {
      const max = Number(n.value);
      if (!Number.isInteger(max) || max < 1 || max > 100000) {
        error.textContent = 'Choose a whole number from 1 to 100,000.';
        return false;
      }
      try {
        await startJob('/api/expand', { maxNodes: max });
      } catch (e) {
        error.textContent = e.message;
        return false;
      }
      return true;
    },
  }]);
}

async function libraryStats() {
  const s = await api('/api/library/stats');
  const pct = (n) => (s.works > 0 ? ' (' + Math.round((100 * n) / s.works) + '%)' : '');
  modal(s.name, [facts([
    ['Folder', s.folder || 'A single database file, with no folder'],
    ['Papers', s.works.toLocaleString()],
    ['Added by you', s.seeds.toLocaleString()],
    ['Details fetched', s.fetched.toLocaleString() + pct(s.fetched)],
    ['Known only by ID', s.stubs.toLocaleString() + (s.unresolved ? ', ' + s.unresolved.toLocaleString() + ' unknown to OpenAlex' : '')],
    ['Citations', s.citations.toLocaleString()],
    ['No reference list', s.deadEnds.toLocaleString() + ' fetched papers, where the graph cannot go further'],
    ['PDFs', s.pdfs.toLocaleString() + ', ' + s.searchable.toLocaleString() + ' searchable'],
  ])], [{ label: 'Close', primary: true }]);
}

async function rescan() {
  const r = await api('/api/library/scan', 'POST');
  const lines = [['Paper folders', r.folders.toLocaleString()], ['Files', r.files.toLocaleString() + (r.newFiles ? ', ' + r.newFiles + ' new' : '')]];
  if (r.moved) lines.push(['Moved or renamed', r.moved.toLocaleString() + ', found again']);
  if (r.missing) lines.push(['Gone', r.missing.toLocaleString() + ' folders. The papers stay in your library.']);
  if (r.loose) lines.push(['Not matched', r.loose.toLocaleString() + ' PDFs in papers/ not yet matched to a paper']);
  modal('Papers folder scanned', [facts(lines)], [{ label: 'Close', primary: true }]);
  await loadLibrary();
  loadPapers();
}

// Paper menu: one paper's PDF, and the papers one citation step from it.

// tellWith is tell with a button that does something about what was said.
function tellWith(text, label, fn) {
  tell(text);
  const t = $('toast');
  t.append(' ', button(label, 'link on-accent', () => {
    t.hidden = true;
    fn();
  }));
}

// afterJob runs fn once the job just started has finished.
function afterJob(kind, fn) {
  const once = (j) => {
    job.listeners = job.listeners.filter((f) => f !== once);
    if (j.kind === kind) fn(j);
  };
  job.listeners.push(once);
}

async function downloadPDFs(ids) {
  try {
    await startJob('/api/pdfs', { ids });
  } catch (e) {
    tell(e.message);
    return;
  }
  afterJob('download', (j) => tell(j.error || j.summary));
}

// attachPDF files a PDF the person has under the paper: one they choose, or
// file once they have said it is the right one despite its title.
async function attachPDF(p, file, force) {
  let res;
  try {
    res = await api('/api/papers/' + encodeURIComponent(p.id) + '/attach', 'POST', { file, force });
  } catch (e) {
    tell(e.message);
    return;
  }
  if (!res) return; // no file chosen
  if (res.state === 'unconfirmed') {
    const say = el('p');
    say.append('The title of ', el('i', '', p.title), ' is not on the first pages of ', el('b', '', res.name),
      '. It may be another paper, or a scan with no text.');
    modal('Is this the right file?', [say], [
      { label: 'Choose another file', run: () => { setTimeout(() => attachPDF(p, '', false), 0); } },
      { label: 'Add it anyway', primary: true, run: () => attachPDF(p, res.file, true) },
    ]);
    return;
  }
  if (res.state === 'duplicate') {
    tell(res.reason);
    return;
  }
  tell('Added ' + res.name + ' to this paper.' + (res.pages ? ' Searchable: ' + res.pages + (res.pages === 1 ? ' page.' : ' pages.') : ''));
  await loadLibrary();
  loadPapers();
  if (state.selected === p.id) select(p.id);
}

async function aroundDialog(p) {
  let a;
  try {
    a = await api('/api/papers/' + encodeURIComponent(p.id) + '/around');
  } catch (e) {
    tell(e.message);
    return;
  }
  if (a.local) {
    tell('This is your own document, which OpenAlex does not know. Match it to OpenAlex first.');
    return;
  }
  const credits = (n, per) => {
    if (n <= 0) return 'free';
    const c = Math.ceil(n / per);
    return c + (c === 1 ? ' credit' : ' credits');
  };
  const box = (on) => {
    const b = el('input');
    b.type = 'checkbox';
    b.checked = on;
    return b;
  };
  const option = (check, title, parts, cost) => {
    const label = el('label', 'opt');
    const text = el('span', 'opt-text');
    text.append(el('b', '', title), ...parts);
    label.append(check, text, cost);
    return label;
  };

  const refs = box(true);
  const follow = box(false);
  const refNote = el('span', 'muted');
  const refCost = el('span', 'cost');
  // How many references a run would fetch; -1 while its own list is not here.
  const toFetch = () => (!a.fetched ? -1 : follow.checked ? a.unfetched : a.untitled);
  const citing = box(a.citedBy > a.here);
  const n = el('input');
  n.type = 'number';
  n.min = '1';
  n.max = '1000';
  n.value = String(Math.min(100, Math.max(1, a.citedBy)));
  n.className = 'num';
  const citeNote = el('span', 'muted');
  citeNote.append(a.citedBy.toLocaleString() + (a.citedBy === 1 ? ' paper' : ' papers') + ' worldwide, ' +
    a.here.toLocaleString() + ' here. Most cited first, up to ', n);
  const citeCost = el('span', 'cost');
  const error = el('div', 'error');

  const update = () => {
    const f = toFetch();
    refNote.textContent = f < 0 ? 'Its own reference list is fetched first.'
      : a.cites.toLocaleString() + (a.cites === 1 ? ' paper, ' : ' papers, ') + f.toLocaleString() + ' not fetched yet';
    refs.disabled = f === 0;
    if (f === 0) refs.checked = false;
    refCost.textContent = f < 0 ? '1 credit, then 1 per 100' : credits(f, 100);
    citing.disabled = a.citedBy === 0;
    if (a.citedBy === 0) citing.checked = false;
    citeCost.textContent = credits(Math.min(Number(n.value) || 0, a.citedBy), 200);
    const count = (refs.checked ? Math.max(0, f) : 0) + (citing.checked ? Math.min(Number(n.value) || 0, a.citedBy) : 0);
    const b = $('modal').querySelector('.dialog-actions .primary');
    if (b) {
      b.textContent = f < 0 && refs.checked ? 'Bring them in' : 'Bring in ' + count.toLocaleString() + (count === 1 ? ' paper' : ' papers');
      b.disabled = !(refs.checked || citing.checked) || (f >= 0 && count === 0);
    }
  };
  [refs, follow, citing].forEach((b) => b.addEventListener('change', update));
  n.addEventListener('input', update);

  const followRow = el('label', 'opt-plain');
  followRow.append(follow, el('span', 'muted', 'Also record what each of them cites. Adds their references to your list as papers not fetched yet, often thousands.'));
  const go = {
    label: 'Bring them in', primary: true, run: async () => {
      const max = Number(n.value);
      if (citing.checked && (!Number.isInteger(max) || max < 1 || max > 1000)) {
        error.textContent = 'Choose a whole number from 1 to 1,000.';
        return false;
      }
      try {
        await startJob('/api/papers/' + encodeURIComponent(a.id) + '/around',
          { references: refs.checked, citing: citing.checked ? max : 0, follow: follow.checked });
      } catch (e) {
        error.textContent = e.message;
        return false;
      }
      afterJob('around', (j) => {
        if (j.error) tell(j.error);
        else tellWith(j.summary + '.', 'Show them on the map', () => show('map', a.id));
      });
      return true;
    },
  };
  modal('Bring in papers around this one', [
    el('p', 'around-title', a.title + (a.by ? ' (' + a.by + ')' : '')),
    option(refs, 'What it cites', [refNote], refCost),
    option(citing, 'What cites it', [citeNote], citeCost),
    followRow,
    el('p', 'muted small', 'They come in with their titles, authors and years, and appear on the citation map around this paper. Nothing else in the library grows. OpenAlex gives 1,000 free credits a day.'),
    error,
  ], [cancel, go]);
  update();
}

// Help menu.
function shortcuts() {
  const list = el('div', 'keys-list');
  bar.menus.forEach((m) => {
    const rows = m.items.filter((it) => it.keys || it.shown);
    if (rows.length === 0) return;
    list.append(el('h3', '', m.name.toUpperCase()));
    rows.forEach((it) => {
      const row = el('div', 'keys-row');
      row.append(el('span', '', it.label), el('span', '', it.shown || keyText(it.keys)));
      list.append(row);
    });
  });
  modal('Keyboard shortcuts', [list], [{ label: 'Close', primary: true }]);
}

function about() {
  const logo = $('titlebar').querySelector('.tb-logo svg').cloneNode(true);
  logo.setAttribute('class', 'about-logo');
  modal('Filiation', [logo,
    para('A citation graph and retrieval engine you run on your own computer. It follows the reference lists of the papers you give it, keeps the open-access PDFs, and lets you search everything you have read.'),
    facts([['Version', bar.version || 'dev'], ['Library', ready() ? state.lib.folder || state.lib.name : 'None open'], ['Licence', 'Apache-2.0'], ['Data', 'OpenAlex, CC0']]),
  ], [{ label: 'Close', primary: true }]);
}

// Edit menu: the text commands act on the text box or selection, as the
// system's own Edit menu would.
async function editCommand(id) {
  const box = document.activeElement;
  const typing = box && (box.tagName === 'INPUT' || box.tagName === 'TEXTAREA');
  if (id === 'edit.paste') {
    if (!typing) return;
    const text = window.runtime ? await window.runtime.ClipboardGetText() : await navigator.clipboard.readText();
    box.setRangeText(text, box.selectionStart, box.selectionEnd, 'end');
    box.dispatchEvent(new Event('input', { bubbles: true }));
    return;
  }
  if (id === 'edit.selectAll' && !typing && onScreen() === 'papers') {
    pickAll();
    return;
  }
  document.execCommand({ 'edit.undo': 'undo', 'edit.redo': 'redo', 'edit.cut': 'cut', 'edit.copy': 'copy', 'edit.selectAll': 'selectAll' }[id]);
}

async function copy(text, said) {
  try {
    await copyText(text);
    tell(said);
  } catch (e) {
    tell('Could not copy: ' + e.message);
  }
}

// View menu.
const ZOOMS = [0.8, 0.9, 1, 1.1, 1.25, 1.5];
function zoomTo(z) {
  document.documentElement.style.zoom = z === 1 ? '' : String(z);
  setPref('zoom', z === 1 ? '' : String(z));
}
function zoomBy(by) {
  const now = Number(document.documentElement.style.zoom) || 1;
  const at = ZOOMS.findIndex((z) => Math.abs(z - now) < 0.01);
  zoomTo(ZOOMS[Math.min(ZOOMS.length - 1, Math.max(0, (at < 0 ? 2 : at) + by))]);
}

function togglePane(selector, key) {
  const pane = document.querySelector(selector);
  pane.classList.toggle('closed');
  setPref(key, pane.classList.contains('closed') ? '1' : '');
  if (key === 'side' && window.drawWhole) drawWhole();
}

async function fullScreen() {
  if (window.runtime) {
    if (await window.runtime.WindowIsFullscreen()) window.runtime.WindowUnfullscreen();
    else window.runtime.WindowFullscreen();
  } else if (document.fullscreenElement) {
    document.exitFullscreen();
  } else {
    document.documentElement.requestFullscreen();
  }
}

// Go menu: Back and Forward walk the screens shown, as a browser walks pages.
const showScreen = show;
// eslint-disable-next-line no-global-assign
show = function (name, arg) {
  const from = onScreen();
  if (!bar.replaying && from && from !== 'start' && from !== 'reader' && name !== 'reader' && (from !== name || arg !== undefined)) {
    bar.history.push({ name: from, arg: from === 'map' ? mapState.id : undefined, paper: state.selected });
    if (bar.history.length > 50) bar.history.shift();
    bar.ahead = [];
  }
  showScreen(name, arg);
};

function walk(back) {
  if (onScreen() === 'reader') {
    $('reader-back').click();
    return;
  }
  const from = back ? bar.history : bar.ahead;
  const to = back ? bar.ahead : bar.history;
  const step = from.pop();
  if (!step) return;
  to.push({ name: onScreen(), arg: onScreen() === 'map' ? mapState.id : undefined, paper: state.selected });
  bar.replaying = true;
  try {
    showScreen(step.name, step.arg);
    if (step.name === 'papers' && step.paper && step.paper !== state.selected) select(step.paper);
  } finally {
    bar.replaying = false;
  }
}

// Go to Paper: find one by title or fil ID as you type, as VS Code's Go to File.
const quick = { asked: 0, rows: [], hot: 0, timer: 0 };
function openQuick() {
  $('quick').hidden = false;
  $('quick-input').value = '';
  $('quick-list').replaceChildren(el('div', 'note', 'Type a few words of a title, or a fil ID.'));
  $('quick-input').focus();
}
function closeQuick() {
  $('quick').hidden = true;
}
async function findQuick(text) {
  const asked = ++quick.asked;
  if (!text) {
    $('quick-list').replaceChildren(el('div', 'note', 'Type a few words of a title, or a fil ID.'));
    quick.rows = [];
    return;
  }
  let found = [];
  try {
    if (/^F[0-9A-Z]{8}$/i.test(text)) {
      const p = await api('/api/papers/' + encodeURIComponent(text.toUpperCase()));
      found = [{ id: p.id, title: p.title, by: p.authors || '' }];
    }
  } catch (e) {
    found = [];
  }
  if (found.length === 0) {
    try {
      const res = await api('/api/papers?' + new URLSearchParams({ title: text, show: 'all', sort: 'citedworld' }));
      found = res.papers.slice(0, 12);
    } catch (e) {
      if (asked === quick.asked) $('quick-list').replaceChildren(el('div', 'note error', e.message));
      return;
    }
  }
  if (asked !== quick.asked) return;
  quick.rows = found;
  quick.hot = 0;
  if (found.length === 0) {
    $('quick-list').replaceChildren(el('div', 'note', 'No paper in this library has that title.'));
    return;
  }
  $('quick-list').replaceChildren(...found.map((p, i) => {
    const b = el('button', 'quick-row' + (i === 0 ? ' hot' : ''));
    b.type = 'button';
    b.setAttribute('role', 'option');
    b.append(el('span', 'title-text', p.title), el('span', 'muted', p.by || ''));
    b.addEventListener('click', () => goTo(p.id));
    b.addEventListener('mouseenter', () => hotQuick(i));
    return b;
  }));
}
function hotQuick(i) {
  quick.hot = i;
  $('quick-list').querySelectorAll('.quick-row').forEach((b, j) => b.classList.toggle('hot', j === i));
}
function goTo(id) {
  closeQuick();
  openPaper(id);
}
$('quick-input').addEventListener('input', (e) => {
  clearTimeout(quick.timer);
  quick.timer = setTimeout(() => findQuick(e.target.value.trim()), 120);
});
$('quick-input').addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    e.preventDefault();
    closeQuick();
  } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault();
    if (quick.rows.length) hotQuick((quick.hot + (e.key === 'ArrowDown' ? 1 : quick.rows.length - 1)) % quick.rows.length);
  } else if (e.key === 'Enter') {
    e.preventDefault();
    if (quick.rows[quick.hot]) goTo(quick.rows[quick.hot].id);
  }
});
$('quick-input').addEventListener('blur', () => setTimeout(() => {
  if (!$('quick').contains(document.activeElement)) closeQuick();
}, 150));

function paperPicker(kind) {
  const ids = state.picked.size > 1 ? pickedIDs() : [state.selected];
  const r = $('tb-lib').getBoundingClientRect();
  picker({ left: r.left, bottom: r.bottom }, kind, ids);
}

// run does what a menu item says, however it was asked for.
async function run(id) {
  if (!enabled(id)) return;
  const p = selectedPaper();
  try {
    switch (id) {
      case 'lib.new': return newLibrary();
      case 'lib.open': return openLibraryFolder();
      case 'lib.recent': return openSwitcher();
      case 'lib.close': return closeLibrary();
      case 'lib.reveal': return await api('/api/library/folder', 'POST');
      case 'lib.scan': return await rescan();
      case 'lib.stats': return await libraryStats();
      case 'lib.move': return moveLibrary();
      case 'paper.add': return $('add-paper').click();
      case 'pdfs.import':
        show('inbox');
        return $('inbox-add').click();
      case 'zotero':
        show('inbox');
        return document.querySelector('.zotero-entry')?.click();
      case 'export.graphml': return $('map-export').click();
      case 'app.quit': return window.runtime ? window.runtime.Quit() : window.close();
      case 'edit.undo': case 'edit.redo': case 'edit.cut': case 'edit.copy': case 'edit.paste': case 'edit.selectAll':
        return await editCommand(id);
      case 'paper.cite': return await copy(p.citation || p.title, 'Citation copied');
      case 'paper.copyId': return await copy(p.id, 'Copied ' + p.id);
      case 'find.library':
        if (onScreen() !== 'papers') show('papers');
        $('filter').focus();
        return $('filter').select();
      case 'find.text':
        show('search');
        return $('q').focus();
      case 'view.papers': return document.querySelector('.nav[data-screen="papers"]').click();
      case 'view.favourites': return $('favourites').click();
      case 'view.search': return show('search');
      case 'view.map': return show('map');
      case 'view.inbox': return show('inbox');
      case 'view.assistants': return show('assistants');
      case 'view.sidebar': return togglePane('.side', 'side');
      case 'view.details': return togglePane('#detail', 'detail');
      case 'view.mapFilters':
        if (onScreen() !== 'map') show('map');
        if (mapState.mode !== 'whole') document.querySelector('[data-map-mode="whole"]').click();
        return toggleMapFilters(onScreen() === 'map' && !$('mf-panel').hidden ? false : true);
      case 'view.zoomIn': return zoomBy(1);
      case 'view.zoomOut': return zoomBy(-1);
      case 'view.zoomReset': return zoomTo(1);
      case 'view.fullscreen': return await fullScreen();
      case 'graph.expand': return await expandGraph();
      case 'pdfs.fetch': return $('fetch-pdfs').click();
      case 'job.stop': return await api('/api/job/stop', 'POST');
      case 'collection.new':
        if (document.querySelector('.side').classList.contains('closed')) togglePane('.side', 'side');
        return $('new-collection').click();
      case 'label.new':
        if (document.querySelector('.side').classList.contains('closed')) togglePane('.side', 'side');
        return $('new-label').click();
      case 'go.back': return walk(true);
      case 'go.forward': return walk(false);
      case 'go.paper': return openQuick();
      case 'go.path':
        show('map', p.id);
        return setTimeout(() => document.querySelector('.path-form input')?.focus(), 300);
      case 'paper.read': return openReader(p.id, 1, []);
      case 'paper.open': return await api('/api/papers/' + encodeURIComponent(p.id) + '/open', 'POST');
      case 'paper.download': return downloadPDFs(state.picked.size > 1 ? pickedIDs() : [p.id]);
      case 'paper.around': return aroundDialog(p);
      case 'paper.attach': return attachPDF(p, '', false);
      case 'paper.map': return show('map', p.id);
      case 'paper.favourite': return setFavourite(state.picked.size > 1 ? pickedIDs() : [p.id], !p.favourite);
      case 'paper.collect': return paperPicker('collection');
      case 'paper.label': return paperPicker('label');
      case 'paper.folder': return await api('/api/papers/' + encodeURIComponent(p.id) + '/folder', 'POST');
      case 'help.docs': return openOutside(DOCS);
      case 'help.keys': return shortcuts();
      case 'help.assistant': return show('assistants');
      case 'help.issue': return openOutside(ISSUES);
      case 'help.licence': return openOutside(LICENCE);
      case 'help.about': return about();
    }
  } catch (e) {
    tell(e.message);
  }
  return undefined;
}

// Shortcuts. The text commands are left to the browser, which already does
// them in text boxes; everything else is the page's.
document.addEventListener('keydown', (e) => {
  if (e.key === 'Alt' && !mac()) {
    $('titlebar').classList.add('keys');
    return;
  }
  if (bar.open && bar.open !== 'switcher') {
    menuKeys(e);
    return;
  }
  if ($('modal').open) return;
  const pressed = keyOf(e);
  if (bar.chord) {
    const chord = bar.chord;
    bar.chord = '';
    if (chord === 'cmdorctrl+k' && pressed === 'cmdorctrl+s') {
      e.preventDefault();
      run('help.keys');
      return;
    }
  }
  if (pressed === 'cmdorctrl+k') {
    bar.chord = pressed;
    e.preventDefault();
    return;
  }
  // Alt and a menu's first letter opens it, as on Windows.
  if (!mac() && e.altKey && !e.ctrlKey && !e.shiftKey && /^[a-z]$/i.test(e.key)) {
    const i = bar.menus.findIndex((m) => m.name[0].toLowerCase() === e.key.toLowerCase());
    if (i >= 0) {
      e.preventDefault();
      openMenu(i, true);
      return;
    }
  }
  const id = bar.keys.get(pressed);
  if (!id) return;
  e.preventDefault();
  run(id);
}, true);
document.addEventListener('keyup', (e) => {
  if (e.key === 'Alt') $('titlebar').classList.remove('keys');
});

// menuKeys moves through an open menu with the arrow keys.
function menuKeys(e) {
  const items = [...$('tb-panel').querySelectorAll('.tb-item')];
  const at = bar.menus.findIndex((m) => m.name === bar.open);
  switch (e.key) {
    case 'Escape': closeMenus(); break;
    case 'ArrowDown': hotItem(nextItem(bar.hot, 1)); break;
    case 'ArrowUp': hotItem(nextItem(bar.hot < 0 ? 0 : bar.hot, -1)); break;
    case 'ArrowRight': openMenu((at + 1) % bar.menus.length, true); break;
    case 'ArrowLeft': openMenu((at + bar.menus.length - 1) % bar.menus.length, true); break;
    case 'Enter': case ' ':
      if (items[bar.hot]) items[bar.hot].click();
      break;
    case 'Alt': $('titlebar').classList.add('keys'); return;
    default: {
      const i = bar.menus.findIndex((m) => m.name[0].toLowerCase() === e.key.toLowerCase());
      if (e.altKey && i >= 0) openMenu(i, true);
      else return;
    }
  }
  e.preventDefault();
  e.stopPropagation();
}

// The window's own buttons, on Windows, where the page draws the frame.
async function windowButtons() {
  if (bar.platform !== 'windows') return;
  $('tb-ctl').hidden = false;
  const icon = async () => {
    const max = await window.runtime.WindowIsMaximised();
    $('tb-max-icon').setAttribute('d', max ? 'M2.5.5h7v7M.5 2.5h7v7h-7z' : 'M.5.5h9v9h-9z');
    $('tb-max').setAttribute('aria-label', max ? 'Restore' : 'Maximise');
    $('tb-max').title = max ? 'Restore' : 'Maximise';
  };
  $('tb-min').addEventListener('click', () => window.runtime.WindowMinimise());
  $('tb-max').addEventListener('click', () => window.runtime.WindowToggleMaximise());
  $('tb-close').addEventListener('click', () => window.runtime.Quit());
  // A double click on the bar itself maximises, as on any window's title bar.
  $('titlebar').addEventListener('dblclick', (e) => {
    if (e.target.closest('button')) return;
    window.runtime.WindowToggleMaximise();
  });
  window.addEventListener('resize', icon);
  icon();
}

async function startTitleBar() {
  bar.platform = await platform();
  document.body.dataset.platform = bar.platform;
  if (pref('side')) document.querySelector('.side').classList.add('closed');
  if (pref('detail')) $('detail').classList.add('closed');
  if (pref('zoom')) document.documentElement.style.zoom = pref('zoom');
  if (mac() && window.runtime) window.runtime.EventsOn('menu', run);
  windowButtons();
  try {
    await loadMenus();
  } catch (e) {
    $('tb-menus').replaceChildren();
  }
}
