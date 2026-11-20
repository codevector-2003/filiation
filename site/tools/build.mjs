// Writes the documentation pages, the guides and the sitemap from the content
// in site/tools/docs and site/tools/guides, so every page shares one header,
// sidebar and footer. The output is committed: GitHub Pages serves plain files
// and builds nothing.
//
//   node site/tools/build.mjs
//
// Run it after changing anything in site/tools. Each content file starts with
// a comment holding its title, group and intro; the page list below holds the
// order, the address and what search engines are shown.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const SITE = join(dirname(fileURLToPath(import.meta.url)), '..');
const BASE = 'https://codevector-2003.github.io/filiation/';
const REPO = 'https://github.com/codevector-2003/filiation';
const TODAY = new Date().toISOString().slice(0, 10);

const DOCS = [
  { slug: 'install', seo: 'Install Filiation on Windows, macOS and Linux', desc: 'Download the Filiation desktop app for Windows, or the fil command line for Windows, macOS and Linux. No account, no server, nothing else to install.' },
  { slug: 'first-library', seo: 'Build your first research library from one paper', desc: 'Add one paper by DOI, arXiv ID or title, follow its references, keep the open-access PDFs and search them by keyword, in four commands or clicks.' },
  { slug: 'desktop-app', seo: 'The Filiation desktop app: papers, search, citation map, reader', desc: 'A tour of the Filiation app: the Papers list, full-text search with the PDF page lit, the citation map, the Reader, citation groups and keyboard shortcuts.' },
  { slug: 'command-line', seo: 'fil command line reference', desc: 'Every fil command: add, expand, fetch, import, zotero, search, neighbours, path, groups, export graphml, mcp and more, with their main options.' },
  { slug: 'mcp', seo: 'Connect Claude, Cursor, Copilot or Gemini to your papers (MCP)', desc: 'Set up the Filiation MCP server in Claude Code, Claude Desktop, Cursor, VS Code with GitHub Copilot or Gemini CLI, so your AI assistant searches your research library and cites pages.' },
  { slug: 'zotero', seo: 'Import your Zotero library and build a citation graph', desc: 'Bring a Zotero 7 library into Filiation read-only, through Zotero\'s local API or an RDF export, and grow a citation map around it.' },
  { slug: 'how-it-works', seo: 'How Filiation works: OpenAlex citations, open access, fil IDs', desc: 'Where Filiation\'s citations come from (OpenAlex), what a stub is, why it downloads only open-access PDFs, and what your library folder holds.' },
  { slug: 'troubleshooting', seo: 'Filiation troubleshooting and FAQ', desc: 'Fixes for common problems: the MCP server not starting, papers with no PDF, expansion stopping early, Windows SmartScreen, and where your files live.' },
];

const GUIDES = [
  { slug: 'zotero-citation-graph', seo: 'Turn your Zotero library into a citation graph', desc: 'Keep Zotero for references and citations. Filiation reads your Zotero library read-only, follows its references, finds the papers it keeps citing that you lack, and keeps their open-access PDFs.' },
  { slug: 'citation-map-alternative', seo: 'A free, open-source citation map that runs on your computer', desc: 'An open-source alternative to citation-map websites: build the map from one paper, keep the PDFs and their text on your own computer, and search them to the page.' },
  { slug: 'ai-assistant-research-library', seo: 'Let your AI assistant answer from your own papers (MCP)', desc: 'Connect Claude, Cursor, GitHub Copilot or Gemini to your research library over MCP, so answers quote your PDFs with the page each passage came from.' },
];

const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
const strip = (html) => html.replace(/<[^>]+>/g, ' ').replace(/&[a-z]+;/g, ' ').replace(/\s+/g, ' ').trim();

// A content file: a leading comment of "key: value" lines, then the body.
const load = (dir, slug) => {
  const raw = readFileSync(join(SITE, 'tools', dir, slug + '.html'), 'utf8').replace(/\r\n/g, '\n');
  const m = raw.match(/^<!--\n([\s\S]*?)\n-->\n/);
  if (!m) throw new Error(dir + '/' + slug + ': no header comment');
  const meta = Object.fromEntries(m[1].split('\n').map((l) => [l.slice(0, l.indexOf(':')).trim(), l.slice(l.indexOf(':') + 1).trim()]));
  return { ...meta, body: raw.slice(m[0].length).trim() };
};

