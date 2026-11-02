// The Reader: a paper's own PDF, drawn by PDF.js, with notes kept beside it in
// the library. Highlights are boxes over the page, stored as fractions of its
// size, so they fit at any zoom and the PDF file is never touched.
'use strict';

const COLOURS = { yellow: '#F6DE7A', green: '#A9D8B4', blue: '#A8C8E6', pink: '#EDB3C6' };

const reader = {
  paper: null, doc: null, page: 1, pages: 0, scale: 1.1, tool: 'select', colour: 'yellow',
  opening: 0, finding: 0, notes: [], tab: 'notes', words: [], selection: null, from: 'papers', draw: 0, findText: '', comment: null,
};

let pdfjs;

async function loadPDFjs() {
  if (!pdfjs) {
    pdfjs = await import('./pdfjs/pdf.min.mjs');
    pdfjs.GlobalWorkerOptions.workerSrc = 'pdfjs/pdf.worker.min.mjs';
  }
  return pdfjs;
}

// openDocument opens a paper's PDF; indexed asks for only the edition its text
// was read from.
async function openDocument(id, indexed) {
  const lib = await loadPDFjs();
  const url = '/api/papers/' + encodeURIComponent(id) + '/pdf' + (indexed ? '?indexed=true' : '');
  return lib.getDocument({ url, disableAutoFetch: true }).promise;
}

// drawPage draws one page into box, with the text layer that lets its words be
// selected and found. It returns where each matching word is on the page. A
// draw overtaken by a newer one, as current says, leaves box as it is.
async function drawPage(doc, n, scale, box, words, current) {
  const page = await doc.getPage(n);
  if (current && !current()) return [];
  const viewport = page.getViewport({ scale });
  const ratio = window.devicePixelRatio || 1;
  const canvas = el('canvas');
  canvas.width = Math.floor(viewport.width * ratio);
  canvas.height = Math.floor(viewport.height * ratio);
  canvas.style.width = viewport.width + 'px';
  canvas.style.height = viewport.height + 'px';
  const text = el('div', 'textLayer');
  const marks = el('div', 'mark-layer');
  box.style.width = viewport.width + 'px';
  box.style.height = viewport.height + 'px';
  box.style.setProperty('--scale-factor', scale);
  box.replaceChildren(canvas, marks, text, el('div', 'pin-layer'));
  await page.render({ canvasContext: canvas.getContext('2d'), viewport, transform: ratio === 1 ? null : [ratio, 0, 0, ratio, 0, 0] }).promise;
  const layer = new pdfjs.TextLayer({ textContentSource: page.streamTextContent(), container: text, viewport });
  await layer.render();
  return matchWords(text, marks, words);
}

// wholeWords matches these words alone, so that "art" does not light "partial".
function wholeWords(words) {
  const any = words.map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|');
  return new RegExp('(?<![\\p{L}\\p{N}])(' + any + ')(?![\\p{L}\\p{N}])', 'giu');
}

// matchWords lights up each matching word on the page.
function matchWords(text, marks, words) {
  const wanted = [...new Set(words.map((w) => w.toLowerCase()).filter(Boolean))];
  if (wanted.length === 0) return [];
  const re = wholeWords(wanted);
  const base = marks.getBoundingClientRect();
  const found = [];
  const walk = document.createTreeWalker(text, NodeFilter.SHOW_TEXT);
  for (let node = walk.nextNode(); node; node = walk.nextNode()) {
    for (const m of node.data.matchAll(re)) {
      const range = document.createRange();
      range.setStart(node, m.index);
      range.setEnd(node, m.index + m[0].length);
      for (const r of range.getClientRects()) {
        const b = el('div', 'hit');
        b.style.left = r.left - base.left + 'px';
        b.style.top = r.top - base.top + 'px';
        b.style.width = r.width + 'px';
        b.style.height = r.height + 'px';
        marks.append(b);
        found.push(b);
      }
    }
  }
  return found;
}

