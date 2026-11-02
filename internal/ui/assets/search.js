// The Search screen: passages in the text of the papers, and the page each is on.
'use strict';

const found = { paper: null, id: '', page: 0, pages: 0, words: [], asked: 0, askedPage: 0 };

screens.search = (paper) => {
  if (paper) {
    found.paper = paper;
    $('only-text').textContent = 'Only ' + paper.title;
    $('only-paper').checked = true;
    $('only-label').hidden = false;
  }
  $('q').focus();
};

function hitRow(h) {
  const a = el('button', 'row');
  a.type = 'button';
  const head = el('div', 'hit-head');
  head.append(el('span', 'title-text', h.title), el('span', 'chip pdf', 'page ' + h.page));
  if (h.references) head.append(el('span', 'chip', 'reference list'));
  const p = el('p', 'passage');
  for (const part of h.parts) p.append(part.mark ? el('mark', '', part.text) : part.text);
  a.append(head, p);
  if (h.by) a.append(el('span', 'muted', h.by));
  a.addEventListener('click', () => {
    document.querySelectorAll('#hits .row').forEach((r) => r.classList.toggle('on', r === a));
    found.id = h.id;
    found.words = h.parts.filter((x) => x.mark).map((x) => x.text);
    loadPage(h.page);
  });
  return a;
}

async function runSearch() {
  const words = $('q').value.trim();
  if (!words) return;
  const asked = ++found.asked;
  const q = new URLSearchParams({ q: words, references: $('with-refs').checked });
  if (found.paper && $('only-paper').checked) q.set('paper', found.paper.id);
  try {
    const res = await api('/api/search?' + q);
    if (asked !== found.asked) return;
    const papers = new Set(res.hits.map((h) => h.id)).size;
    let note = res.hits.length === 0 ? 'No passage contains any of those words.'
      : res.hits.length + (res.hits.length === 1 ? ' passage' : ' passages') + ' in ' + papers + (papers === 1 ? ' paper' : ' papers') + ', best match first.';
    if (res.partial) note = 'No passage has all of those words. These have some of them. ' + note;
    if (res.capped) note += ' Only the best are shown.';
    if (res.unread > 0) note += ' ' + res.unread + (res.unread === 1 ? ' PDF is' : ' PDFs are') + ' not read yet, so not searched.';
    $('search-note').textContent = note;
    $('hits').replaceChildren(...res.hits.map(hitRow));
    if (res.hits.length > 0) $('hits').firstChild.click();
  } catch (e) {
    if (asked === found.asked) {
      $('search-note').textContent = '';
      $('hits').replaceChildren(el('div', 'note error', e.message));
    }
  }
}

// markWords puts text into box with the matched words highlighted.
function markWords(box, text, words) {
  const escaped = [...new Set(words)].filter(Boolean).map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
  if (escaped.length === 0) {
    box.textContent = text;
    return;
  }
  const re = new RegExp('(' + escaped.join('|') + ')', 'gi');
  box.replaceChildren(...text.split(re).map((part, i) => (i % 2 ? el('mark', '', part) : document.createTextNode(part))));
}

async function loadPage(n) {
  const asked = ++found.askedPage;
  try {
    const p = await api('/api/papers/' + encodeURIComponent(found.id) + '/pages/' + n);
    if (asked !== found.askedPage) return;
    found.page = p.page;
    found.pages = p.pages;
    $('page-title').textContent = p.pages > 0 ? 'Page ' + p.page + ' of ' + p.pages : 'Page ' + p.page;
    $('page-prev').disabled = p.page <= 1;
    $('page-next').disabled = p.page >= p.pages;
    $('page-open').hidden = false;
    const sheet = $('page-text');
    if (p.text) markWords(sheet, p.text, found.words);
    else sheet.textContent = 'This page has no text.';
    sheet.scrollTop = 0;
    const mark = sheet.querySelector('mark');
    if (mark) mark.scrollIntoView({ block: 'center' });
  } catch (e) {
    if (asked === found.askedPage) $('page-text').textContent = e.message;
  }
}

$('search-form').addEventListener('submit', (e) => {
  e.preventDefault();
  runSearch();
});
$('page-prev').addEventListener('click', () => loadPage(found.page - 1));
$('page-next').addEventListener('click', () => loadPage(found.page + 1));
$('page-open').addEventListener('click', () => {
  api('/api/papers/' + encodeURIComponent(found.id) + '/open', 'POST').catch((e) => { $('search-note').textContent = e.message; });
});