const write = (rel, html) => {
  const file = join(SITE, rel);
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, html);
  console.log('wrote', rel);
};

const GH = '<svg class="gh" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"/></svg>';
const STAR = '<svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 1.2l2.1 4.3 4.7.7-3.4 3.3.8 4.7L8 12l-4.2 2.2.8-4.7L1.2 6.2l4.7-.7z" fill="#E3B341"/></svg>';
const THEME = '<button class="theme-btn" type="button" aria-label="Colour theme: as your system"><svg class="ti ti-system" width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="2" y="3" width="14" height="9.5" rx="1.5"/><path d="M6.5 15.5h5M9 12.5v3"/></svg><svg class="ti ti-light" width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"><circle cx="9" cy="9" r="3.2"/><path d="M9 1.5v1.8M9 14.7v1.8M1.5 9h1.8M14.7 9h1.8M3.7 3.7l1.3 1.3M13 13l1.3 1.3M3.7 14.3L5 13M13 5l1.3-1.3"/></svg><svg class="ti ti-dark" width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" aria-hidden="true"><path d="M15.2 11.2A6.5 6.5 0 0 1 6.8 2.8a6.5 6.5 0 1 0 8.4 8.4z"/></svg></button>';
const MENU = '<button class="menu-btn" type="button" aria-label="Menu" aria-expanded="false" aria-controls="topnav"><svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true"><path d="M2 4.5h14M2 9h14M2 13.5h14" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg></button>';
const ARROW = (dir) => '<span class="arr"><svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="' + (dir < 0 ? 'M13 8H3M7 4L3 8l4 4' : 'M3 8h10M9 4l4 4-4 4') + '"/></svg></span>';

const head = ({ root, title, desc, path, css, jsonld }) => `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)}</title>
<meta name="description" content="${esc(desc)}">
<link rel="canonical" href="${BASE}${path}">
<meta property="og:type" content="article">
<meta property="og:site_name" content="Filiation">
<meta property="og:title" content="${esc(title)}">
<meta property="og:description" content="${esc(desc)}">
<meta property="og:url" content="${BASE}${path}">
<meta property="og:image" content="${BASE}assets/img/og.jpg">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta name="twitter:card" content="summary_large_image">
<meta name="theme-color" content="#1F5F8B">
<link rel="icon" href="${root}assets/icon.svg" type="image/svg+xml">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=Source+Serif+4:opsz,wght@8..60,500;8..60,600;8..60,700&display=swap" rel="stylesheet">
<script>try{var t=localStorage.getItem('fil-theme');if(t==='light'||t==='dark')document.documentElement.dataset.theme=t;}catch(e){}</script>
<link rel="stylesheet" href="${root}assets/site.css">
<link rel="stylesheet" href="${root}assets/${css}">
<script type="application/ld+json">${JSON.stringify(jsonld)}</script>
</head>
<body>
<a class="skip" href="#main">Skip to the content</a>
`;

const header = (root, current) => `<header class="top full">
<div class="wrap">
<a class="brand" href="${root}"><img src="${root}assets/icon.svg" alt="" width="28" height="28">Filiation</a>
<nav class="topnav" id="topnav" aria-label="Site">
<a href="${root}">Product</a>
<a href="${root}docs/"${current === 'docs' ? ' aria-current="page"' : ''}>Docs</a>
<a href="${root}guides/zotero-citation-graph/"${current === 'guides' ? ' aria-current="page"' : ''}>Guides</a>
<a href="${root}#start">Download</a>
</nav>
<span class="spacer"></span>
<span class="fine ver" data-version="v{v}"></span>
<a class="star" href="${REPO}" aria-label="Star Filiation on GitHub">
<span>${GH}${STAR}Star</span><span class="count" data-stars></span>
</a>
${THEME}
<a class="btn primary small dl" href="${root}#start">Download</a>
${MENU}
</div>
</header>
`;