function drawNotes() {
  const box = $('reader-page');
  const marks = box.querySelector('.mark-layer');
  if (!marks) return;
  const pins = box.querySelector('.pin-layer');
  marks.querySelectorAll('.note-box').forEach((n) => n.remove());
  pins.replaceChildren();
  const here = reader.notes.filter((n) => n.page === reader.page && Array.isArray(n.rects) && n.rects.length > 0);
  for (const n of here) {
    for (const r of n.rects) {
      const b = el('div', 'note-box');
      b.style.left = r.x * 100 + '%';
      b.style.top = r.y * 100 + '%';
      b.style.width = r.w * 100 + '%';
      b.style.height = r.h * 100 + '%';
      b.style.background = COLOURS[n.colour] || COLOURS.yellow;
      marks.append(b);
    }
    if (n.comment) {
      const pin = button('', 'note-pin', () => focusNote(n.id));
      pin.setAttribute('aria-label', 'Comment on this highlight');
      pin.title = n.comment;
      pin.style.top = n.rects[0].y * 100 + '%';
      pins.append(pin);
    }
  }
}

async function showPage(n) {
  if (!reader.doc) return;
  const page = Math.min(reader.pages, Math.max(1, n));
  reader.page = page;
  const draw = ++reader.draw;
  hideMenu();
  $('reader-pg').value = page;
  $('reader-prev').disabled = page <= 1;
  $('reader-next').disabled = page >= reader.pages;
  document.querySelectorAll('.thumb').forEach((t) => t.classList.toggle('on', Number(t.dataset.page) === page));
  const on = document.querySelector('.thumb.on');
  if (on) on.scrollIntoView({ block: 'nearest' });
  try {
    const found = await drawPage(reader.doc, page, reader.scale, $('reader-page'), reader.words, () => draw === reader.draw);
    if (draw !== reader.draw) return;
    drawNotes();
    $('reader-view').scrollTop = 0;
    if (found.length > 0) found[0].scrollIntoView({ block: 'center' });
    if (reader.tab === 'cited') renderSide();
  } catch (e) {
    if (draw === reader.draw) $('reader-page').replaceChildren(el('div', 'note error', e.message));
  }
}

async function drawThumbs(id) {
  const list = $('reader-thumbs');
  list.replaceChildren();
  const seen = new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      seen.unobserve(e.target);
      thumbImage(e.target, id);
    }
  }, { root: list });
  for (let n = 1; n <= reader.pages; n++) {
    const b = button('', 'thumb' + (n === reader.page ? ' on' : ''), () => showPage(n));
    b.dataset.page = n;
    b.setAttribute('aria-label', 'Page ' + n);
    b.append(el('span', 'thumb-sheet'), el('span', 'thumb-n', String(n)));
    list.append(b);
    seen.observe(b);
  }
}

async function thumbImage(b, id) {
  if (reader.paper.id !== id) return;
  const page = await reader.doc.getPage(Number(b.dataset.page)).catch(() => null); // the paper may have closed
  if (!page) return;
  const viewport = page.getViewport({ scale: 72 / page.getViewport({ scale: 1 }).width });
  const canvas = el('canvas');
  canvas.width = viewport.width;
  canvas.height = viewport.height;
  await page.render({ canvasContext: canvas.getContext('2d'), viewport }).promise;
  b.querySelector('.thumb-sheet').replaceChildren(canvas);
}

// openReader opens a paper in the Reader, at a page and with words to light.
async function openReader(id, page, words) {
  reader.from = document.querySelector('.nav.on') ? document.querySelector('.nav.on').dataset.screen : 'papers';
  reader.words = words || [];
  reader.comment = null;
  const opening = ++reader.opening;
  $('reader-find').value = '';
  show('reader');
  $('reader-page').replaceChildren(el('div', 'note', 'Opening…'));
  try {
    const [paper, doc, notes] = await Promise.all([api('/api/papers/' + encodeURIComponent(id)), openDocument(id), api('/api/papers/' + encodeURIComponent(id) + '/notes')]);
    if (opening !== reader.opening) { // another paper was opened meanwhile
      doc.destroy();
      return;
    }
    if (reader.doc) reader.doc.destroy();
    reader.paper = paper;
    reader.doc = doc;
    reader.pages = doc.numPages;
    reader.notes = notes.notes;
    $('reader-title').textContent = paper.title;
    $('reader-of').textContent = 'of ' + doc.numPages;
    drawThumbs(id);
    renderSide();
    await showPage(page || 1);
  } catch (e) {
    if (opening === reader.opening) $('reader-page').replaceChildren(el('div', 'note error', e.message));
  }
}

