// The documentation pages: tabs that remember the visitor's choice across
// pages, "on this page" following the reading position, and the sidebar's
// search over every page. Each page is complete without it.
(() => {
  const $ = (s, root = document) => root.querySelector(s);
  const $$ = (s, root = document) => [...root.querySelectorAll(s)];

  // Tabs: a choice in one group (the system, the assistant) holds on every page.
  const choose = (group, value) => {
    $$('[data-tabgroup="' + group + '"] [data-tab]').forEach((b) => b.setAttribute('aria-selected', String(b.dataset.tab === value)));
    $$('[data-panel^="' + group + ':"]').forEach((p) => { p.hidden = p.dataset.panel !== group + ':' + value; });
    if (group === 'os') {
      const dl = $('#cli-download');
      if (dl) { dl.dataset.os = value; if (window.filAssets && window.filAssets.cli[value]) dl.href = window.filAssets.cli[value]; }
    }
    try { localStorage.setItem('fil-docs-' + group, value); } catch (err) { /* private window */ }
  };
  $$('[data-tabgroup]').forEach((g) => {
    const group = g.dataset.tabgroup;
    g.addEventListener('click', (e) => { const b = e.target.closest('[data-tab]'); if (b) choose(group, b.dataset.tab); });
    let saved = null;
    try { saved = localStorage.getItem('fil-docs-' + group); } catch (err) { /* private window */ }
    const want = saved || (group === 'os' ? window.filOS : null);
    if (want && g.querySelector('[data-tab="' + want + '"]')) choose(group, want);
  });

  // Commands name the newest release.
  const cmds = $$('code[data-cmd]');
  cmds.forEach((c) => { c.dataset.template = c.textContent; });
  const fill = (v) => cmds.forEach((c) => { c.textContent = c.dataset.template.replace(/\{v\}/g, v); });
  fill(window.filVersion);
  document.addEventListener('fil:version', (e) => {
    fill(e.detail);
    const os = $('[data-tabgroup="os"] [aria-selected="true"]');
    if (os) choose('os', os.dataset.tab);
  });

  // "On this page" follows the reading position.
  const heads = $$('.page h2[id], .page h3[id]');
  if (heads.length && 'IntersectionObserver' in window) {
    const links = $$('#toc a');
    const observer = new IntersectionObserver((entries) => {
      const seen = entries.filter((e) => e.isIntersecting).map((e) => e.target.id);
      if (seen.length) links.forEach((a) => a.classList.toggle('on', a.getAttribute('href') === '#' + seen[0]));
    }, { rootMargin: '-80px 0px -65% 0px' });
    heads.forEach((h) => observer.observe(h));
  }

  // The sidebar's search, over every page's words, loaded on first use.
  const find = $('#doc-find');
  const nav = $('#doc-nav');
  if (!find || !nav) return;
  const root = nav.dataset.root || '';
  const navLinks = $$('.nav', nav);
  let index = null;
  const load = () => index || (index = fetch(root + 'assets/docs-index.json').then((r) => r.json()).catch(() => []));
  const filter = async () => {
    const q = find.value.trim().toLowerCase().split(/\s+/).filter(Boolean);
    const pages = await load();
    const hits = new Set(pages.filter((p) => q.every((w) => p.text.includes(w))).map((p) => p.slug));
    let shown = 0;
    navLinks.forEach((a) => {
      const slug = a.getAttribute('href').replace(/\/$/, '').split('/').pop();
      const hit = !q.length || hits.has(slug);
      a.hidden = !hit;
      if (hit) shown++;
    });
    $$('.group', nav).forEach((g) => {
      let n = g.nextElementSibling, any = false;
      while (n && n.classList.contains('nav')) { if (!n.hidden) any = true; n = n.nextElementSibling; }
      g.hidden = !any;
    });
    $('#no-match').hidden = shown > 0;
  };
  find.addEventListener('focus', load);
  find.addEventListener('input', filter);
  find.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter') return;
    const first = navLinks.find((a) => !a.hidden);
    if (first) location.href = first.href;
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === '/' && !e.target.closest('input, textarea')) { e.preventDefault(); find.focus(); }
  });
})();
