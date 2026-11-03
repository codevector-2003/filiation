// The Inbox: PDFs the person already has, identified and filed. A title search
// alone never files a PDF: when fil is not sure, the person chooses.
'use strict';

const inbox = { items: [], selected: 0, choice: {}, dropped: [] };

screens.inbox = async () => {
  await loadInbox();
  loadZotero();
  const lib = state.lib || {};
  $('inbox-tip').textContent = 'Drag PDFs anywhere into this window, or save them to ' + (lib.folder ? lib.folder + '​/Inbox' : 'the Inbox folder') + ' and open this screen.';
  lookAgain();
};

// One job runs at a time, so when another is running this waits for it: the
// job's end comes back here.
function lookAgain() {
  if (inbox.dropped.length > 0) {
    const files = inbox.dropped;
    inbox.dropped = [];
    startJob('/api/inbox/import', { files }).catch((e) => {
      if (e.busy) inbox.dropped.unshift(...files);
      else showInboxError(e);
    });
  } else if (inbox.fresh) {
    startJob('/api/inbox/import').catch((e) => { if (!e.busy) showInboxError(e); });
  }
}

function dropPDFs(paths) {
  inbox.dropped.push(...paths);
  show('inbox');
}

function showInboxError(e) {
  $('inbox-main').replaceChildren(el('div', 'note error', e.message));
}

async function loadInbox() {
  try {
    const res = await api('/api/inbox');
    inbox.items = res.items;
    inbox.fresh = res.fresh;
    renderInbox();
  } catch (e) {
    showInboxError(e);
  }
}

const stateLabel = {
  choose: (it) => (it.candidates.length === 1 ? '1 possible paper' : it.candidates.length + ' possible papers'),
  unknown: () => 'Not identified',
};

function renderInbox() {
  const review = inbox.items.filter((i) => i.state !== 'filed');
  const filed = inbox.items.filter((i) => i.state === 'filed');
  $('inbox-count').textContent = review.length === 0 ? 'Nothing to review' : review.length + ' to review';
  $('inbox-review').replaceChildren(...review.map((it) => {
    const li = el('li');
    const b = el('button', 'file ' + it.state + (it.id === inbox.selected ? ' on' : ''));
    b.type = 'button';
    b.append(el('span', 'file-name', it.name), el('span', 'file-state', stateLabel[it.state](it)));
    b.addEventListener('click', () => selectItem(it.id));
    li.append(b);
    return li;
  }));
  $('filed-head').hidden = filed.length === 0;
  $('inbox-filed').replaceChildren(...filed.map((it) => {
    const li = el('li', 'file filed');
    li.append(el('span', 'file-name', it.name));
    if (it.paperId) li.append(button(short(it.title || it.reason || 'In the library'), 'link', () => openInPapers(it.paperId)));
    else if (it.reason) li.append(el('span', 'file-state', it.reason));
    return li;
  }));
  $('zotero-entry').replaceChildren(zoteroEntry());
  $('review-head').hidden = review.length === 0;
  if (inbox.selected === 'zotero') {
    renderZotero();
  } else {
    if (!review.some((i) => i.id === inbox.selected)) inbox.selected = review.length > 0 ? review[0].id : 0;
    renderItem(review.find((i) => i.id === inbox.selected));
  }
  document.querySelectorAll('.file.on').forEach((b) => b.classList.remove('on'));
  const on = [...$('inbox-review').querySelectorAll('button.file')][review.findIndex((i) => i.id === inbox.selected)];
  if (on) on.classList.add('on');
}

function selectItem(id) {
  inbox.selected = id;
  renderInbox();
}