screens.reader = () => {};

function closeReader() {
  hideMenu();
  show(reader.from);
}

// The selection menu.
function hideMenu() {
  $('sel-menu').hidden = true;
  $('sel-copies').hidden = true;
  reader.selection = null;
}

// selectionHere returns what is selected on the page, as text and as boxes in
// fractions of the page's size.
function selectionHere() {
  const sel = window.getSelection();
  const text = sel.toString().replace(/\s+/g, ' ').trim();
  const box = $('reader-page');
  if (!text || sel.rangeCount === 0 || !box.contains(sel.anchorNode)) return null;
  const base = box.getBoundingClientRect();
  const rects = [...sel.getRangeAt(0).getClientRects()]
    .filter((r) => r.width > 1 && r.height > 1)
    .map((r) => ({
      x: (r.left - base.left) / base.width,
      y: (r.top - base.top) / base.height,
      w: r.width / base.width,
      h: r.height / base.height,
    }));
  return { text, rects, page: reader.page, bottom: Math.max(...rects.map((r) => r.y + r.h)), left: Math.min(...rects.map((r) => r.x)) };
}

function cited(text) {
  const c = reader.paper.citation;
  const where = (c ? c + ', ' : '') + 'p. ' + reader.page;
  return '“' + text + '” (' + where + ')';
}

function showMenu(sel) {
  reader.selection = sel;
  const menu = $('sel-menu');
  const copies = $('sel-copies');
  menu.hidden = false;
  menu.style.left = Math.max(0, Math.min(62, sel.left * 100)) + '%';
  menu.style.top = 'calc(' + sel.bottom * 100 + '% + 8px)';
  copies.hidden = true;
}

async function copyText(text) {
  if (window.runtime) await window.runtime.ClipboardSetText(text);
  else await navigator.clipboard.writeText(text);
}

async function addHighlight(colour, comment) {
  const sel = reader.selection;
  if (!sel) return null;
  const id = reader.paper.id;
  const n = await api('/api/papers/' + encodeURIComponent(id) + '/notes', 'POST',
    { page: sel.page, colour, quote: sel.text, rects: sel.rects, comment: comment || '' });
  if (reader.paper.id !== id) return n; // saved, but another paper is open now
  reader.notes.push(n);
  reader.notes.sort((a, b) => a.page - b.page || a.id - b.id);
  window.getSelection().removeAllRanges();
  hideMenu();
  drawNotes();
  reader.tab = 'notes';
  renderSide();
  return n;
}

// The side panel.
function renderSide() {
  document.querySelectorAll('[data-side]').forEach((b) => b.classList.toggle('on', b.dataset.side === reader.tab));
  $('side-notes-tab').textContent = 'Notes ' + reader.notes.length;
  if (reader.tab === 'notes') renderNotes();
  else if (reader.tab === 'cited') renderCited();
  else renderDetails();
}

function noteCard(n) {
  const card = el('div', 'note-card');
  card.dataset.id = n.id;
  const top = el('div', 'note-top');
  const dot = el('span', 'dot');
  dot.style.background = COLOURS[n.colour] || 'transparent';
  top.append(dot, button('Page ' + n.page, 'link', () => showPage(n.page)));
  top.append(button('Delete', 'link quiet', async () => {
    await api('/api/notes/' + n.id + '/delete', 'POST');
    reader.notes = reader.notes.filter((o) => o.id !== n.id);
    drawNotes();
    renderSide();
  }));
  card.append(top);
  if (n.quote) {
    const q = el('p', 'note-quote', n.quote);
    q.style.borderLeftColor = COLOURS[n.colour] || 'var(--line)';
    card.append(q);
  }
  if (n.comment) card.append(el('p', 'note-comment', n.comment));
  card.append(button(n.comment ? 'Edit comment' : 'Add a comment', 'link quiet', () => {
    reader.comment = { id: n.id, text: n.comment };
    renderSide();
  }));
  return card;
}

