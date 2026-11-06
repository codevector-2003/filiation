// Organising the library: Favourites, the collection tree, labels, picking
// several papers, the right-click menu and dragging. Collections and labels
// only group papers; nothing here moves a PDF or deletes a paper.
'use strict';

const LABEL_COLOURS = { blue: '#1F5F8B', amber: '#B7791F', green: '#2F7D6B', pink: '#B4476B', purple: '#5B4A8A', grey: '#6B6F76' };
const STAR = 'M12 4l2.5 5.2 5.7.8-4.1 4 1 5.7-5.1-2.7-5.1 2.7 1-5.7-4.1-4 5.7-.8z';
const FOLDER = 'M3 7h6l2 2h12v10H3z';
const STACK = 'M4 7h16M4 12h16M4 17h16';

const treeOpen = new Set(remembered('fil.open'));

function remembered(key) {
  try {
    return JSON.parse(localStorage.getItem(key)) || [];
  } catch (e) {
    return [];
  }
}

function remember(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch (e) {
    // Only a convenience: the tree opens closed next time.
  }
}

function icon(path, cls) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  if (cls) svg.setAttribute('class', cls);
  const d = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  d.setAttribute('d', path);
  svg.append(d);
  return svg;
}

function dot(colour) {
  const d = el('span', 'dot');
  d.style.background = LABEL_COLOURS[colour] || LABEL_COLOURS.grey;
  return d;
}

const collections = () => (state.lib ? state.lib.collections : []);
const collectionById = (id) => collections().find((c) => c.id === id);
function labelById(id) {
  return state.lib ? state.lib.labels.find((l) => l.id === id) : undefined;
}

// pathOf names a collection with the ones it sits in: Thesis › Chapter 2.
function pathOf(id) {
  const names = [];
  for (let c = collectionById(id); c; c = collectionById(c.parent)) names.unshift(c.name);
  return names.join(' › ') || 'A deleted collection';
}

// inside is a collection and every one in it.
function inside(id) {
  const out = [id];
  for (let i = 0; i < out.length; i++) collections().forEach((c) => { if (c.parent === out[i]) out.push(c.id); });
  return out;
}

// Where the list is: the whole library, Favourites, or one collection.

function placeChosen() {
  return state.collection !== 0 || state.favourites || state.labels.length > 0;
}

function choosePlace(collection, favourites) {
  state.collection = collection;
  state.favourites = favourites;
  show('papers');
}

function toggleLabelFilter(id) {
  state.labels = state.labels.includes(id) ? state.labels.filter((l) => l !== id) : [...state.labels, id];
  show('papers');
}

function markPlace() {
  renderViews();
  const papers = !$('screen-papers').hidden;
  document.querySelector('.nav[data-screen="papers"]').classList.toggle('on', papers && !state.collection && !state.favourites);
  $('favourites').classList.toggle('on', papers && state.favourites);
  document.querySelectorAll('.tree-row').forEach((r) => {
    const on = Number(r.dataset.collection) === state.collection;
    r.classList.toggle('on', on);
    r.setAttribute('aria-selected', on ? 'true' : 'false');
  });
  document.querySelectorAll('.label-row').forEach((b) => {
    const on = state.labels.includes(Number(b.dataset.label));
    b.classList.toggle('on', on);
    b.setAttribute('aria-pressed', on ? 'true' : 'false');
  });
}

function emptyPlace() {
  if (state.favourites && !state.title && !state.labels.length) return 'No favourites yet. Star a paper to keep it here.';
  const c = collectionById(state.collection);
  if (c && c.works === 0) {
    return c.folder ? 'This folder holds no papers yet.'
      : 'No papers here yet. Drag papers onto this collection, or right-click a paper and choose Add to collection.';
  }
  return 'No papers match.';
}

// The sidebar.

function renderOrganised() {
  $('count-favourites').textContent = state.lib.favourites.toLocaleString();
  renderTree();
  renderLabels();
  markPlace();
}

function renderTree(editing) {
  const kids = new Map();
  collections().forEach((c) => kids.set(c.parent, [...(kids.get(c.parent) || []), c]));
  const rows = [];
  const walk = (parent, depth) => {
    (kids.get(parent) || []).forEach((c) => {
      rows.push(treeRow(c, depth, kids.has(c.id)));
      if (editing && editing.parent === c.id && editing.id === 0) rows.push(nameRow(editing, depth + 1));
      if (treeOpen.has(c.id)) walk(c.id, depth + 1);
    });
  };
  if (editing && editing.parent === 0 && editing.id === 0) rows.push(nameRow(editing, 0));
  walk(0, 0);
  if (editing && editing.id !== 0) {
    const at = rows.findIndex((r) => Number(r.dataset.collection) === editing.id);
    if (at >= 0) rows[at] = nameRow(editing, Number(rows[at].dataset.depth));
  }
  if (rows.length === 0) rows.push(el('div', 'side-note', 'Make one with +, or make folders in papers/.'));
  $('collections').replaceChildren(...rows);
  markPlace();
  const input = $('collections').querySelector('input');
  if (input) {
    input.focus();
    input.select();
  }
}

