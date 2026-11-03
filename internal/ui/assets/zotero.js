// Bringing in a Zotero library, from the Inbox. Zotero is only read: this asks
// what is there, shows it before anything is written, and runs the import as
// the one job.
'use strict';

const zot = { data: null, skip: new Set(), refs: true, pdfs: true, tags: false, asked: 0 };

function zoteroEntry() {
  const b = el('button', 'file zotero-entry' + (inbox.selected === 'zotero' ? ' on' : ''));
  b.type = 'button';
  const line = !zot.data ? 'Bring in your Zotero library'
    : zot.data.state === 'notExport' ? 'Not a Zotero export'
      : zot.data.state !== 'ready' ? 'Not found on this computer'
        : (zot.data.from ? zot.data.from + ', ' : '') + zot.data.items.toLocaleString() + (zot.data.items === 1 ? ' item' : ' items') + (zot.data.already ? ', ' + zot.data.already.toLocaleString() + ' in already' : '');
  b.append(el('span', 'file-name', 'Zotero'), el('span', 'file-state', line));
  b.addEventListener('click', () => {
    inbox.selected = 'zotero';
    renderInbox();
    loadZotero();
  });
  return b;
}

async function loadZotero() {
  const asked = ++zot.asked;
  let data;
  try {
    data = await api('/api/zotero');
  } catch (e) {
    if (asked === zot.asked) $('inbox-main').replaceChildren(el('div', 'note error', e.message));
    return;
  }
  if (asked !== zot.asked) return;
  zot.data = data;
  $('zotero-entry').replaceChildren(zoteroEntry());
  if (inbox.selected === 'zotero') renderZotero();
}

function renderZotero() {
  const d = zot.data;
  const main = $('inbox-main');
  if (!d) {
    main.replaceChildren(el('div', 'note', 'Looking for Zotero…'));
    return;
  }
  const j = job.last || {};
  if (j.running && j.kind === 'zotero') main.replaceChildren(zoteroRunning(j));
  else if (d.state === 'notExport') main.replaceChildren(zoteroNotExport(d));
  else if (d.state !== 'ready') main.replaceChildren(zoteroConnect(d.state));
  else if (d.last && zot.showLast) main.replaceChildren(zoteroDone(d.last));
  else main.replaceChildren(zoteroPreview(d));
}

function zoteroConnect(why) {
  const box = el('div', 'zot');
  box.append(el('h1', '', 'Bring in your Zotero library'),
    el('p', 'zot-lead', 'Filiation reads your Zotero library on this computer and never changes it. Your papers, their collections and their PDFs come in; Zotero stays exactly as it is.'));
  const card = el('div', 'zot-card');
  card.append(el('strong', '', why === 'notAllowed'
    ? 'Zotero is running, but it is not letting other apps read it yet.'
    : 'Zotero is not running, or it is not letting other apps read it.'));
  const steps = el('ol', 'zot-steps');
  ['Open Zotero 7 or later.', 'In Zotero, choose Edit, then Settings, then Advanced.',
    'Tick “Allow other applications on this computer to communicate with Zotero”.'].forEach((s) => steps.append(el('li', '', s)));
  card.append(steps, button('Look again', 'btn primary', loadZotero));
  box.append(card, zoteroExportWay());
  return box;
}

// zoteroExportWay is the way in for a Zotero that is not running here.
function zoteroExportWay() {
  const way = el('div', 'zot-export');
  const steps = el('ol', 'zot-steps');
  ['In Zotero, choose File, then Export Library.', 'Choose Zotero RDF and tick Export Files.',
    'Copy the folder it makes to this computer, then choose the .rdf file inside it.'].forEach((s) => steps.append(el('li', '', s)));
  const error = el('div', 'error');
  way.append(el('strong', '', 'Zotero on another computer, or an older Zotero?'), steps,
    button('Choose an export file', 'btn', () => zoteroChoose(error)), error);
  return way;
}

async function zoteroChoose(error) {
  if (error) error.textContent = '';
  let data;
  try {
    data = await api('/api/zotero/export', 'POST');
  } catch (e) {
    if (error) error.textContent = e.message;
    return;
  }
  if (!data) return; // the dialog was closed
  zot.data = data;
  zot.skip.clear();
  zot.showLast = false;
  $('zotero-entry').replaceChildren(zoteroEntry());
  renderZotero();
}

async function zoteroLive() {
  zot.data = await api('/api/zotero/live', 'POST');
  zot.skip.clear();
  $('zotero-entry').replaceChildren(zoteroEntry());
  renderZotero();
}

function zoteroNotExport(d) {
  const box = el('div', 'zot');
  box.append(el('h1', '', 'That file is not a Zotero RDF export'),
    el('p', 'zot-lead', d.from + ' is not one Filiation can read. A BibTeX or CSL JSON export leaves out your collections and your PDFs, so Filiation reads only Zotero RDF. In Zotero, choose File, Export Library, then Zotero RDF, and tick Export Files.'));
  const error = el('div', 'error');
  const actions = el('div', 'actions');
  actions.append(button('Choose another file', 'btn primary', () => zoteroChoose(error)), button('Back', 'btn', zoteroLive));
  box.append(actions, error);
  return box;
}

