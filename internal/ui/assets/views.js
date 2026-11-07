// Saved views: the Papers list's filters under a name, in the sidebar (D33).
// A view is a question asked again each time, so it finds papers that match
// later; nothing is filed. Search can keep to one, or to the filters directly.
'use strict';

const views = () => (state.lib && state.lib.views ? state.lib.views : []);

// currentFilters is what the Papers list is showing, as a view keeps it.
function currentFilters() {
  return {
    show: state.show === 'all' ? '' : state.show, sort: state.sort, title: state.title,
    collection: state.collection, favourites: state.favourites, labels: [...state.labels].sort((a, b) => a - b),
    yearFrom: state.yearFrom, yearTo: state.yearTo, authors: [...state.authors].sort(), venues: [...state.venues].sort(),
  };
}

function sameFilters(a, b) {
  const norm = (f) => JSON.stringify({
    show: f.show || '', sort: f.sort || '', title: f.title || '', collection: f.collection || 0, favourites: !!f.favourites,
    labels: [...(f.labels || [])].sort((x, y) => x - y), yearFrom: f.yearFrom || 0, yearTo: f.yearTo || 0,
    authors: [...(f.authors || [])].sort(), venues: [...(f.venues || [])].sort(),
  });
  return norm(a) === norm(b);
}

function emptyFilters(f) {
  return !f.show && !f.title && !f.collection && !f.favourites && !(f.labels || []).length && !f.yearFrom && !f.yearTo &&
    !(f.authors || []).length && !(f.venues || []).length;
}

// describe says what a view keeps, a line each.
function describe(f) {
  const lines = [];
  if (f.collection) lines.push('In ' + pathOf(f.collection) + ', and the collections inside it');
  if (f.favourites) lines.push('Favourites only');
  (f.labels || []).forEach((id) => {
    const l = labelById(id);
    lines.push('Labelled ' + (l ? l.name : 'with a deleted label'));
  });
  if (f.yearFrom && f.yearTo) lines.push(f.yearFrom === f.yearTo ? 'From ' + f.yearFrom + ' only' : 'From ' + f.yearFrom + ' to ' + f.yearTo);
  else if (f.yearFrom) lines.push('From ' + f.yearFrom);
  else if (f.yearTo) lines.push('Up to ' + f.yearTo);
  if ((f.authors || []).length) lines.push('By ' + f.authors.join(' or '));
  if ((f.venues || []).length) lines.push('In ' + f.venues.join(' or '));
  if (f.title) lines.push('Titles with ' + f.title);
  const shows = { seeds: 'Added by you', pdf: 'With a PDF', notext: 'With a PDF but no text' };
  if (f.show) lines.push(shows[f.show] || f.show);
  if (f.sort) {
    const o = [...$('sort').options].find((x) => x.value === f.sort);
    lines.push('Sorted: ' + (o ? o.textContent : f.sort));
  }
  return lines;
}

function renderViews() {
  const here = !$('screen-papers').hidden;
  const now = currentFilters();
  const rows = views().map((v) => {
    const b = el('button', 'nav view-row');
    b.type = 'button';
    b.dataset.view = v.id;
    b.append(icon('M4 5h16l-6 7v6l-4 2v-8z'), el('span', 'tree-name', v.name), el('span', 'count', v.papers.toLocaleString()));
    b.title = describe(v.filters).join('\n');
    b.classList.toggle('on', here && sameFilters(now, v.filters));
    b.addEventListener('click', () => openView(v));
    b.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      viewMenu(v, e.clientX, e.clientY);
    });
    return b;
  });
  $('views').replaceChildren(...(rows.length ? rows : [el('div', 'side-note', 'Filter the Papers list, then save it as a view.')]));
  fillSearchIn();
}

// openView puts a view's filters on the Papers list, where they can still be
// changed as chips.
function openView(v) {
  const f = v.filters;
  state.show = f.show || 'all';
  state.sort = f.sort || '';
  state.title = f.title || '';
  state.collection = f.collection || 0;
  state.favourites = !!f.favourites;
  state.group = null;
  state.labels = [...(f.labels || [])];
  state.yearFrom = f.yearFrom || 0;
  state.yearTo = f.yearTo || 0;
  state.authors = [...(f.authors || [])];
  state.venues = [...(f.venues || [])];
  $('filter').value = state.title;
  $('sort').value = state.sort;
  document.querySelectorAll('.tab[data-show]').forEach((t) => {
    t.classList.toggle('on', t.dataset.show === state.show);
    t.setAttribute('aria-pressed', t.dataset.show === state.show ? 'true' : 'false');
  });
  show('papers');
}