function treeRow(c, depth, parent) {
  const row = el('div', 'tree-row');
  row.setAttribute('role', 'treeitem');
  row.setAttribute('aria-level', depth + 1);
  row.dataset.collection = c.id;
  row.dataset.depth = depth;
  row.style.paddingLeft = depth * 16 + 'px';
  if (!c.folder) row.dataset.drop = 'collection';
  if (parent) {
    row.setAttribute('aria-expanded', treeOpen.has(c.id) ? 'true' : 'false');
    const t = button(treeOpen.has(c.id) ? '▾' : '▸', 'twist', () => toggleOpen(c.id));
    t.setAttribute('aria-label', (treeOpen.has(c.id) ? 'Collapse ' : 'Expand ') + c.name);
    t.tabIndex = -1;
    row.append(t);
  } else {
    row.append(el('span', 'twist'));
  }
  const pick = el('button', 'tree-pick');
  pick.type = 'button';
  pick.append(icon(c.folder ? FOLDER : STACK, c.folder ? 'from-folder' : ''), el('span', 'tree-name', c.name));
  if (c.zotero) {
    const z = el('span', 'zmark', 'Z');
    z.title = 'From Zotero';
    pick.append(z);
  }
  pick.append(el('span', 'count', c.works.toLocaleString()));
  pick.title = c.folder ? pathOf(c.id) + ' (a folder in papers/)' : pathOf(c.id);
  pick.addEventListener('click', () => choosePlace(state.collection === c.id ? 0 : c.id, false));
  pick.addEventListener('keydown', (e) => treeKey(e, c, parent));
  row.append(pick);
  row.addEventListener('contextmenu', (e) => {
    e.preventDefault();
    collectionMenu(c, e.clientX, e.clientY);
  });
  if (!c.folder) row.addEventListener('pointerdown', (e) => dragCollection(e, c));
  return row;
}

function toggleOpen(id, to) {
  const now = to === undefined ? !treeOpen.has(id) : to;
  if (now) treeOpen.add(id);
  else treeOpen.delete(id);
  remember('fil.open', [...treeOpen]);
  renderTree();
  const back = document.querySelector('.tree-row[data-collection="' + id + '"] .tree-pick');
  if (back) back.focus();
}

function treeKey(e, c, parent) {
  const picks = [...document.querySelectorAll('.tree-pick')];
  const at = picks.indexOf(e.target);
  const go = (i) => { if (picks[i]) picks[i].focus(); };
  if (e.key === 'ArrowDown') go(at + 1);
  else if (e.key === 'ArrowUp') go(at - 1);
  else if (e.key === 'ArrowRight' && parent && !treeOpen.has(c.id)) toggleOpen(c.id, true);
  else if (e.key === 'ArrowLeft' && parent && treeOpen.has(c.id)) toggleOpen(c.id, false);
  else if (e.key === 'ArrowLeft' && c.parent) document.querySelector('.tree-row[data-collection="' + c.parent + '"] .tree-pick').focus();
  else if (e.key === 'F2' && !c.folder) renderTree({ id: c.id, parent: c.parent, name: c.name });
  else if (e.key === 'Delete' && !c.folder) confirmDelete(c, e.target);
  else if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
    const r = e.target.getBoundingClientRect();
    collectionMenu(c, r.left + 24, r.bottom);
  } else return;
  e.preventDefault();
}

// nameRow is the box a new collection's name, or a new name, is typed in.
function nameRow(editing, depth) {
  const row = el('div', 'tree-row editing');
  row.style.paddingLeft = depth * 16 + 22 + 'px';
  const input = el('input', 'tree-input');
  input.value = editing.name || '';
  input.setAttribute('aria-label', editing.id ? 'New name for ' + editing.name : 'Name of the new collection');
  let done = false;
  const finish = async (keep) => {
    if (done) return;
    done = true;
    const name = input.value.trim();
    if (!keep || !name || name === editing.name) {
      renderTree();
      return;
    }
    try {
      if (editing.id) {
        await api('/api/collections/' + editing.id + '/rename', 'POST', { name });
      } else {
        const res = await api('/api/collections', 'POST', { name, parent: editing.parent });
        if (editing.parent) reveal(editing.parent);
        if (editing.file) await api('/api/collections/' + res.id + '/papers', 'POST', { papers: editing.file, on: true });
      }
    } catch (e) {
      tell(e.message);
    }
    await changed(!!editing.file);
  };
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') finish(true);
    else if (e.key === 'Escape') finish(false);
    e.stopPropagation();
  });
  input.addEventListener('blur', () => finish(true));
  row.append(input);
  return row;
}

