// The documentation: one page at a time, chosen by the address (#install,
// #mcp …), tabs that remember the visitor's choice across pages, a filter for
// the sidebar, and the "on this page" list. Without the script, every page
// shows in order and every link still works.
(() => {
  const $ = (s, root = document) => root.querySelector(s);
  const $$ = (s, root = document) => [...root.querySelectorAll(s)];
  const pages = $$('.page');
  const navLinks = $$('#doc-nav .nav');
  const ids = pages.map((p) => p.id);

  const arrow = (dir) => '<span class="arr"><svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="' + (dir < 0 ? 'M13 8H3M7 4L3 8l4 4' : 'M3 8h10M9 4l4 4-4 4') + '"/></svg></span>';
  const text = (s) => { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; };

  // The page for an address: a page's own id, or the page holding an id.
  const pageFor = (hash) => {
    const id = decodeURIComponent(hash.replace(/^#/, ''));
    if (ids.includes(id)) return { page: id, anchor: null };
    const target = id && document.getElementById(id);
    const page = target && target.closest('.page');
    return page ? { page: page.id, anchor: id } : { page: ids[0], anchor: null };
  };

  const show = () => {
    const { page, anchor } = pageFor(location.hash);
    const i = ids.indexOf(page);
    const el = pages[i];
    pages.forEach((p) => p.classList.toggle('on', p === el));
    navLinks.forEach((a) => (a.getAttribute('href') === '#' + page ? a.setAttribute('aria-current', 'page') : a.removeAttribute('aria-current')));
    document.title = el.querySelector('h1').textContent + ' · Filiation documentation';

    const toc = $('#toc');
    toc.replaceChildren(toc.querySelector('b'));
    $$('h2[id], h3[id]', el).forEach((h, k) => {
      const a = document.createElement('a');
      a.href = '#' + h.id;
      a.textContent = h.textContent;
      if (k === 0) a.className = 'on';
      toc.append(a);
    });

    const prev = pages[i - 1], next = pages[i + 1];
    $('#pager').innerHTML =
      (prev ? '<a class="pg" href="#' + prev.id + '">' + arrow(-1) + '<span><small>PREVIOUS</small><b>' + text(prev.querySelector('h1').textContent) + '</b></span></a>' : '') +
      (next ? '<a class="pg next" href="#' + next.id + '"><span><small>NEXT · ' + text(next.dataset.group.toUpperCase()) + '</small><b>' + text(next.querySelector('h1').textContent) + '</b></span>' + arrow(1) + '</a>' : '');

    if (anchor) document.getElementById(anchor).scrollIntoView();
    else window.scrollTo(0, 0);
    watch(el);
  };

  // "On this page" follows the reading position.
  let observer;
  const watch = (el) => {
    if (observer) observer.disconnect();
    if (!('IntersectionObserver' in window)) return;
    observer = new IntersectionObserver((entries) => {
      const seen = entries.filter((e) => e.isIntersecting).map((e) => e.target.id);
      if (!seen.length) return;
      $$('#toc a').forEach((a) => a.classList.toggle('on', a.getAttribute('href') === '#' + seen[0]));
    }, { rootMargin: '-80px 0px -65% 0px' });
    $$('h2[id], h3[id]', el).forEach((h) => observer.observe(h));
  };

  // Clicking a contents link inside the current page scrolls without
  // re-rendering; the address still changes so it can be shared.
  document.addEventListener('click', (e) => {
    const a = e.target.closest('#toc a');
    if (!a) return;
    e.preventDefault();
    const id = a.getAttribute('href').slice(1);
    history.replaceState(null, '', '#' + id);
    document.getElementById(id).scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
  });

  // Tabs: a choice in one group (the system, the assistant) holds on every page.
  const choose = (group, value) => {
    $$('[data-tabgroup="' + group + '"] [data-tab]').forEach((b) => b.setAttribute('aria-selected', String(b.dataset.tab === value)));
    $$('[data-panel^="' + group + ':"]').forEach((p) => { p.hidden = p.dataset.panel !== group + ':' + value; });
    if (group === 'os') { const dl = $('#cli-download'); if (dl) { dl.dataset.os = value; if (window.filAssets && window.filAssets.cli[value]) dl.href = window.filAssets.cli[value]; } }
    try { localStorage.setItem('fil-docs-' + group, value); } catch (err) { /* private window */ }
  };
  $$('[data-tabgroup]').forEach((g) => {
    const group = g.dataset.tabgroup;
    g.addEventListener('click', (e) => { const b = e.target.closest('[data-tab]'); if (b) choose(group, b.dataset.tab); });
    let saved = null;
    try { saved = localStorage.getItem('fil-docs-' + group); } catch (err) { /* private window */ }
    const first = group === 'os' && window.filOS ? window.filOS : null;
    const want = saved || first;
    if (want && g.querySelector('[data-tab="' + want + '"]')) choose(group, want);
  });

  // Commands name the newest release.
  const fill = (v) => $$('code[data-cmd]').forEach((c) => { c.textContent = c.dataset.template.replace(/\{v\}/g, v); });
  $$('code[data-cmd]').forEach((c) => { c.dataset.template = c.textContent; });
  fill('0.4.1');
  document.addEventListener('fil:version', (e) => { fill(e.detail); const os = $('[data-tabgroup="os"] [aria-selected="true"]'); if (os) choose('os', os.dataset.tab); });

  // The sidebar filter: matches page titles and their text.
  const find = $('#doc-find');
  const words = pages.map((p) => p.textContent.toLowerCase());
  find.addEventListener('input', () => {
    const q = find.value.trim().toLowerCase().split(/\s+/).filter(Boolean);
    let shown = 0;
    navLinks.forEach((a) => {
      const i = ids.indexOf(a.getAttribute('href').slice(1));
      const hit = q.every((w) => words[i].includes(w));
      a.hidden = !hit;
      if (hit) shown++;
    });
    $$('#doc-nav .group').forEach((g) => {
      let n = g.nextElementSibling, any = false;
      while (n && n.classList.contains('nav')) { if (!n.hidden) any = true; n = n.nextElementSibling; }
      g.hidden = !any;
    });
    $('#no-match').hidden = shown > 0;
  });
  find.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter') return;
    const first = navLinks.find((a) => !a.hidden);
    if (first) location.hash = first.getAttribute('href');
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === '/' && !e.target.closest('input, textarea')) { e.preventDefault(); find.focus(); }
  });

  window.addEventListener('hashchange', show);
  show();
})();