function renderItem(it) {
  const main = $('inbox-main');
  if (!it) {
    main.replaceChildren(el('div', 'note', inbox.items.length > 0 ? 'Everything here has been dealt with.' : 'Nothing is waiting. Add PDFs you already have, and they are identified and filed here. To bring in a Zotero library, choose Zotero on the left.'));
    return;
  }
  const head = el('div', 'item-head');
  head.append(el('span', 'item-name', it.name), el('span', 'muted', it.reason || it.clue || ''));
  const body = el('div', 'item-body');
  const error = el('div', 'error');
  const file = async (req) => {
    error.textContent = '';
    try {
      await api('/api/inbox/' + it.id + '/file', 'POST', req);
      await loadLibrary();
      await loadInbox();
    } catch (e) {
      error.textContent = e.message;
    }
  };

  if (it.candidates.length > 0) {
    body.append(el('h1', '', 'Which paper is this?'));
    const table = el('div', 'candidates');
    table.setAttribute('role', 'radiogroup');
    table.setAttribute('aria-label', 'Candidates');
    const top = el('div', 'cand-row cand-head');
    top.append(el('span'), el('span', '', 'Record in OpenAlex'), el('span', '', 'Year'), el('span', '', 'Title and author in the PDF'));
    table.append(top);
    const chosen = inbox.choice[it.id] ?? it.candidates.findIndex((c) => c.verified);
    const pick = { index: chosen >= 0 ? chosen : 0 };
    const fileBtn = button('', 'btn primary', () => file({ candidate: pick.index }));
    const label = (c) => { fileBtn.textContent = 'File as ' + (c.by || short(c.title)); };
    label(it.candidates[pick.index]);
    it.candidates.forEach((c) => {
      const row = el('label', 'cand-row');
      const radio = el('input');
      radio.type = 'radio';
      radio.name = 'candidate';
      radio.checked = c.index === pick.index;
      radio.addEventListener('change', () => {
        pick.index = c.index;
        inbox.choice[it.id] = c.index;
        label(c);
      });
      const what = el('span', 'cand-what');
      what.append(el('span', 'title-text', c.title), el('span', 'muted', [c.by, c.type].filter(Boolean).join(' · ')));
      row.append(radio, what, el('span', '', c.year || ''), el('span', c.verified ? 'found' : 'muted', c.verified ? 'Found' : 'No'));
      table.append(row);
    });
    body.append(table, el('p', 'cand-note', 'A title search alone never files a PDF, so Filiation asks you.'));
    const actions = el('div', 'actions');
    actions.append(fileBtn, button('Skip for now', 'btn plain', () => skip(it)));
    body.append(actions, error, otherPaper(file));
  } else {
    body.append(el('h1', '', 'Filiation could not identify this PDF'), otherPaper(file));
    const actions = el('div', 'actions');
    actions.append(button('Skip for now', 'btn plain', () => skip(it)));
    body.append(actions, error);
  }
  main.replaceChildren(head, body);
}

// otherPaper names the paper by hand, or files it as a document of the
// person's own, with no citations.
function otherPaper(file) {
  const box = el('div', 'other-paper');
  const named = el('form', 'inline-form');
  const input = el('input');
  input.placeholder = 'Its DOI, arXiv ID or OpenAlex ID';
  input.setAttribute('aria-label', 'The identifier of the paper');
  const go = el('button', 'btn', 'It is this paper');
  go.type = 'submit';
  named.append(input, go);
  named.addEventListener('submit', (e) => {
    e.preventDefault();
    if (input.value.trim()) file({ input: input.value });
  });
  const local = el('form', 'inline-form');
  const title = el('input');
  title.placeholder = 'Title, for a document OpenAlex does not know';
  title.setAttribute('aria-label', 'Title of the document');
  const keep = el('button', 'btn', 'Keep as my document');
  keep.type = 'submit';
  local.append(title, keep);
  local.addEventListener('submit', (e) => {
    e.preventDefault();
    if (title.value.trim()) file({ title: title.value });
  });
  box.append(named, local);
  return box;
}

async function skip(it) {
  await api('/api/inbox/' + it.id + '/skip', 'POST');
  loadInbox();
}

$('inbox-folder').addEventListener('click', () => {
  api('/api/inbox/folder', 'POST').catch(showInboxError);
});

$('inbox-add').addEventListener('click', async () => {
  try {
    await api('/api/inbox/choose', 'POST');
    pollJob();
  } catch (e) {
    showInboxError(e);
  }
});

job.listeners.push(async (j) => {
  if (j.kind === 'import' || inbox.dropped.length > 0) await loadInbox();
  // Only drops wait here: looking for new files again on each job's end could
  // go round for ever, spending a title search each time.
  if (!j.error && inbox.dropped.length > 0) lookAgain();
});