function renderLabels() {
  const ls = state.lib.labels;
  $('labels').replaceChildren(...(ls.length === 0 ? [el('div', 'side-note', 'Make one with +.')] : ls.map((l) => {
    const b = el('button', 'label-row');
    b.type = 'button';
    b.dataset.label = l.id;
    b.dataset.drop = 'label';
    b.append(dot(l.colour), el('span', 'tree-name', l.name), el('span', 'count', l.works.toLocaleString()));
    b.addEventListener('click', () => toggleLabelFilter(l.id));
    b.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      labelMenu(l, e.clientX, e.clientY);
    });
    b.addEventListener('keydown', (e) => {
      if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
        e.preventDefault();
        const r = b.getBoundingClientRect();
        labelMenu(l, r.left + 24, r.bottom);
      }
    });
    return b;
  })));
}

// Papers: the star, labels on a row, and what changes them.

function labelChip(id) {
  const l = labelById(id);
  if (!l) return null;
  const c = el('span', 'chip label-chip');
  c.append(dot(l.colour), document.createTextNode(l.name));
  return c;
}

function starIcon(on) {
  const svg = icon(STAR, on ? 'star-icon on' : 'star-icon');
  return svg;
}

function starButton(p) {
  const b = el('button', 'star-btn');
  b.type = 'button';
  b.tabIndex = -1;
  b.setAttribute('aria-pressed', p.favourite ? 'true' : 'false');
  b.setAttribute('aria-label', (p.favourite ? 'Remove from favourites: ' : 'Add to favourites: ') + p.title);
  b.append(starIcon(p.favourite));
  b.addEventListener('click', (e) => {
    e.stopPropagation();
    setFavourite([p.id], !p.favourite);
  });
  return b;
}

function favouriteButton(p) {
  const b = el('button', 'btn small fav-btn');
  b.type = 'button';
  b.setAttribute('aria-pressed', p.favourite ? 'true' : 'false');
  b.append(starIcon(p.favourite), document.createTextNode(p.favourite ? 'Favourite' : 'Add to favourites'));
  b.addEventListener('click', () => setFavourite([p.id], !p.favourite));
  return b;
}

// filedIn is the selected paper's collections and labels, each with a way off.
function filedIn(p) {
  const ids = [p.id];
  const part = (title, chips, add, addLabel) => {
    const box = el('div', 'filed');
    box.append(el('h2', '', title));
    const list = el('div', 'filed-list');
    list.append(...chips);
    const a = button(addLabel, 'add-chip', (e) => add(e.currentTarget));
    list.append(a);
    box.append(list);
    return box;
  };
  const cs = p.collections.filter(collectionById).map((id) => {
    const c = collectionById(id);
    const chip = el('span', 'filed-chip');
    chip.append(document.createTextNode(pathOf(id)));
    if (!c.folder) {
      const x = button('×', 'chip-x', () => fileIn(id, ids, false));
      x.setAttribute('aria-label', 'Remove from ' + pathOf(id));
      chip.append(x);
    }
    return chip;
  });
  const ls = p.labels.filter(labelById).map((id) => {
    const l = labelById(id);
    const chip = el('span', 'filed-chip label');
    chip.append(dot(l.colour), document.createTextNode(l.name));
    const x = button('×', 'chip-x', () => setLabel(id, ids, false));
    x.setAttribute('aria-label', 'Take off ' + l.name);
    chip.append(x);
    return chip;
  });
  return [
    part('COLLECTIONS', cs, (at) => picker(at, 'collection', ids), 'Add to collection'),
    part('LABELS', ls, (at) => picker(at, 'label', ids), 'Add label'),
  ];
}

// paperState is what is known of a paper's filing: its row, or the paper open
// on the right.
function paperState(id) {
  return state.rows.get(id) || (state.paper && state.paper.id === id ? state.paper : null);
}

function patch(ids, fn) {
  ids.forEach((id) => {
    const p = state.rows.get(id);
    if (p) fn(p);
    if (state.paper && state.paper.id === id && state.paper !== p) fn(state.paper);
  });
}

// changed brings the sidebar up to date, then the list: in place when the
// papers shown cannot change, and from the start when they can.
async function changed(listToo) {
  await loadLibrary();
  if (listToo && placeChosen()) {
    loadPapers();
    return;
  }
  paperRows().forEach((r) => {
    const p = state.rows.get(r.dataset.id);
    if (p) r.replaceWith(rowFor(p));
  });
  if (state.picked.size > 1) showPicked();
  else if (state.paper) renderPaper(state.paper);
}

async function organise(path, body, ids, fn) {
  try {
    await api(path, 'POST', body);
    patch(ids, fn);
  } catch (e) {
    tell(e.message);
  }
  await changed(true);
}