// under is a Zotero collection and every one inside it.
function zoteroUnder(key) {
  const out = [key];
  for (let i = 0; i < out.length; i++) zot.data.collections.forEach((c) => { if (c.parent === out[i]) out.push(c.key); });
  return out;
}

function zoteroPreview(d) {
  const box = el('div', 'zot');
  const head = el('div', 'zot-head');
  const things = d.items === 1 ? ' item' : ' items';
  head.append(el('h1', '', d.items.toLocaleString() + things + (d.from ? ' in the export' : ' in your Zotero library')));
  if (d.from) {
    const where = el('span', 'muted', 'Read from ' + d.from + ' in ' + d.folder + '. Nothing is written yet. ');
    const other = el('a', '', 'Choose another file');
    other.href = '#';
    other.addEventListener('click', (e) => { e.preventDefault(); zoteroChoose(); });
    where.append(other);
    head.append(where);
  } else {
    head.append(el('span', 'muted', 'Read from Zotero just now. Nothing is written yet.'));
  }
  const stats = el('div', 'zot-stats');
  [[d.withDOI, 'with a DOI'], [d.withOtherID, 'with an arXiv or PubMed ID'], [d.noID, 'with no ID: kept as your own documents'],
    [d.pdfs, d.from ? 'with a PDF in the export' : 'with a PDF on this computer']].forEach(([n, text]) => stats.append(stat(n, text)));
  box.append(head, stats);
  if (d.pdfsMissing > 0) {
    box.append(el('div', 'zot-warn', (d.pdfsMissing === 1 ? '1 PDF is' : d.pdfsMissing.toLocaleString() + ' PDFs are') +
      ' named in the export but not beside it. Export Files was probably not ticked, or the files folder was left behind. Those papers still come in, without their PDFs.'));
  }
  if (d.already > 0) box.append(el('p', 'muted', d.already.toLocaleString() + ' were brought in before. Bringing them in again adds only what is new, and changes nothing you did here.'));

  const cols = el('div', 'zot-cols');
  const left = el('div', 'zot-left');
  left.append(el('h2', '', 'COLLECTIONS TO BRING IN'));
  const tree = el('div', 'zot-tree');
  tree.setAttribute('role', 'group');
  tree.setAttribute('aria-label', 'Zotero collections');
  const depth = (c) => { let n = 0; for (let p = c.parent; p; p = (d.collections.find((o) => o.key === p) || {}).parent) n++; return n; };
  d.collections.forEach((c) => {
    const row = el('label', 'zot-col');
    row.style.paddingLeft = 8 + depth(c) * 20 + 'px';
    const tick = el('input');
    tick.type = 'checkbox';
    tick.checked = !zot.skip.has(c.key);
    tick.addEventListener('change', () => {
      zoteroUnder(c.key).forEach((k) => { if (tick.checked) zot.skip.delete(k); else zot.skip.add(k); });
      renderZotero();
    });
    row.append(tick, el('span', 'zot-name', c.name), el('span', 'muted', c.items.toLocaleString()));
    tree.append(row);
  });
  if (d.collections.length === 0) tree.append(el('span', 'muted', 'Your Zotero library has no collections.'));
  left.append(tree, el('span', 'muted', 'Items in no collection come in too, under Papers.'));

  const right = el('div', 'zot-right');
  right.append(el('h2', '', 'OPTIONS'));
  const option = (key, title, note) => {
    const row = el('label', 'zot-option');
    const tick = el('input');
    tick.type = 'checkbox';
    tick.checked = zot[key];
    tick.addEventListener('change', () => { zot[key] = tick.checked; renderZotero(); });
    const text = el('span');
    text.append(el('span', 'zot-option-title', title), el('span', 'muted', note));
    row.append(tick, text);
    return row;
  };
  right.append(
    option('refs', 'Record what each paper cites', 'Makes a citation map of your Zotero papers. Their references come in as papers to fetch later.'),
    option('pdfs', 'Copy the PDFs into your library', 'Copied into papers/ and made searchable. Zotero keeps its own copies.'),
    option('tags', 'Make labels from my Zotero tags', d.tags > 0
      ? 'Only tags you added yourself: ' + d.tags.toLocaleString() + (d.tags === 1 ? ' of them.' : ' of them.') + ' Tags publishers added are left out.'
      : 'You have not added tags of your own in Zotero.'));
  if (d.from) {
    right.append(el('h2', '', 'BRINGING IT IN AGAIN'),
      el('span', 'muted', 'An export has no Zotero keys. Next time, papers matched in OpenAlex are known by their DOI, and your own documents by their title and year, so a newer export adds only what is new.'));
  }
  cols.append(left, right);
  box.append(cols);

  const lookups = Math.max(0, d.withDOI + d.withOtherID - d.already);
  const minutes = Math.max(1, Math.ceil(lookups / 5 / 60));
  const foot = el('div', 'zot-foot');
  const error = el('div', 'error');
  const go = button(zot.skip.size ? 'Bring in the chosen collections' : 'Bring in ' + d.items.toLocaleString() + (d.items === 1 ? ' item' : ' items'), 'btn primary', async () => {
    error.textContent = '';
    try {
      zot.showLast = true;
      await startJob('/api/zotero/import', { skip: [...zot.skip], refs: zot.refs, pdfs: zot.pdfs, tags: zot.tags });
    } catch (e) {
      error.textContent = e.message;
    }
  });
  foot.append(el('span', 'zot-estimate', lookups === 0 ? 'Nothing to look up in OpenAlex.'
    : 'About ' + minutes + (minutes === 1 ? ' minute' : ' minutes') + ': ' + lookups.toLocaleString() + (lookups === 1 ? ' lookup' : ' lookups') + ' in OpenAlex, at up to 5 a second.'), go);
  if (d.from) go.before(button('Not now', 'btn', zoteroLive));
  box.append(foot, error);
  return box;
}

