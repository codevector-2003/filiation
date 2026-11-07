// Citation groups: the library's topics, found from who cites whom and
// suggested as collections (D34). A group is worked out again each time, so
// it is never a list kept anywhere; only a choice about one is.
'use strict';

const grp = { data: null, asked: 0, showHidden: false, countKey: '' };

// groupDetail is how finely groups are cut, the same on this screen, the
// Papers list and the map.
function groupDetail() {
  const d = pref('groupDetail');
  return d === 'broad' || d === 'fine' ? d : 'normal';
}

async function loadGroups() {
  const asked = ++grp.asked;
  const list = $('groups-list');
  if (!grp.data || grp.data.detail !== groupDetail()) {
    list.replaceChildren(el('div', 'note muted', 'Finding groups…'));
    $('groups-note').textContent = '';
    $('groups-foot').replaceChildren();
  }
  let res;
  try {
    res = await api('/api/groups?detail=' + groupDetail());
  } catch (e) {
    if (asked === grp.asked) list.replaceChildren(el('div', 'note error', e.message));
    return;
  }
  if (asked !== grp.asked) return;
  grp.data = res;
  showGroupCount(res);
  renderGroups();
}

function showGroupCount(res) {
  const n = res.groups.filter((g) => !g.dismissed).length;
  $('count-groups').textContent = n ? n.toLocaleString() : '';
}

// refreshGroupCount fills the sidebar's count once the library has changed.
async function refreshGroupCount() {
  if (!state.lib || !state.lib.ready) {
    $('count-groups').textContent = '';
    return;
  }
  const key = state.lib.folder + '/' + state.lib.works + '/' + groupDetail();
  if (key === grp.countKey) return;
  grp.countKey = key;
  try {
    showGroupCount(await api('/api/groups?detail=' + groupDetail()));
  } catch (e) {
    grp.countKey = '';
  }
}

function renderGroups() {
  const res = grp.data;
  document.querySelectorAll('#group-detail button').forEach((b) => {
    const on = b.dataset.detail === res.detail;
    b.classList.toggle('on', on);
    b.setAttribute('aria-pressed', on ? 'true' : 'false');
  });
  const n = (k) => k.toLocaleString();
  const papers = (k) => (k === 1 ? '1 paper' : n(k) + ' papers');
  const note = $('groups-note');
  if (res.groups.length === 0) {
    note.textContent = '';
    $('groups-list').replaceChildren(emptyGroups(res));
    $('groups-foot').replaceChildren();
    return;
  }
  note.textContent = (res.groups.length === 1 ? '1 group' : res.groups.length + ' groups') + ' among the ' + papers(res.linked) +
    ' linked to at least two others in your library.' +
    (res.fringe ? ' ' + papers(res.fringe) + ' linked to only one other ' + (res.fringe === 1 ? 'is' : 'are') + ' not grouped.' : '');
  const rows = res.groups.filter((g) => grp.showHidden || !g.dismissed).map(groupRow);
  $('groups-list').replaceChildren(...rows);
  const foot = [];
  if (res.dismissed > 0) {
    foot.push(el('span', 'muted', (res.dismissed === 1 ? '1 group' : res.dismissed + ' groups') + ' marked not useful.'),
      button(grp.showHidden ? 'Hide them' : 'Show them', 'link', () => { grp.showHidden = !grp.showHidden; renderGroups(); }));
  }
  $('groups-foot').replaceChildren(...foot);
}

function emptyGroups(res) {
  const box = el('div', 'groups-empty');
  box.append(el('strong', '', 'No groups yet'),
    para(res.total === 0
      ? 'Add a paper first. Groups come from the citations between your papers.'
      : 'Groups come from the citations between your papers, and only ' + res.linked.toLocaleString() + ' of ' +
        res.total.toLocaleString() + ' are linked to at least two others. Expanding the library follows the references outward and finds more links.'),
    para('Where OpenAlex has few reference lists, mostly in the humanities, groups stay few and small. That is the data, not a fault.'));
  return box;
}