function setFavourite(ids, on) {
  return organise('/api/favourites', { papers: ids, on }, ids, (p) => { p.favourite = on; });
}

function fileIn(collection, ids, on) {
  return organise('/api/collections/' + collection + '/papers', { papers: ids, on }, ids, (p) => {
    p.collections = on ? [...new Set([...p.collections, collection])] : p.collections.filter((c) => c !== collection);
  });
}

function setLabel(label, ids, on) {
  return organise('/api/labels/' + label + '/papers', { papers: ids, on }, ids, (p) => {
    p.labels = on ? [...new Set([...p.labels, label])] : p.labels.filter((l) => l !== label);
  });
}

// tell says briefly what went wrong, and goes by itself.
let toldTimer;
function tell(text) {
  const t = $('toast');
  t.textContent = text.charAt(0).toUpperCase() + text.slice(1);
  t.hidden = false;
  clearTimeout(toldTimer);
  toldTimer = setTimeout(() => { t.hidden = true; }, 6000);
}

// Several papers at once.

function clickRow(id, e) {
  if (e.ctrlKey || e.metaKey) {
    togglePick(id);
  } else if (e.shiftKey && state.anchor) {
    const ids = paperRows().map((r) => r.dataset.id);
    const a = ids.indexOf(state.anchor);
    const b = ids.indexOf(id);
    if (a < 0 || b < 0) return select(id);
    state.picked = new Set(ids.slice(Math.min(a, b), Math.max(a, b) + 1));
    state.selected = id;
    showPicked();
  } else {
    select(id);
  }
}

function togglePick(id) {
  if (state.picked.size === 0 && state.selected) state.picked.add(state.selected);
  if (state.picked.has(id)) state.picked.delete(id);
  else state.picked.add(id);
  if (state.picked.size === 1) {
    select([...state.picked][0]);
    return;
  }
  if (state.picked.size === 0) {
    select(id);
    return;
  }
  state.selected = id;
  state.anchor = id;
  showPicked();
}

function pickAll() {
  state.picked = new Set(paperRows().map((r) => r.dataset.id));
  if (state.picked.size > 1) showPicked();
}

function pickedIDs() {
  return state.picked.size > 1 ? [...state.picked] : state.selected ? [state.selected] : [];
}

function markRow(r) {
  const many = state.picked.size > 1;
  const on = many ? state.picked.has(r.dataset.id) : r.dataset.id === state.selected;
  r.classList.toggle('on', on);
  r.querySelector('.pick').checked = on && many;
  const main = r.querySelector('.row-main');
  if (on) main.setAttribute('aria-current', 'true');
  else main.removeAttribute('aria-current');
}

function showPicked() {
  const many = state.picked.size > 1;
  $('rows').classList.toggle('picking', many);
  paperRows().forEach(markRow);
  $('sel-bar').hidden = !many;
  document.querySelector('#screen-papers .toolbar').hidden = many;
  if (!many) return;
  const ids = [...state.picked];
  $('sel-count').textContent = ids.length + ' selected';
  const known = ids.map((id) => state.rows.get(id)).filter(Boolean);
  const allFav = known.length > 0 && known.every((p) => p.favourite);
  $('sel-fav').textContent = allFav ? 'Remove from favourites' : 'Favourite';
  state.askedPaper++;
  state.paper = null;
  renderPicked(known, ids.length);
}

function renderPicked(known, n) {
  const shared = (key) => known.reduce((acc, p) => acc.filter((x) => p[key].includes(x)), known.length ? known[0][key] : []);
  const cs = shared('collections').filter(collectionById);
  const ls = shared('labels').filter(labelById);
  const part = (title, items) => {
    const box = el('div', 'filed');
    box.append(el('h2', '', title));
    const list = el('div', 'filed-list');
    list.append(...(items.length ? items : [el('span', 'muted', 'None')]));
    box.append(list);
    return box;
  };
  $('detail').replaceChildren(
    el('h1', '', n + ' papers selected'),
    el('span', 'muted', 'Changes here apply to all ' + n + '. The papers stay in your library.'),
    part('COLLECTIONS THEY SHARE', cs.map((id) => el('span', 'filed-chip', pathOf(id)))),
    part('LABELS THEY SHARE', ls.map((id) => {
      const c = el('span', 'filed-chip label');
      c.append(dot(labelById(id).colour), document.createTextNode(labelById(id).name));
      return c;
    })),
  );
}

$('sel-fav').addEventListener('click', () => {
  const ids = pickedIDs();
  setFavourite(ids, !ids.every((id) => (state.rows.get(id) || {}).favourite));
});
$('sel-collect').addEventListener('click', (e) => picker(e.currentTarget, 'collection', pickedIDs()));
$('sel-label').addEventListener('click', (e) => picker(e.currentTarget, 'label', pickedIDs()));
$('sel-clear').addEventListener('click', () => select(state.selected));