const footer = (root, scripts) => `<footer class="foot">
<div class="wrap">
<span style="font-weight: 700; color: #FFFFFF">Filiation</span>
<a href="${root}">Product</a>
<a href="${root}docs/">Documentation</a>
${GUIDES.map((g) => `<a href="${root}guides/${g.slug}/">${esc(load('guides', g.slug).short || load('guides', g.slug).title)}</a>`).join('\n')}
<a href="${REPO}">GitHub</a>
<span class="spacer"></span>
<span style="font-size: 13px">Apache-2.0 · Reference data from OpenAlex</span>
<small>Claude, GitHub Copilot, Cursor, Gemini, Zotero and other names are trademarks of their owners, used here only to say what Filiation works with. Zotero is a registered trademark of the Corporation for Digital Scholarship. No endorsement is implied.</small>
</div>
</footer>
${scripts.map((s) => `<script src="${root}assets/${s}"></script>`).join('\n')}
</body>
</html>
`;

const crumbs = (items) => ({ '@context': 'https://schema.org', '@type': 'BreadcrumbList', itemListElement: items.map(([name, path], i) => ({ '@type': 'ListItem', position: i + 1, name, item: BASE + path })) });

// The documentation.
const docs = DOCS.map((d) => ({ ...d, ...load('docs', d.slug) }));
const groups = [...new Set(docs.map((d) => d.group))];
const sidebar = (root, current) => `<aside class="side" aria-label="Documentation">
<label class="find"><svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><circle cx="7" cy="7" r="4.8"/><path d="M10.6 10.6L14 14"/></svg><input id="doc-find" type="search" placeholder="Search the docs" aria-label="Search the docs" autocomplete="off"><kbd>/</kbd></label>
<nav id="doc-nav" data-root="${root}">
${groups.map((g) => `<div class="group">${esc(g.toUpperCase())}</div>\n` + docs.filter((d) => d.group === g).map((d) => `<a class="nav" href="${root}docs/${d.slug}/"${d.slug === current ? ' aria-current="page"' : ''}>${esc(d.title)}</a>`).join('\n')).join('\n')}
<p class="no-match" id="no-match" hidden>Nothing matches. Try fewer words.</p>
</nav>
</aside>
`;

docs.forEach((d, i) => {
  const root = '../../';
  const prev = docs[i - 1], next = docs[i + 1];
  const toc = [...d.body.matchAll(/<h[23][^>]*id="([^"]+)"[^>]*>([\s\S]*?)<\/h[23]>/g)].map(([, id, t]) => `<a href="#${id}">${esc(strip(t))}</a>`).join('\n');
  write(`docs/${d.slug}/index.html`,
    head({ root, title: d.seo + ' · Filiation docs', desc: d.desc, path: `docs/${d.slug}/`, css: 'docs.css', jsonld: crumbs([['Filiation', ''], ['Documentation', 'docs/'], [d.title, `docs/${d.slug}/`]]) }) +
    header(root, 'docs') +
    `<div class="layout">\n${sidebar(root, d.slug)}<main class="main" id="main">
<article class="page on">
<div class="crumb"><a href="../">Docs</a> › ${esc(d.group)}</div>
<h1 class="serif">${esc(d.title)}</h1>
<p class="intro">${d.intro}</p>
${d.body.replace(/\{\{root\}\}/g, root)}
</article>
<nav class="pager" aria-label="Pages">
${prev ? `<a class="pg" href="../${prev.slug}/">${ARROW(-1)}<span><small>PREVIOUS</small><b>${esc(prev.title)}</b></span></a>` : ''}
${next ? `<a class="pg next" href="../${next.slug}/"><span><small>NEXT · ${esc(next.group.toUpperCase())}</small><b>${esc(next.title)}</b></span>${ARROW(1)}</a>` : ''}
</nav>
</main>
<nav class="toc" aria-label="On this page" id="toc"><b>ON THIS PAGE</b>
${toc}
</nav>
</div>
` + footer(root, ['site.js', 'docs.js']));
});

