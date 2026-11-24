// Reader tabs: several papers open at once, one tab each, after the Library
// tab, which holds the side menu and every other screen. Each tab remembers
// where its reader was: the page, the zoom, the side panel, the words found.
// The tabs are kept in the library and come back when it is opened again.
'use strict';

const tabs = {
  list: [],          // the paper tabs, in the order shown
  active: '',        // the fil ID of the tab in front; '' is the Library tab
  screen: 'papers',  // the screen the Library tab shows
  closed: [],        // the tabs closed, last first out, to reopen
  seen: 0,           // a counter, so the tabs seen longest ago are known
  timer: 0,          // a save waiting to be sent
  restored: false,   // saving waits for the kept tabs to be read back
  frozen: false,     // set while the library changes; nothing is saved then
};

const KEEP_DOCS = 4;  // tabs that keep their PDF open; older ones reopen it
const MAX_TABS = 50;  // as library.MaxReaderTabs
const TAB_SVG = {
  lib: 'M2 2.5h3v11H2zM6.5 2.5h3v11h-3zM11 3l2.8-.6 2 10.8-2.8.6z',
  doc: 'M3 1.5h5l3 3v8H3zM8 1.5v3h3',
};

const tabOf = (id) => tabs.list.find((t) => t.id === id);

function svgIcon(d, box) {
  const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  s.setAttribute('viewBox', box || '0 0 16 16');
  s.setAttribute('aria-hidden', 'true');
  const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  p.setAttribute('d', d);
  s.append(p);
  return s;
}

// tabName is the short name a tab shows: "Piwowar 2018".
function tabName(t) {
  const p = t.paper;
  if (!p) return 'Opening…';
  if (p.citation) return p.citation.replace(/ et al\./, '').replace(/,(?=\s*\d{4})/, '');
  return p.title;
}

// screenName is how the Library tab names the screen it is on.
function screenName(name) {
  if (name === 'papers' && state.favourites) return 'Favourites';
  const nav = document.querySelector('.nav[data-screen="' + name + '"]');
  if (nav) {
    const words = [...nav.childNodes].filter((n) => n.nodeType === Node.TEXT_NODE).map((n) => n.textContent).join('').trim();
    if (words) return words;
  }
  return name.charAt(0).toUpperCase() + name.slice(1);
}

function newTab(id) {
  return { id, page: 1, scale: reader.scale || 1.1, side: 'notes', find: '', found: '', words: [], scroll: 0, paper: null, doc: null, notes: null, seen: 0 };
}

// openReader opens a paper in its tab, opening a tab after the one in front
// if it has none. With a page, the tab turns to it and lights words there;
// without, an open tab is shown where it was left.
function openReader(id, page, words) {
  let t = tabOf(id);
  if (!t) {
    if (tabs.list.length >= MAX_TABS) closeTab(oldestTab().id, true);
    t = newTab(id);
    const at = tabs.list.findIndex((x) => x.id === tabs.active);
    tabs.list.splice(at < 0 ? tabs.list.length : at + 1, 0, t);
  }
  const fresh = page != null;
  if (fresh) {
    t.page = page;
    t.words = words || [];
    t.find = '';
    t.found = '';
    t.scroll = 0;
  }
  return activateTab(t, fresh);
}

function oldestTab() {
  return tabs.list.filter((t) => t.id !== tabs.active).sort((a, b) => a.seen - b.seen)[0] || tabs.list[0];
}

// activateTab brings a tab to the front.
function activateTab(t, fresh) {
  if (tabs.active && tabs.active !== t.id) stashTab(tabOf(tabs.active));
  tabs.active = t.id;
  t.seen = ++tabs.seen;
  show('reader');
  tabsChanged();
  if (t.doc && t.paper) return presentTab(t, fresh);
  return loadTab(t, fresh);
}

// goLibrary brings the Library tab to the front, on the screen it was on.
function goLibrary() {
  show(tabs.screen || 'papers');
}

// tabsShown follows show(): any screen but the Reader is the Library tab's.
function tabsShown(name) {
  if (name === 'reader') return;
  if (tabs.active) stashTab(tabOf(tabs.active));
  hideMenu();
  tabs.active = '';
  if (name !== 'start') tabs.screen = name;
  tabsChanged();
}