// Pop-ups: a menu, the picker that finds or makes a collection or label, and
// the questions asked before deleting. One is open at a time.

let popBack = null;

function closePop() {
  const p = $('pop');
  if (p.hidden) return;
  p.hidden = true;
  p.replaceChildren();
  if (popBack && document.contains(popBack)) popBack.focus();
  popBack = null;
}

function openPop(x, y, cls, parts, label) {
  const p = $('pop');
  popBack = document.activeElement;
  p.className = 'pop ' + cls;
  p.onkeydown = null;
  p.removeAttribute('role');
  p.setAttribute('aria-label', label);
  p.replaceChildren(...parts);
  p.hidden = false;
  p.style.left = '0px';
  p.style.top = '0px';
  const r = p.getBoundingClientRect();
  p.style.left = Math.max(8, Math.min(x, window.innerWidth - r.width - 8)) + 'px';
  p.style.top = Math.max(8, Math.min(y, window.innerHeight - r.height - 8)) + 'px';
  return p;
}

function popAt(anchor) {
  const r = anchor.getBoundingClientRect();
  return [r.left, r.bottom + 4];
}

document.addEventListener('pointerdown', (e) => {
  if (!$('pop').hidden && !$('pop').contains(e.target)) closePop();
}, true);
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && !$('pop').hidden) {
    e.preventDefault();
    e.stopPropagation();
    closePop();
  }
}, true);
window.addEventListener('resize', closePop);

// menu shows a list of actions; an action left out is one that cannot apply.
function menu(x, y, label, items) {
  const buttons = [];
  const parts = items.map((it) => {
    if (it === '-') return el('div', 'pop-line');
    const b = button(it.label, it.danger ? 'pop-item danger' : 'pop-item', () => {
      const at = b.getBoundingClientRect();
      closePop();
      it.run(at);
    });
    b.setAttribute('role', 'menuitem');
    buttons.push(b);
    return b;
  });
  const p = openPop(x, y, 'menu', parts, label);
  p.setAttribute('role', 'menu');
  p.onkeydown = (e) => {
    const at = buttons.indexOf(document.activeElement);
    if (e.key === 'ArrowDown') buttons[(at + 1) % buttons.length].focus();
    else if (e.key === 'ArrowUp') buttons[(at - 1 + buttons.length) % buttons.length].focus();
    else return;
    e.preventDefault();
  };
  if (buttons[0]) buttons[0].focus();
}

function rowMenu(id, x, y) {
  if (!(state.picked.size > 1 && state.picked.has(id)) && state.selected !== id) select(id);
  const ids = pickedIDs();
  const known = ids.map(paperState).filter(Boolean);
  const allFav = known.length > 0 && known.every((p) => p.favourite);
  const here = collectionById(state.collection);
  const items = [
    { label: allFav ? 'Remove from favourites' : 'Add to favourites', run: () => setFavourite(ids, !allFav) },
    { label: 'Add to collection…', run: (at) => picker(at, 'collection', ids) },
    { label: 'Label…', run: (at) => picker(at, 'label', ids) },
  ];
  const direct = here && !here.folder && known.some((p) => p.collections.includes(here.id));
  if (direct || ids.length === 1) items.push('-');
  if (direct) items.push({ label: 'Remove from ' + here.name, run: () => fileIn(here.id, ids, false) });
  if (ids.length === 1) items.push({ label: 'Show on the map', run: () => show('map', ids[0]) });
  const fetchable = known.filter((p) => !p.pdf && p.zotero !== 'unmatched' && !p.local);
  if (fetchable.length > 0) {
    items.push({ label: fetchable.length > 1 ? 'Download PDFs (' + fetchable.length + ')' : 'Download PDF', run: () => downloadPDFs(fetchable.map((p) => p.id)) });
  }
  if (ids.length === 1) items.push({ label: 'Bring in papers around this…', run: () => aroundDialog({ id: ids[0] }) });
  menu(x, y, ids.length > 1 ? ids.length + ' papers' : 'Paper', items);
}

function collectionMenu(c, x, y) {
  if (c.folder) {
    menu(x, y, c.name, [{ label: 'Show its papers', run: () => choosePlace(c.id, false) }]);
    return;
  }
  const items = [
    { label: 'New collection inside', run: () => { treeOpen.add(c.id); renderTree({ id: 0, parent: c.id, name: '' }); } },
    { label: 'Rename', run: () => renderTree({ id: c.id, parent: c.parent, name: c.name }) },
    { label: 'Move into…', run: (at) => picker(at, 'move', [], c) },
  ];
  if (c.parent) items.push({ label: 'Move to the top', run: () => moveCollection(c, 0) });
  items.push('-', { label: 'Delete collection', danger: true, run: () => confirmDelete(c) });
  menu(x, y, c.name, items);
}