function saveViewDialog(v) {
  const f = v ? v.filters : currentFilters();
  const name = el('input');
  name.type = 'text';
  name.value = v ? v.name : suggestName(f);
  const keeps = el('div', 'view-keeps');
  keeps.append(el('div', 'mf-head', 'WHAT IT KEEPS'), ...describe(f).map((l) => el('div', '', l)));
  const field = el('label', 'field');
  field.append(el('span', '', 'Name'), name);
  const error = el('div', 'error');
  modal(v ? 'Rename view' : 'Save as view', [
    v ? el('span') : para('A view is a saved set of filters, not a list. Papers that match later show up in it on their own, and nothing is moved or copied.'),
    field, keeps, error,
  ], [cancel, {
    label: v ? 'Rename' : 'Save view', primary: true, run: async () => {
      try {
        if (v) await api('/api/views/' + v.id, 'POST', { name: name.value });
        else await api('/api/views', 'POST', { name: name.value, filters: f });
      } catch (e) {
        error.textContent = e.message;
        return false;
      }
      await loadLibrary();
      return true;
    },
  }]);
  name.select();
}

// suggestName names a view after what it keeps first.
function suggestName(f) {
  const parts = [];
  if (f.collection) parts.push((collectionById(f.collection) || {}).name || 'Collection');
  if (f.favourites) parts.push('Favourites');
  (f.labels || []).forEach((id) => { const l = labelById(id); if (l) parts.push(l.name); });
  if (f.yearFrom) parts.push('since ' + f.yearFrom);
  else if (f.yearTo) parts.push('up to ' + f.yearTo);
  if (f.show === 'pdf') parts.push('with PDF');
  if (f.show === 'seeds') parts.push('added by me');
  if (f.title) parts.push('"' + f.title + '"');
  const name = parts.join(', ') || 'My view';
  return name.charAt(0).toUpperCase() + name.slice(1);
}

function viewMenu(v, x, y) {
  const now = currentFilters();
  const items = [
    { label: 'Open', run: () => openView(v) },
    { label: 'Search inside it', run: () => searchInView(v) },
    '-',
  ];
  if (!emptyFilters(now) && !sameFilters(now, v.filters)) {
    items.push({ label: 'Update to these filters', run: () => updateView(v, now) });
  }
  items.push({ label: 'Rename', run: () => saveViewDialog(v) }, '-',
    { label: 'Delete view', danger: true, run: () => deleteView(v) });
  menu(x, y, v.name, items);
}

async function updateView(v, f) {
  try {
    await api('/api/views/' + v.id, 'POST', { filters: f });
    await loadLibrary();
    tell(v.name + ' now keeps the filters on the Papers list');
  } catch (e) {
    tell(e.message);
  }
}

function deleteView(v) {
  modal('Delete ' + v.name + '?', [para('Only the view goes. Its ' + v.papers.toLocaleString() + (v.papers === 1 ? ' paper stays' : ' papers stay') + ' in your library, in their collections.')],
    [cancel, {
      label: 'Delete view', primary: true, run: async () => {
        await api('/api/views/' + v.id + '/delete', 'POST');
        await loadLibrary();
      },
    }]);
}

// The Search screen's In menu: the whole library, Favourites, a view, a
// collection or a label.
function fillSearchIn() {
  const box = $('search-in');
  const was = box.value;
  const opt = (value, text) => {
    const o = el('option', '', text);
    o.value = value;
    return o;
  };
  const group = (label, options) => {
    if (options.length === 0) return [];
    const g = el('optgroup');
    g.label = label;
    g.append(...options);
    return [g];
  };
  box.replaceChildren(opt('', 'All papers'), opt('fav', 'Favourites'),
    ...group('Views', views().map((v) => opt('view:' + v.id, v.name))),
    ...group('Collections', collections().map((c) => opt('col:' + c.id, pathOf(c.id)))),
    ...group('Labels', (state.lib && state.lib.labels ? state.lib.labels : []).map((l) => opt('label:' + l.id, l.name))));
  box.value = [...box.options].some((o) => o.value === was) ? was : '';
}

// searchScope adds the Search screen's filters to a search.
function searchScope(q) {
  const [kind, id] = $('search-in').value.split(':');
  if (kind === 'fav') q.set('favourites', '1');
  if (kind === 'view') q.set('view', id);
  if (kind === 'col') q.set('collection', id);
  if (kind === 'label') q.set('label', id);
  const from = Number($('search-from').value) || 0;
  const to = Number($('search-to').value) || 0;
  if (from) q.set('year_from', to ? Math.min(from, to) : from);
  if (to) q.set('year_to', from ? Math.max(from, to) : to);
}

function searchInView(v) {
  show('search');
  $('search-in').value = 'view:' + v.id;
  if ($('q').value.trim()) runSearch();
}

$('new-view').addEventListener('click', () => {
  if (emptyFilters(currentFilters())) {
    tell('Filter the Papers list first, then save it as a view');
    show('papers');
    return;
  }
  saveViewDialog(null);
});
['search-in', 'search-from', 'search-to'].forEach((id) => $(id).addEventListener('change', () => {
  if ($('q').value.trim()) runSearch();
}));