// tabPaged follows the Reader to another page.
function tabPaged(id, page) {
  const t = tabOf(id);
  if (!t || tabs.active !== id) return;
  t.page = page;
  const pg = document.querySelector('.tab.paper[data-id="' + id + '"] .tab-pg');
  if (pg) pg.textContent = 'p. ' + page;
  saveTabsSoon();
}

// tabsLoaded follows a tab's paper arriving: its name is known now.
function tabsLoaded() {
  dropOldDocs();
  renderTabs();
}

function closeTab(id, quiet) {
  const i = tabs.list.findIndex((t) => t.id === id);
  if (i < 0) return;
  const t = tabs.list[i];
  const front = tabs.active === id;
  if (front) stashTab(t);
  tabs.closed.push({ id: t.id, page: t.page, scale: t.scale, side: t.side, find: t.find, at: i });
  if (tabs.closed.length > 20) tabs.closed.shift();
  tabs.list.splice(i, 1);
  forgetDoc(t);
  if (front && !quiet) {
    tabs.active = '';
    const next = tabs.list[i] || tabs.list[i - 1];
    if (next) activateTab(next, false);
    else goLibrary();
    return;
  }
  tabsChanged();
}

function forgetDoc(t) {
  if (!t.doc) return;
  if (reader.doc === t.doc) {
    reader.doc = null;
    reader.paper = null;
  }
  t.doc.destroy();
  t.doc = null;
}

// dropOldDocs closes the PDFs of the tabs seen longest ago; such a tab opens
// its PDF again, at the same page, when it is next shown.
function dropOldDocs() {
  const open = tabs.list.filter((t) => t.doc && t.id !== tabs.active).sort((a, b) => b.seen - a.seen);
  open.slice(KEEP_DOCS - 1).forEach(forgetDoc);
}

function reopenTab() {
  while (tabs.closed.length) {
    const c = tabs.closed.pop();
    const open = tabOf(c.id);
    if (open) return activateTab(open, false);
    const t = Object.assign(newTab(c.id), c);
    delete t.at;
    tabs.list.splice(Math.min(c.at, tabs.list.length), 0, t);
    return activateTab(t, false);
  }
  return undefined;
}

function closeTabs(keep) {
  const front = tabOf(tabs.active);
  if (front) stashTab(front);
  tabs.list.filter((t) => !keep(t)).forEach((t) => {
    tabs.closed.push({ id: t.id, page: t.page, scale: t.scale, side: t.side, find: t.find, at: tabs.list.indexOf(t) });
    forgetDoc(t);
  });
  tabs.list = tabs.list.filter(keep);
  if (tabs.active && !tabOf(tabs.active)) {
    tabs.active = '';
    goLibrary();
    return;
  }
  tabsChanged();
}

// nextTab goes along the tabs, the Library tab first, round again at the end.
function nextTab(by) {
  const order = ['', ...tabs.list.map((t) => t.id)];
  const at = order.indexOf(tabs.active);
  const to = order[(at + by + order.length) % order.length];
  if (to) activateTab(tabOf(to), false);
  else goLibrary();
}

// tabsChanged redraws what shows which papers are open, and keeps the tabs.
const openSeen = { rows: '', detail: '' };
function tabsChanged() {
  renderTabs();
  const open = tabs.list.map((t) => t.id).sort().join(' ');
  if (open !== openSeen.rows) {
    openSeen.rows = open;
    markOpenRows();
  }
  // The paper on show offers Read, or Go to tab; it is drawn again only when
  // it can be seen, and only when that has changed.
  if (state.paper && !$('screen-papers').hidden && open !== openSeen.detail) {
    openSeen.detail = open;
    const scroll = $('detail').scrollTop;
    renderPaper(state.paper);
    $('detail').scrollTop = scroll;
  }
  saveTabsSoon();
}