function labelMenu(l, x, y) {
  menu(x, y, l.name, [
    { label: 'Rename or change colour', run: () => labelForm(l) },
    '-',
    { label: 'Delete label', danger: true, run: () => confirmDeleteLabel(l) },
  ]);
}

// reveal opens every collection above id, so it is in view.
function reveal(id) {
  for (let c = collectionById(id); c; c = collectionById(c.parent)) treeOpen.add(c.id);
  remember('fil.open', [...treeOpen]);
}

async function moveCollection(c, parent) {
  try {
    await api('/api/collections/' + c.id + '/move', 'POST', { parent });
    if (parent) reveal(parent);
  } catch (e) {
    tell(e.message);
  }
  await changed(true);
}

function sideAnchor(selector) {
  const row = document.querySelector(selector);
  if (!row) return [280, 200];
  const r = row.getBoundingClientRect();
  return [r.right + 6, r.top];
}

function askFirst(x, y, title, body, ok, run) {
  const keep = button('Keep it', 'btn', closePop);
  const go = button(ok, 'btn danger', () => {
    closePop();
    run();
  });
  const actions = el('div', 'pop-actions');
  actions.append(keep, go);
  const p = openPop(x, y, 'ask', [el('strong', '', title), el('span', 'muted', body), actions], title);
  p.setAttribute('role', 'alertdialog');
  keep.focus();
}

function confirmDelete(c) {
  const under = inside(c.id).length - 1;
  const [x, y] = sideAnchor('.tree-row[data-collection="' + c.id + '"]');
  const papers = c.works === 1 ? 'The 1 paper and its PDF stay' : c.works > 0 ? 'The ' + c.works.toLocaleString() + ' papers and their PDFs stay' : 'Nothing else changes';
  askFirst(x, y, under ? 'Delete ' + c.name + ' and its ' + (under === 1 ? 'subcollection' : under + ' subcollections') + '?' : 'Delete ' + c.name + '?',
    papers + (c.works > 0 ? ' in your library.' : '.'), 'Delete collection', async () => {
      try {
        await api('/api/collections/' + c.id + '/delete', 'POST', {});
        if (inside(c.id).includes(state.collection)) state.collection = 0;
      } catch (e) {
        tell(e.message);
      }
      await changed(true);
    });
}

function confirmDeleteLabel(l) {
  const [x, y] = sideAnchor('.label-row[data-label="' + l.id + '"]');
  askFirst(x, y, 'Delete the label ' + l.name + '?',
    l.works > 0 ? 'It comes off ' + (l.works === 1 ? '1 paper' : l.works.toLocaleString() + ' papers') + '. The papers stay in your library.' : 'No paper carries it.',
    'Delete label', async () => {
      try {
        await api('/api/labels/' + l.id + '/delete', 'POST', {});
        state.labels = state.labels.filter((id) => id !== l.id);
      } catch (e) {
        tell(e.message);
      }
      await changed(true);
    });
}

// labelForm makes a label, or changes one: its name and its colour.
function labelForm(l, then) {
  const used = state.lib.labels.map((o) => o.colour);
  let colour = l ? l.colour : state.lib.colours.find((c) => !used.includes(c)) || state.lib.colours[used.length % state.lib.colours.length];
  const input = el('input', 'pop-input');
  input.value = l ? l.name : '';
  input.setAttribute('aria-label', 'Label name');
  input.placeholder = 'Label name';
  const swatches = el('div', 'swatches');
  swatches.setAttribute('role', 'group');
  swatches.setAttribute('aria-label', 'Colour');
  const paint = () => swatches.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', b.dataset.colour === colour ? 'true' : 'false'));
  swatches.append(...state.lib.colours.map((c) => {
    const b = button('', 'swatch', () => { colour = c; paint(); });
    b.dataset.colour = c;
    b.style.background = LABEL_COLOURS[c];
    b.setAttribute('aria-label', c);
    return b;
  }));
  paint();
  const error = el('div', 'error');
  const save = async () => {
    const name = input.value.trim();
    if (!name) return;
    try {
      if (l) {
        await api('/api/labels/' + l.id, 'POST', { name, colour });
      } else {
        const res = await api('/api/labels', 'POST', { name, colour });
        if (then) await then(res.id);
      }
    } catch (e) {
      error.textContent = e.message;
      return;
    }
    closePop();
    await changed(!!then);
  };
  const cancel = button('Cancel', 'btn', closePop);
  const actions = el('div', 'pop-actions');
  actions.append(cancel, button(l ? 'Save' : 'Make label', 'btn primary', save));
  const [x, y] = l ? sideAnchor('.label-row[data-label="' + l.id + '"]') : popAt($('new-label'));
  const p = openPop(x, y, 'form', [el('strong', '', l ? 'Change the label' : 'New label'), input, swatches, error, actions], 'Label');
  p.setAttribute('role', 'dialog');
  // Enter saves from anywhere in the form, a colour included.
  p.onkeydown = (e) => {
    if (e.key !== 'Enter' || e.target === cancel) return;
    e.preventDefault();
    save();
  };
  input.focus();
}