// The documentation's front page, which also sends the old one-page
// addresses (docs/#mcp) to their own pages.
const OLD = { install: 'install', first: 'first-library', app: 'desktop-app', cli: 'command-line', mcp: 'mcp', zotero: 'zotero', how: 'how-it-works', help: 'troubleshooting' };
write('docs/index.html',
  head({ root: '../', title: 'Filiation documentation', desc: 'Install Filiation, build your first library from one paper, use the desktop app and the fil command line, bring in Zotero, and connect your AI assistant over MCP.', path: 'docs/', css: 'docs.css', jsonld: crumbs([['Filiation', ''], ['Documentation', 'docs/']]) }).replace('<body>', `<body>\n<script>(function(){var h=location.hash.slice(1),m=${JSON.stringify(OLD)};if(!h)return;var p=m[h];if(!p){var k=h.split('-')[0];if(m[k])p=m[k]+'/#'+h;}else p+='/';if(p)location.replace(p);})();</script>`) +
  header('../', 'docs') +
  `<div class="layout">\n${sidebar('../', '')}<main class="main" id="main">
<article class="page on">
<div class="crumb">Docs</div>
<h1 class="serif">Documentation</h1>
<p class="intro">Everything about Filiation: installing it, building a library from one paper, the app and the command line, Zotero, and your AI assistant.</p>
${groups.map((g) => `<h2 class="serif" id="${g.toLowerCase().replace(/[^a-z]+/g, '-')}">${esc(g)}</h2>\n<div class="doc-cards">\n` + docs.filter((d) => d.group === g).map((d) => `<a class="doc-card" href="${d.slug}/"><b>${esc(d.title)}</b><span>${d.intro}</span></a>`).join('\n') + '\n</div>').join('\n')}
<h2 class="serif" id="guides">Guides</h2>
<div class="doc-cards">
${GUIDES.map((g) => { const c = load('guides', g.slug); return `<a class="doc-card" href="../guides/${g.slug}/"><b>${esc(c.title)}</b><span>${c.intro}</span></a>`; }).join('\n')}
</div>
</article>
</main>
<nav class="toc" aria-label="On this page" id="toc"><b>ON THIS PAGE</b>
${groups.map((g) => `<a href="#${g.toLowerCase().replace(/[^a-z]+/g, '-')}">${esc(g)}</a>`).join('\n')}
<a href="#guides">Guides</a>
</nav>
</div>
` + footer('../', ['site.js', 'docs.js']));

// The sidebar's search: each page's title and words, fetched on first use.
write('assets/docs-index.json', JSON.stringify(docs.map((d) => ({ slug: d.slug, title: d.title, text: (d.title + ' ' + strip(d.intro) + ' ' + strip(d.body)).toLowerCase() }))));

// The guides.
GUIDES.forEach((g, i) => {
  const c = load('guides', g.slug);
  const root = '../../';
  const others = GUIDES.filter((x) => x !== g).map((x) => { const o = load('guides', x.slug); return `<a class="doc-card" href="../${x.slug}/"><b>${esc(o.title)}</b><span>${o.intro}</span></a>`; }).join('\n');
  write(`guides/${g.slug}/index.html`,
    head({ root, title: g.seo + ' · Filiation', desc: g.desc, path: `guides/${g.slug}/`, css: 'docs.css', jsonld: [crumbs([['Filiation', ''], ['Guides', `guides/${g.slug}/`], [c.title, `guides/${g.slug}/`]]), { '@context': 'https://schema.org', '@type': 'Article', headline: c.title, description: g.desc, image: BASE + 'assets/img/og.jpg', dateModified: TODAY, author: { '@type': 'Organization', name: 'Filiation', url: BASE }, mainEntityOfPage: BASE + `guides/${g.slug}/` }] }) +
    header(root, 'guides') +
    `<main class="guide" id="main">
<article class="page on">
<div class="crumb">Guide</div>
<h1 class="serif">${esc(c.title)}</h1>
<p class="intro">${c.intro}</p>
${c.body.replace(/\{\{root\}\}/g, root)}
<div class="cta">
<div><b>Try Filiation</b><span>Free and open source, on your own computer. The app for Windows; the command line for Windows, macOS and Linux.</span></div>
<a class="btn primary" href="${root}#start">Download</a>
<a class="btn" href="${root}docs/">Read the docs</a>
</div>
<h2 class="serif" id="more">More guides</h2>
<div class="doc-cards">
${others}
</div>
</article>
</main>
` + footer(root, ['site.js']));
});

// The sitemap, for Search Console.
const urls = ['', 'docs/', ...docs.map((d) => `docs/${d.slug}/`), ...GUIDES.map((g) => `guides/${g.slug}/`)];
write('sitemap.xml', `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urls.map((u) => `  <url><loc>${BASE}${u}</loc><lastmod>${TODAY}</lastmod></url>`).join('\n')}
</urlset>
`);