// markOpenRows marks the papers in the Papers list that are open in a tab.
function markOpenRows() {
  document.querySelectorAll('#rows .paper-row').forEach((row) => {
    const meta = row.querySelector('.row-meta');
    const chip = meta && meta.querySelector('.chip.open-tab');
    if (tabOf(row.dataset.id) && meta && !chip) {
      const pdf = meta.querySelector('.chip.pdf');
      const c = el('span', 'chip open-tab', 'Open in a tab');
      if (pdf) pdf.after(c);
      else meta.prepend(c);
    } else if (!tabOf(row.dataset.id) && chip) chip.remove();
  });
}

function renderTabs() {
  const strip = $('tabstrip');
  if (!state.lib || !state.lib.ready) {
    strip.hidden = true;
    return;
  }
  strip.hidden = false;
  const lib = el('button', 'tab lib' + (tabs.active ? '' : ' on'));
  lib.type = 'button';
  lib.setAttribute('role', 'tab');
  lib.setAttribute('aria-selected', String(!tabs.active));
  lib.title = 'Library: the side menu and all its screens';
  lib.append(svgIcon(TAB_SVG.lib), document.createTextNode('Library · ' + screenName(tabs.screen)));
  lib.addEventListener('click', goLibrary);
  const parts = [lib, ...tabs.list.map(paperTab)];
  const all = el('button', 'tab-all');
  all.type = 'button';
  all.setAttribute('aria-label', 'All open tabs');
  all.append(document.createTextNode(tabs.list.length === 0 ? '1 tab' : (tabs.list.length + 1) + ' tabs'), svgIcon('M2 3.5l3 3 3-3', '0 0 10 10'));
  all.addEventListener('click', () => {
    const r = all.getBoundingClientRect();
    tabList(r.right - 248, r.bottom + 4);
  });
  parts.push(all);
  strip.replaceChildren(...parts);
  const on = strip.querySelector('.tab.on');
  if (on) on.scrollIntoView({ block: 'nearest', inline: 'nearest' });
}

function paperTab(t) {
  const on = tabs.active === t.id;
  const box = el('div', 'tab paper' + (on ? ' on' : ''));
  box.dataset.id = t.id;
  const go = el('button', 'tab-go');
  go.type = 'button';
  go.setAttribute('role', 'tab');
  go.setAttribute('aria-selected', String(on));
  go.title = t.paper ? t.paper.title + (t.paper.authors ? '\n' + t.paper.authors : '') : '';
  go.append(svgIcon(TAB_SVG.doc, '0 0 14 14'), el('span', 'tab-t', tabName(t)), el('span', 'tab-pg', 'p. ' + t.page));
  go.addEventListener('click', () => { if (tabs.active !== t.id) activateTab(t, false); });
  const x = el('button', 'tab-x');
  x.type = 'button';
  x.setAttribute('aria-label', 'Close ' + tabName(t));
  x.title = 'Close (' + keyText('cmdorctrl+w') + ')';
  x.append(svgIcon('M1 1l8 8M9 1L1 9', '0 0 10 10'));
  x.addEventListener('click', (e) => {
    e.stopPropagation();
    closeTab(t.id);
  });
  box.append(go, x);
  // A middle click closes, as in a browser; held down it would scroll.
  box.addEventListener('mousedown', (e) => { if (e.button === 1) e.preventDefault(); });
  box.addEventListener('auxclick', (e) => {
    if (e.button !== 1) return;
    e.preventDefault();
    closeTab(t.id);
  });
  box.addEventListener('contextmenu', (e) => {
    e.preventDefault();
    tabMenu(t, e.clientX, e.clientY);
  });
  box.addEventListener('pointerdown', (e) => dragTab(e, box));
  return box;
}

function tabMenu(t, x, y) {
  const i = tabs.list.indexOf(t);
  const items = [{ label: 'Close', run: () => closeTab(t.id) }];
  if (tabs.list.length > 1) items.push({ label: 'Close other tabs', run: () => closeTabs((o) => o === t) });
  if (i < tabs.list.length - 1) items.push({ label: 'Close tabs to the right', run: () => closeTabs((o) => tabs.list.indexOf(o) <= i) });
  if (tabs.closed.length) items.push('-', { label: 'Reopen closed tab', run: reopenTab });
  items.push('-',
    { label: 'Show in Papers', run: () => openPaper(t.id) },
    { label: 'Open PDF in another app', run: () => api('/api/papers/' + encodeURIComponent(t.id) + '/open', 'POST').catch((e) => tell(e.message)) });
  menu(x, y, tabName(t), items);
}