function focusNote(id) {
  reader.tab = 'notes';
  renderSide();
  const card = document.querySelector('.note-card[data-id="' + id + '"]');
  if (card) card.scrollIntoView({ block: 'center' });
}

function commentForm() {
  const c = reader.comment;
  const form = el('form', 'comment-form');
  const label = el('label', '', c.id ? 'Comment on this note' : 'Comment on the selection');
  label.htmlFor = 'new-note';
  const text = el('textarea');
  text.id = 'new-note';
  text.rows = 3;
  text.placeholder = 'Write a note…';
  text.value = c.text || '';
  const error = el('div', 'error');
  const actions = el('div', 'dialog-actions');
  const save = el('button', 'btn primary', 'Save comment');
  save.type = 'submit';
  actions.append(button('Cancel', 'btn plain', () => {
    reader.comment = null;
    renderSide();
  }), save);
  form.append(label, text, error, actions);
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    try {
      if (c.id) {
        await api('/api/notes/' + c.id, 'POST', { comment: text.value });
        reader.notes.find((n) => n.id === c.id).comment = text.value;
      } else {
        reader.selection = c.selection;
        await addHighlight(reader.colour, text.value);
      }
      reader.comment = null;
      drawNotes();
      renderSide();
    } catch (err) {
      error.textContent = err.message;
    }
  });
  return form;
}

function renderNotes() {
  const box = $('reader-side');
  const parts = reader.notes.map(noteCard);
  if (reader.comment) parts.unshift(commentForm());
  if (parts.length === 0) parts.push(el('div', 'note', 'Select text on the page to highlight it or comment on it.'));
  box.replaceChildren(...parts);
  const area = box.querySelector('textarea');
  if (area) area.focus();
}

async function renderCited() {
  const box = $('reader-side');
  box.replaceChildren(el('div', 'note', 'Looking…'));
  const page = reader.page;
  const id = reader.paper.id;
  try {
    const res = await api('/api/papers/' + encodeURIComponent(id) + '/cited/' + page);
    if (page !== reader.page || reader.tab !== 'cited' || id !== reader.paper.id) return;
    if (res.cited.length === 0) {
      box.replaceChildren(el('div', 'note', 'No citation was found on this page.'));
      return;
    }
    box.replaceChildren(...res.cited.map((c) => {
      const card = el('div', 'note-card');
      card.append(button(c.title, 'link', () => {
        closeReader();
        openInPapers(c.id);
      }));
      if (c.by) card.append(el('span', 'muted', c.by));
      card.append(el('p', 'note-quote', c.sentence));
      return card;
    }));
  } catch (e) {
    if (page === reader.page && reader.tab === 'cited' && id === reader.paper.id) box.replaceChildren(el('div', 'note error', e.message));
  }
}

function renderDetails() {
  const p = reader.paper;
  const card = el('div', 'note-card');
  card.append(el('div', 'link-title', p.title));
  if (p.authors) card.append(el('div', 'authors', p.authors));
  if (p.meta) card.append(el('div', 'meta', p.meta));
  if (p.abstract) card.append(el('p', 'note-comment', p.abstract));
  card.append(button('Open in your PDF reader', 'btn wide', () => api('/api/papers/' + encodeURIComponent(p.id) + '/open', 'POST')));
  $('reader-side').replaceChildren(card);
}

// Find in this paper: the next page with these words, by the library's search.
async function findInPaper(words) {
  if (!words) return;
  reader.findText = words;
  const id = reader.paper.id;
  const finding = ++reader.finding;
  const q = new URLSearchParams({ q: words, paper: id, references: 'true', limit: 'all' });
  const res = await api('/api/search?' + q);
  if (id !== reader.paper.id || finding !== reader.finding) return;
  reader.words = [...new Set(res.hits.flatMap((h) => h.parts.filter((x) => x.mark).map((x) => x.text)))];
  const pages = [...new Set(res.hits.map((h) => h.page))].sort((a, b) => a - b);
  const atLeast = res.capped ? 'at least ' : '';
  $('reader-found').textContent = pages.length === 0 ? 'Not found' : pages.length === 1 ? 'On ' + atLeast + '1 page' : 'On ' + atLeast + pages.length + ' pages';
  if (pages.length > 0) showPage(pages.find((p) => p > reader.page) || pages[0]);
  else document.querySelectorAll('#reader-page .hit').forEach((h) => h.remove());
}

