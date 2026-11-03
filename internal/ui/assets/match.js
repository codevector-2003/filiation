// Matching one of your own documents to the paper OpenAlex knows it as: by
// an identifier you type, which is free, or by choosing from a title search,
// which costs credits and runs only when asked.
'use strict';

const match = { refs: true };

function matchBox(p) {
  const box = el('div', 'match');
  const why = p.zotero
    ? 'Zotero has no DOI, arXiv ID or PubMed ID for it that OpenAlex knows, so it has no citation links. It is searchable as your own document.'
    : 'OpenAlex did not know it when it came in, so it has no citation links. It is searchable as your own document.';
  box.append(el('strong', '', 'Not matched in OpenAlex'), el('span', 'match-why', why));
  const error = el('div', 'error');

  const form = el('form', 'match-id');
  const input = el('input');
  input.placeholder = 'DOI, arXiv ID or PubMed ID';
  input.setAttribute('aria-label', 'The paper\'s DOI, arXiv ID or PubMed ID');
  const go = el('button', 'btn', 'Link');
  go.type = 'submit';
  form.append(input, go);
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    if (input.value.trim()) linkPaper(p, input.value.trim(), error, [go]);
  });
  box.append(form, el('span', 'muted small', 'If you know its ID. Looking one up is free.'));

  const search = el('div', 'match-search');
  const find = button('Find it by title', 'btn', () => findByTitle(p, box, error, find));
  search.append(find, el('span', 'muted small', 'Uses 10 of the 1,000 free OpenAlex credits a day. You choose from what it finds.'));
  box.append(search, error);
  return box;
}

async function findByTitle(p, box, error, find) {
  error.textContent = '';
  find.disabled = true;
  find.textContent = 'Searching OpenAlex…';
  let found;
  try {
    found = await api('/api/papers/' + encodeURIComponent(p.id) + '/candidates', 'POST');
  } catch (e) {
    error.textContent = e.message;
    find.disabled = false;
    find.textContent = 'Find it by title';
    return;
  }
  if (state.paper !== p) return;
  const out = el('div', 'match-found');
  if (found.length === 0) {
    out.append(el('strong', '', 'OpenAlex has nothing with this title'),
      el('span', 'match-why', 'If you find its DOI, give it above and it is linked for free.'));
    box.replaceChildren(out, error);
    return;
  }
  const head = el('div', 'match-found-head');
  head.append(el('strong', '', found.length + (found.length === 1 ? ' found in OpenAlex' : ' found in OpenAlex')), el('span', 'muted', 'Choose the one this is.'));
  const list = el('div', 'match-list');
  const buttons = [];
  found.forEach((c) => {
    const row = el('div', 'match-row');
    const what = el('span', 'match-what');
    const kind = c.type ? c.type.charAt(0).toUpperCase() + c.type.slice(1).replace(/-/g, ' ') : '';
    const cited = c.citedBy > 0 ? 'cited ' + c.citedBy.toLocaleString() + (c.citedBy === 1 ? ' time' : ' times') : '';
    what.append(el('span', 'title-text', c.title), el('span', 'muted', [c.by, c.year, c.venue].filter(Boolean).join(' · ')),
      el('span', 'muted', [kind, cited].filter(Boolean).join(' · ')));
    const tags = el('span', 'chips');
    if (c.titleMatch) tags.append(el('span', 'chip ok', 'Same title'));
    if (c.pdfMatches) tags.append(el('span', 'chip ok', 'Its PDF matches'));
    if (c.inLibrary) tags.append(el('span', 'chip', 'Already in your library'));
    if (tags.childElementCount) what.append(tags);
    const pick = button('This is it', 'btn', () => linkPaper(p, c.openalexId, error, buttons));
    buttons.push(pick);
    row.append(what, pick);
    list.append(row);
  });
  const foot = el('div', 'match-foot');
  const none = button('None of these', 'btn', () => {
    const kept = el('div', 'match');
    kept.append(el('strong', '', 'Kept as your own document'),
      el('span', 'match-why', 'Nothing was linked. If you find its DOI later, give it here and it is linked for free.'),
      button('Back', 'btn', () => box.replaceWith(matchBox(p))));
    out.replaceWith(kept);
  });
  buttons.push(none);
  const refs = el('label', 'match-refs');
  const tick = el('input');
  tick.type = 'checkbox';
  tick.checked = match.refs;
  tick.addEventListener('change', () => { match.refs = tick.checked; });
  refs.append(tick, el('span', '', 'Record what it cites'));
  foot.append(none, refs);
  out.append(head, list, foot, error);
  box.replaceChildren(out);
}

async function linkPaper(p, id, error, buttons) {
  error.textContent = '';
  buttons.forEach((b) => { b.disabled = true; });
  let got;
  try {
    got = await api('/api/papers/' + encodeURIComponent(p.id) + '/link', 'POST', { id, refs: match.refs });
  } catch (e) {
    error.textContent = e.message;
    buttons.forEach((b) => { b.disabled = false; });
    return;
  }
  tell(got.folded
    ? 'Linked. It was in your library already; the two are now one paper, with its PDF, collections and labels.'
    : 'Linked to OpenAlex' + (got.refs > 0 ? ', with ' + got.refs.toLocaleString() + ' references.' : '.'));
  await Promise.all([loadLibrary(), loadPapers()]);
  if (!$('screen-papers').hidden) select(got.id);
}