// picker finds a collection or label to put papers in, or makes one, or finds
// the collection to move one into. A box is ticked when every paper is in it.
function picker(anchor, kind, ids, moving) {
  const [x, y] = anchor.left !== undefined ? [anchor.left, anchor.bottom + 4] : popAt(anchor);
  const input = el('input', 'pop-input');
  input.placeholder = kind === 'label' ? 'Find or make a label' : kind === 'move' ? 'Find a collection' : 'Find or make a collection';
  input.setAttribute('aria-label', input.placeholder);
  const list = el('div', 'pick-list');
  list.setAttribute('role', 'listbox');
  const known = ids.map(paperState).filter(Boolean);
  const options = () => {
    if (kind === 'label') return state.lib.labels.map((l) => ({ id: l.id, name: l.name, colour: l.colour, key: 'labels' }));
    if (kind === 'move') {
      const not = inside(moving.id);
      const out = collections().filter((c) => !c.folder && !not.includes(c.id) && c.id !== moving.parent).map((c) => ({ id: c.id, name: pathOf(c.id) }));
      return out;
    }
    return collections().filter((c) => !c.folder).map((c) => ({ id: c.id, name: pathOf(c.id), key: 'collections' }));
  };
  const draw = () => {
    const find = input.value.trim().toLowerCase();
    const shown = options().filter((o) => o.name.toLowerCase().includes(find));
    const items = shown.map((o) => {
      const b = el('button', 'pick-item');
      b.type = 'button';
      b.setAttribute('role', 'option');
      if (kind !== 'move') {
        const n = known.filter((p) => p[o.key].includes(o.id)).length;
        const box = el('input');
        box.type = 'checkbox';
        box.tabIndex = -1;
        box.checked = n > 0 && n === known.length;
        box.indeterminate = n > 0 && n < known.length;
        box.setAttribute('aria-hidden', 'true');
        b.setAttribute('aria-selected', box.checked ? 'true' : 'false');
        b.append(box);
        if (o.colour) b.append(dot(o.colour));
        b.append(el('span', 'pick-name', o.name));
        b.addEventListener('click', async () => {
          const on = !box.checked;
          if (kind === 'label') await setLabel(o.id, ids, on);
          else await fileIn(o.id, ids, on);
          if (!$('pop').hidden) draw();
        });
      } else {
        b.append(el('span', 'pick-name', o.name));
        b.addEventListener('click', () => {
          closePop();
          moveCollection(moving, o.id);
        });
      }
      return b;
    });
    const exact = options().some((o) => o.name.toLowerCase() === find || o.name.toLowerCase().endsWith('› ' + find));
    if (find && !exact && kind !== 'move') {
      const name = input.value.trim();
      const make = button((kind === 'label' ? 'Make the label “' : 'Make the collection “') + name + '”', 'pick-item make', () => makeAndFile(name));
      make.setAttribute('role', 'option');
      items.unshift(make);
    }
    if (items.length === 0) items.push(el('div', 'side-note', kind === 'move' ? 'No other collection to move it into.' : kind === 'label' ? 'No labels yet. Type a name to make one.' : 'No collections yet. Type a name to make one.'));
    list.replaceChildren(...items);
  };
  const makeAndFile = async (name) => {
    try {
      if (kind === 'label') {
        const used = state.lib.labels.map((o) => o.colour);
        const colour = state.lib.colours.find((c) => !used.includes(c)) || state.lib.colours[used.length % state.lib.colours.length];
        const res = await api('/api/labels', 'POST', { name, colour });
        await setLabel(res.id, ids, true);
      } else {
        const res = await api('/api/collections', 'POST', { name, parent: 0 });
        await fileIn(res.id, ids, true);
      }
    } catch (e) {
      tell(e.message);
      return;
    }
    input.value = '';
    if (!$('pop').hidden) draw();
  };
  input.addEventListener('input', draw);
  input.addEventListener('keydown', (e) => {
    const items = [...list.querySelectorAll('.pick-item')];
    if (e.key === 'ArrowDown' && items[0]) {
      e.preventDefault();
      items[0].focus();
    } else if (e.key === 'Enter' && items[0]) {
      e.preventDefault();
      items[0].click();
    }
  });
  list.addEventListener('keydown', (e) => {
    const items = [...list.querySelectorAll('.pick-item')];
    const at = items.indexOf(document.activeElement);
    if (e.key === 'ArrowDown' && items[at + 1]) items[at + 1].focus();
    else if (e.key === 'ArrowUp') (items[at - 1] || input).focus();
    else return;
    e.preventDefault();
  });
  const title = kind === 'move' ? 'Move ' + moving.name + ' into' : ids.length > 1 ? ids.length + ' papers' : '';
  const parts = title ? [el('strong', 'pick-title', title), input, list] : [input, list];
  const p = openPop(x, y, 'picker', parts, input.placeholder);
  p.setAttribute('role', 'dialog');
  draw();
  input.focus();
}