$('reader-back').addEventListener('click', closeReader);
$('reader-prev').addEventListener('click', () => showPage(reader.page - 1));
$('reader-next').addEventListener('click', () => showPage(reader.page + 1));
$('reader-pg').addEventListener('change', (e) => showPage(Number(e.target.value) || reader.page));
function zoom(by) {
  reader.scale = Math.min(3, Math.max(0.5, Math.round((reader.scale + by) * 10) / 10));
  $('reader-zoom').textContent = Math.round(reader.scale * 100) + '%';
  showPage(reader.page);
}
$('reader-out').addEventListener('click', () => zoom(-0.1));
$('reader-in').addEventListener('click', () => zoom(0.1));
$('reader-export').addEventListener('click', () => {
  api('/api/papers/' + encodeURIComponent(reader.paper.id) + '/notes/export', 'POST').catch((e) => { $('reader-found').textContent = e.message; });
});

document.querySelectorAll('[data-tool]').forEach((b) => {
  b.addEventListener('click', () => {
    reader.tool = b.dataset.tool;
    document.querySelectorAll('[data-tool]').forEach((o) => {
      o.classList.toggle('on', o === b);
      o.setAttribute('aria-pressed', o === b ? 'true' : 'false');
    });
  });
});

document.querySelectorAll('[data-colour]').forEach((b) => {
  b.addEventListener('click', () => {
    reader.colour = b.dataset.colour;
    document.querySelectorAll('[data-colour]').forEach((o) => {
      o.classList.toggle('on', o === b);
      o.setAttribute('aria-checked', o === b ? 'true' : 'false');
    });
  });
});

document.querySelectorAll('[data-side]').forEach((b) => {
  b.addEventListener('click', () => {
    reader.tab = b.dataset.side;
    renderSide();
  });
});

// A finished selection acts by the tool in hand.
$('reader-page').addEventListener('mouseup', () => {
  setTimeout(() => {
    const sel = selectionHere();
    if (!sel) return;
    reader.selection = sel;
    if (reader.tool === 'highlight') addHighlight(reader.colour);
    else if (reader.tool === 'comment') {
      reader.comment = { text: '', selection: sel };
      reader.tab = 'notes';
      renderSide();
    } else showMenu(sel);
  }, 0);
});

$('reader-view').addEventListener('mousedown', (e) => {
  if (!e.target.closest('#sel-menu')) hideMenu();
});

$('sel-copy').addEventListener('click', () => { if (reader.selection) copyText(reader.selection.text); });
$('sel-cite').addEventListener('click', () => {
  if (!reader.selection) return;
  const text = cited(reader.selection.text);
  copyText(text);
  $('sel-copies').textContent = 'Copied: ' + text;
  $('sel-copies').style.left = $('sel-menu').style.left;
  $('sel-copies').style.top = 'calc(' + reader.selection.bottom * 100 + '% + 56px)';
  $('sel-copies').hidden = false;
});
$('sel-menu').querySelectorAll('[data-mark]').forEach((b) => {
  b.addEventListener('click', () => addHighlight(b.dataset.mark));
});
$('sel-comment').addEventListener('click', () => {
  reader.comment = { text: '', selection: reader.selection };
  reader.tab = 'notes';
  renderSide();
});
$('sel-search').addEventListener('click', () => {
  if (!reader.selection) return;
  const text = reader.selection.text;
  closeReader();
  show('search');
  $('only-paper').checked = false; // the whole library, whatever it was opened from
  $('q').value = text;
  runSearch();
});

$('reader-find-form').addEventListener('submit', (e) => {
  e.preventDefault();
  const mine = reader.finding + 1;
  findInPaper($('reader-find').value.trim()).catch((err) => {
    if (mine === reader.finding) $('reader-found').textContent = err.message;
  });
});