function tabList(x, y) {
  const items = [{ label: 'Library · ' + screenName(tabs.screen), on: !tabs.active, run: goLibrary }];
  tabs.list.forEach((t) => items.push({ label: tabName(t) + ' · p. ' + t.page, on: tabs.active === t.id, run: () => activateTab(t, false) }));
  if (tabs.list.length) items.push('-', { label: 'Close all paper tabs', run: () => closeTabs(() => false) });
  menu(x, y, 'Open tabs', items);
}

// dragTab moves a paper tab along the strip once the pointer has moved a
// little; a click without moving is left to the tab.
function dragTab(e, box) {
  if (e.button !== 0 || e.target.closest('.tab-x')) return;
  const strip = $('tabstrip');
  const x0 = e.clientX;
  let moving = false;
  const move = (ev) => {
    if (!moving && Math.abs(ev.clientX - x0) < 6) return;
    moving = true;
    box.classList.add('dragging');
    const others = [...strip.querySelectorAll('.tab.paper')].filter((o) => o !== box);
    const before = others.find((o) => {
      const r = o.getBoundingClientRect();
      return ev.clientX < r.left + r.width / 2;
    });
    strip.insertBefore(box, before || strip.querySelector('.tab-all'));
  };
  const up = () => {
    window.removeEventListener('pointermove', move);
    window.removeEventListener('pointerup', up);
    window.removeEventListener('pointercancel', up);
    if (!moving) return;
    box.classList.remove('dragging');
    tabs.list = [...strip.querySelectorAll('.tab.paper')].map((b) => tabOf(b.dataset.id)).filter(Boolean);
    tabsChanged();
  };
  window.addEventListener('pointermove', move);
  window.addEventListener('pointerup', up);
  window.addEventListener('pointercancel', up);
}

// Keeping the tabs: a second after the last change, and at once before the
// app closes or the library changes.
function saveTabsSoon() {
  if (!tabs.restored || tabs.frozen) return;
  clearTimeout(tabs.timer);
  tabs.timer = setTimeout(() => { saveTabs(false); }, 1000);
}

function tabsToKeep() {
  const front = tabOf(tabs.active);
  if (front && !$('screen-reader').hidden) stashTab(front);
  return {
    tabs: tabs.list.map((t) => ({ id: t.id, page: t.page || 1, zoom: t.scale || 0, side: t.side || '', find: t.find || '' })),
    active: tabs.active,
    screen: tabs.screen,
  };
}

function saveTabs(keepalive) {
  clearTimeout(tabs.timer);
  tabs.timer = 0;
  if (!tabs.restored || tabs.frozen) return Promise.resolve();
  return fetch('/api/tabs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(tabsToKeep()),
    keepalive,
  }).catch(() => {});
}

// flushTabs sends a save still waiting.
async function flushTabs() {
  if (tabs.timer) await saveTabs(false);
}

window.addEventListener('pagehide', () => { if (tabs.timer) saveTabs(true); });

// restoreTabs brings back the tabs kept for this library. Each paper's name
// is asked for now; its PDF only when its tab is shown.
async function restoreTabs() {
  let kept = { tabs: [] };
  try {
    kept = await api('/api/tabs');
  } catch (e) {
    // The app opens without them.
  }
  tabs.list = kept.tabs.map((k) => Object.assign(newTab(k.id), { page: k.page || 1, scale: k.zoom || 1.1, side: k.side || 'notes', find: k.find || '' }));
  if (kept.screen && document.querySelector('.nav[data-screen="' + kept.screen + '"]')) tabs.screen = kept.screen;
  tabs.restored = true;
  tabsChanged();
  tabs.list.forEach((t) => {
    api('/api/papers/' + encodeURIComponent(t.id)).then((p) => {
      if (!t.paper) t.paper = p;
      renderTabs();
    }).catch(() => {});
  });
  const front = tabOf(kept.active);
  if (front) activateTab(front, false);
}