function groupRow(g) {
  const row = el('div', 'group-row' + (g.dismissed ? ' dismissed' : ''));
  const body = el('div', 'group-body');
  const name = button(g.name, 'group-name', () => openGroup(g));
  name.title = 'Show its papers on the Papers list';
  const years = g.yearFrom ? ' · ' + (g.yearFrom === g.yearTo ? g.yearFrom : g.yearFrom + '–' + g.yearTo) : '';
  body.append(name,
    el('span', 'group-meta', g.papers.toLocaleString() + ' papers · ' + g.fetched.toLocaleString() + ' fetched' + years),
    el('span', 'group-top', g.top.map((p) => p.title + (p.by ? ' (' + p.by + ')' : '')).join('; ')));
  const actions = el('div', 'group-actions');
  const made = g.collection && collectionById(g.collection);
  if (made) {
    actions.append(el('span', 'group-made', 'Saved as ' + pathOf(g.collection)),
      button('Open', 'btn small', () => choosePlace(g.collection, false)));
  } else if (g.dismissed) {
    actions.append(button('Suggest again', 'btn small', () => dismissGroup(g, false)));
  } else {
    actions.append(button('Make collection…', 'btn small', () => groupCollectionDialog(g)),
      button('Not useful', 'link', () => dismissGroup(g, true)));
  }
  row.append(body, actions);
  return row;
}

// openGroup shows a group's papers on the Papers list, as a chip.
function openGroup(g) {
  state.group = { anchor: g.anchor, name: g.name, detail: groupDetail(), papers: g.papers, fringe: g.fringe };
  state.collection = 0;
  state.favourites = false;
  show('papers');
}

function groupCollectionDialog(g) {
  const name = el('input');
  name.type = 'text';
  name.value = g.name;
  const field = el('label', 'field');
  field.append(el('span', '', 'Name'), name);
  const parent = el('select');
  const top = el('option', '', 'The top level');
  top.value = '0';
  parent.append(top, ...collections().filter((c) => !c.folder).map((c) => {
    const o = el('option', '', pathOf(c.id));
    o.value = String(c.id);
    return o;
  }));
  const where = el('label', 'field');
  where.append(el('span', '', 'Inside'), parent);
  const parts = [para('The ' + g.papers.toLocaleString() + ' papers in this group are filed in a new collection. They stay where they are too, and the collection is yours to change from then on.'), field, where];
  const fringe = el('input');
  fringe.type = 'checkbox';
  if (g.fringe > 0) {
    const l = el('label', 'mf-check group-fringe');
    l.append(fringe, el('span', '', 'Also file the ' + (g.fringe === 1 ? 'paper' : g.fringe.toLocaleString() + ' papers') +
      ' linked to this group and nothing else'));
    parts.push(l);
  }
  const error = el('div', 'error');
  parts.push(error);
  modal('Make a collection from ' + g.name, parts, [cancel, {
    label: 'Make collection', primary: true, run: async () => {
      let res;
      try {
        res = await api('/api/groups/' + encodeURIComponent(g.anchor) + '/collection', 'POST',
          { detail: groupDetail(), name: name.value, parent: Number(parent.value), fringe: fringe.checked });
      } catch (e) {
        error.textContent = e.message;
        return false;
      }
      await loadLibrary();
      const n = g.papers + (fringe.checked ? g.fringe : 0);
      tellWith('Filed ' + n.toLocaleString() + ' papers in ' + pathOf(res.id), 'Open', () => choosePlace(res.id, false));
      if (!$('screen-groups').hidden) loadGroups();
      else if (state.group && state.group.anchor === g.anchor) {
        await loadPapers();
        if (state.selected && state.picked.size <= 1) select(state.selected); // its collections have changed
      }
      return true;
    },
  }]);
  name.select();
}

async function dismissGroup(g, dismissed) {
  try {
    await api('/api/groups/' + encodeURIComponent(g.anchor) + '/dismiss', 'POST', { dismissed });
  } catch (e) {
    tell(e.message);
    return;
  }
  if (dismissed) tellWith(g.name + ' will not be suggested again', 'Undo', () => dismissGroup(g, false));
  await loadGroups();
}

screens.groups = () => loadGroups();

document.querySelectorAll('#group-detail button').forEach((b) => b.addEventListener('click', () => {
  setPref('groupDetail', b.dataset.detail === 'normal' ? '' : b.dataset.detail);
  grp.countKey = '';
  loadGroups();
}));