$('new-collection').addEventListener('click', () => renderTree({ id: 0, parent: 0, name: '' }));
$('new-label').addEventListener('click', () => labelForm(null));
$('favourites').addEventListener('click', () => choosePlace(0, !state.favourites || $('screen-papers').hidden));

// Dragging, with the pointer rather than the browser's drag and drop: the
// desktop window keeps that for files dropped from outside, and on macOS and
// Linux a drop inside the page never arrives.

const drag = { from: null, on: null, hit: null, swallow: false };

function dragPapers(e, id) {
  if (e.button !== 0 || e.target.closest('input, .star-btn')) return;
  const ids = state.picked.size > 1 && state.picked.has(id) ? [...state.picked] : [id];
  startDrag(e, { papers: ids, text: ids.length === 1 ? '1 paper' : ids.length + ' papers' });
}

function dragCollection(e, c) {
  if (e.button !== 0 || e.target.closest('input')) return;
  startDrag(e, { collection: c, text: c.name });
}

function startDrag(e, what) {
  drag.from = { x: e.clientX, y: e.clientY, what };
  drag.on = false;
  window.addEventListener('pointermove', dragMove);
  window.addEventListener('pointerup', dragEnd);
  window.addEventListener('pointercancel', dragStop);
}

// dropFor says what letting go over target would do, or null when nothing.
function dropFor(target, what) {
  if (!target) return null;
  const kind = target.dataset.drop;
  if (what.papers) {
    if (kind === 'collection') {
      const c = collectionById(Number(target.dataset.collection));
      return { text: 'Add to ' + c.name, run: () => fileIn(c.id, what.papers, true) };
    }
    if (kind === 'label') {
      const l = labelById(Number(target.dataset.label));
      return { text: 'Label ' + l.name, run: () => setLabel(l.id, what.papers, true) };
    }
    if (kind === 'favourite') return { text: 'Add to favourites', run: () => setFavourite(what.papers, true) };
    return null;
  }
  const c = what.collection;
  if (kind === 'collection') {
    const to = Number(target.dataset.collection);
    if (inside(c.id).includes(to) || to === c.parent) return null;
    return { text: 'Move into ' + collectionById(to).name, run: () => moveCollection(c, to) };
  }
  if (kind === 'top' && c.parent) return { text: 'Move to the top', run: () => moveCollection(c, 0) };
  return null;
}

function dragMove(e) {
  const f = drag.from;
  if (!drag.on) {
    if (Math.hypot(e.clientX - f.x, e.clientY - f.y) < 6) return;
    drag.on = true;
    document.body.classList.add('dragging');
    closePop();
  }
  const badge = $('drag-badge');
  badge.hidden = false;
  badge.style.left = e.clientX + 14 + 'px';
  badge.style.top = e.clientY + 10 + 'px';
  const under = document.elementFromPoint(e.clientX, e.clientY);
  const target = under ? under.closest('[data-drop]') : null;
  const drop = dropFor(target, f.what);
  if (drag.hit && drag.hit !== target) drag.hit.classList.remove('drop-on');
  drag.hit = drop ? target : null;
  if (drag.hit) drag.hit.classList.add('drop-on');
  drag.drop = drop;
  const what = el('b', '', f.what.text);
  badge.replaceChildren(what, el('span', '', drop ? drop.text : ''));
}

function dragEnd() {
  const drop = drag.on ? drag.drop : null;
  if (drag.on) drag.swallow = true; // the click that ends a drag is not a click on what is under it
  dragStop();
  if (drop) drop.run();
}

function dragStop() {
  window.removeEventListener('pointermove', dragMove);
  window.removeEventListener('pointerup', dragEnd);
  window.removeEventListener('pointercancel', dragStop);
  if (drag.hit) drag.hit.classList.remove('drop-on');
  document.body.classList.remove('dragging');
  $('drag-badge').hidden = true;
  drag.from = null;
  drag.on = false;
  drag.hit = null;
  drag.drop = null;
  setTimeout(() => { drag.swallow = false; }, 0);
}

window.addEventListener('click', (e) => {
  if (drag.swallow) {
    e.stopPropagation();
    e.preventDefault();
    drag.swallow = false;
  }
}, true);
window.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && drag.on) dragStop();
});