function zoteroRunning(j) {
  const box = el('div', 'zot');
  box.append(el('h1', '', 'Bringing in your Zotero library'));
  const line = el('div', 'zot-progress-line');
  line.append(el('span', '', j.note || 'Reading Zotero'), el('span', 'muted', j.total > 0 ? j.done.toLocaleString() + ' of ' + j.total.toLocaleString() : ''));
  const bar = el('div', 'bar');
  const fill = el('div', 'fill');
  fill.style.width = (j.total > 0 ? Math.round((100 * j.done) / j.total) : 5) + '%';
  bar.append(fill);
  box.append(line, bar,
    el('p', 'muted', 'You can keep working: this runs in the background and shows in the sidebar. Stopping keeps what is in so far, and bringing it in again carries on.'),
    button('Stop', 'btn', () => api('/api/job/stop', 'POST')));
  return box;
}

function zoteroDone(r) {
  const box = el('div', 'zot');
  const head = el('div', 'zot-head');
  head.append(el('h1', '', 'Your Zotero library is in'), el('span', 'muted',
    r.items.toLocaleString() + (r.items === 1 ? ' item, ' : ' items, ') + r.collections.toLocaleString() + (r.collections === 1 ? ' collection' : ' collections')));
  const stats = el('div', 'zot-stats');
  [[r.matched, 'matched in OpenAlex'], [r.unmatched, 'not matched: in as your own documents'], [r.pdfs, 'PDFs copied and searchable'],
    [r.pdfsMissing, 'PDFs Zotero lists but are not on this computer']].forEach(([n, text]) => stats.append(stat(n, text)));
  box.append(head, stats);
  if (r.already > 0) box.append(el('p', 'muted', r.already.toLocaleString() + ' were in already; their collections are up to date.'));
  const actions = el('div', 'actions');
  actions.append(
    button('Show them in Papers', 'btn primary', () => { state.sort = 'added'; $('sort').value = 'added'; choosePlace(0, false); }),
    button('Open the citation map', 'btn', () => show('map')),
    button('Check Zotero for new items', 'btn', () => { zot.showLast = false; loadZotero(); }));
  box.append(actions);
  if (r.notMatched.length > 0) {
    box.append(el('h2', '', 'NOT MATCHED'));
    const list = el('div', 'zot-unmatched');
    r.notMatched.forEach((u) => {
      const b = el('button', 'zot-un');
      b.type = 'button';
      const what = el('span', 'zot-un-what');
      what.append(el('span', 'title-text', u.title), el('span', 'muted', [u.by, u.reason].filter(Boolean).join(' · ')));
      b.append(what, el('span', 'chip', zoteroType(u.type)));
      b.addEventListener('click', () => openInPapers(u.id));
      list.append(b);
    });
    box.append(list, el('p', 'muted', 'These are in your library as your own documents, with their PDFs and collections, searchable but without citation links. Open one to give its DOI or find it by title. Adding a DOI in Zotero and checking again matches them too.'));
  }
  return box;
}

// zoteroType says Zotero's item type in words.
function zoteroType(t) {
  const words = { journalArticle: 'Article', book: 'Book', bookSection: 'Chapter', conferencePaper: 'Conference paper', thesis: 'Thesis',
    report: 'Report', webpage: 'Web page', preprint: 'Preprint', document: 'Document', manuscript: 'Manuscript' };
  return words[t] || 'Item';
}

job.listeners.push((j) => {
  if (j.kind === 'zotero') loadZotero();
});

// The running import redraws as the job moves on.
job.ticks.push((j) => {
  if (j.running && j.kind === 'zotero' && inbox.selected === 'zotero' && !$('screen-inbox').hidden) renderZotero();
});
