// The product page's interactive parts. Each works on its own; none needs the network.
(() => {
  const $ = (s, root = document) => root.querySelector(s);
  const $$ = (s, root = document) => [...root.querySelectorAll(s)];
  const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };
  const select = (buttons, chosen) => buttons.forEach((b) => b.setAttribute('aria-selected', String(b === chosen)));
  const IMG = 'assets/img/';
  const LOGO = 'assets/logos/';

  // Hero: one paper, or the whole library.
  const heroShot = $('#hero-shot'), heroNote = $('#hero-note'), heroBtn = $('#hero-toggle');
  let whole = false;
  heroBtn.addEventListener('click', () => {
    whole = !whole;
    heroShot.src = IMG + (whole ? 'hero-whole.webp' : 'hero-around.webp');
    heroShot.alt = whole ? 'The citation map of a whole library of 2,831 papers' : 'One paper and the papers it cites, in the citation map';
    heroBtn.textContent = whole ? 'Back to one paper' : 'See the whole library';
    heroNote.textContent = whole ? 'A whole library: 2,831 papers and 4,933 citations. Blue ones have their PDF.' : 'One paper and what it cites. Solid boxes have their PDF in your library.';
  });

  // The hero's download button names the visitor's system.
  const hero = $('#hero-download');
  if (window.filOS === 'macos' || window.filOS === 'linux') {
    hero.textContent = 'Download fil for ' + (window.filOS === 'macos' ? 'macOS' : 'Linux');
    hero.dataset.download = 'cli';
  }

  // The demo plays while it is on screen, unless the visitor asked for less motion.
  const demo = $('#demo-video');
  if (demo && 'IntersectionObserver' in window && !matchMedia('(prefers-reduced-motion: reduce)').matches) {
    new IntersectionObserver((entries) => entries.forEach((e) => {
      if (e.isIntersecting) demo.play().catch(() => {}); else demo.pause();
    }), { threshold: 0.4 }).observe(demo);
  }

  // Simple from the first minute.
  const simple = [
    ['Install', 'One installer for Windows, no admin rights. Filiation asks where your library should live, and that is all the setup there is. After that it keeps itself up to date.', 'papers.webp', 'The Papers list'],
    ['Add a paper', 'Paste a DOI, an arXiv ID, a link or a title into the search box. Filiation records what it cites, and follows those references for you.', 'find.webp', 'Adding a paper from the title bar'],
    ['See the map', 'Every paper and every citation between them, on one screen. Bigger means cited by more of your papers.', 'whole.webp', 'The citation map of a library'],
    ['Search', 'Type a few keywords. The passages that match come back with their pages, lit on the PDF.', 'search.webp', 'Search with the PDF page lit'],
  ];
  const simpleTabs = $$('[data-tabs="simple"] [data-tab]');
  simpleTabs.forEach((b) => b.addEventListener('click', () => {
    const [title, text, shot, alt] = simple[+b.dataset.tab];
    select(simpleTabs, b);
    $('#simple-title').textContent = title;
    $('#simple-text').textContent = text;
    $('#simple-shot').src = IMG + shot;
    $('#simple-shot').alt = alt;
  }));

  // Zotero.
  const stage = $('[data-zotero]');
  $$('[data-zotero-toggle]').forEach((b) => b.addEventListener('click', () => {
    const into = stage.dataset.zotero === 'out';
    stage.dataset.zotero = into ? 'in' : 'out';
    $('[data-zcount]').textContent = into ? '417 papers' : 'empty';
    $('#zotero .btn[data-zotero-toggle]').textContent = into ? 'Show it again' : 'Bring in my Zotero library';
  }));

  // How it works.
  const steps = $$('.step');
  steps.forEach((b) => b.addEventListener('click', () => { select(steps, b); $('#step-cmd').textContent = b.dataset.step; }));

  // Try the search.
  const queries = [
    { q: 'attention alignment translation', answers: [
      { title: 'Neural Machine Translation by Jointly Learning to Align and Translate', page: 1, by: 'Bahdanau · 2014', text: '…we conjecture that the use of a fixed-length vector is a bottleneck in improving the performance of this basic encoder-decoder architecture, and propose to extend this by allowing a model to automatically (soft-)search for parts of a source sentence that are relevant to predicting a target word…' },
      { title: 'Neural Machine Translation by Jointly Learning to Align and Translate', page: 2, by: 'Bahdanau · 2014', text: '…We show this allows a model to cope better with long sentences. In this paper, we show that the proposed approach of jointly learning to align and translate achieves significantly improved translation performance…' },
    ] },
    { q: 'sequence to sequence LSTM', answers: [
      { title: 'Sequence to Sequence Learning with Neural Networks', page: 1, by: 'Sutskever · 2014', text: '…Our method uses a multilayered Long Short-Term Memory (LSTM) to map the input sequence to a vector of a fixed dimensionality, and then another deep LSTM to decode the target sequence from the vector…' },
    ] },
    { q: 'rare words translation', answers: [
      { title: 'Addressing the Rare Word Problem in Neural Machine Translation', page: 6, by: 'Luong · 2015', text: '…Rare word translation – On the x-axis, we order newstest2014 sentences by their average frequency rank and divide the sentences into groups of sentences with a comparable prevalence of rare words…' },
      { title: 'Neural Machine Translation of Rare Words with Subword Units', page: 6, by: 'Sennrich · 2016', text: '…Our subword representations cause big improvements in the translation of rare and unseen words, but these only constitute 9-11% of the test sets. Since rare words tend to carry central information…' },
    ] },
  ];
  const filler = new Set(['to', 'the', 'a', 'of', 'in', 'and']);
  const marked = (text, q) => {
    const words = q.toLowerCase().split(/\s+/).filter((w) => w && !filler.has(w));
    const p = el('p');
    const re = new RegExp('(' + words.map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|') + ')', 'gi');
    text.split(re).forEach((part, i) => p.append(i % 2 ? el('mark', '', part) : document.createTextNode(part)));
    return p;
  };
  const qRow = $('#queries');
  const showQuery = (i) => {
    const { q, answers } = queries[i];
    select($$('#queries .chip'), $$('#queries .chip')[i]);
    $('#query-text').textContent = q;
    const box = $('#answers');
    box.replaceChildren(...answers.map((a) => {
      const div = el('div', 'answer');
      const t = el('div', 't');
      t.append(el('b', '', a.title), el('span', 'page', 'page ' + a.page));
      div.append(t, marked(a.text, q), el('small', '', a.by));
      return div;
    }));
  };
  queries.forEach((x, i) => { const b = el('button', 'chip', x.q); b.type = 'button'; b.setAttribute('role', 'tab'); b.addEventListener('click', () => showQuery(i)); qRow.append(b); });
  showQuery(0);

  // Ask your AI assistant.
  const assistants = [
    { name: 'Claude', logo: 'claude.svg', setup: 'claude mcp add filiation -- fil mcp' },
    { name: 'GitHub Copilot', logo: 'copilot.svg', setup: 'Filiation › AI assistants › VS Code › Connect' },
    { name: 'Cursor', logo: 'cursor.svg', setup: 'Filiation › AI assistants › Cursor › Connect' },
    { name: 'Gemini CLI', logo: 'gemini.svg', setup: 'Filiation › AI assistants › Gemini CLI › Connect' },
  ];
  const aRow = $('#assistants');
  assistants.forEach((a, i) => {
    const b = el('button', 'atab');
    b.type = 'button';
    b.setAttribute('role', 'tab');
    b.setAttribute('aria-selected', String(i === 0));
    const m = el('span', 'mono'); const img = el('img'); img.src = LOGO + a.logo; img.alt = ''; m.append(img);
    b.append(m, document.createTextNode(a.name));
    b.addEventListener('click', () => {
      select($$('.atab'), b);
      $('#assistant-setup').textContent = a.setup;
      $('#assistant-name').textContent = a.name;
      $('#assistant-logo').src = LOGO + a.logo;
    });
    aRow.append(b);
  });

  // The desktop app.
  const app = [
    ['papers.webp', 'Papers', 'Your library, one paper per row, with its PDF, labels and the paper open beside it.'],
    ['search.webp', 'Search', 'A few keywords, the passages that match them, and the PDF page lit.'],
    ['map.webp', 'Citation map', 'What a paper cites and what cites it, and which of them you have the PDF for.'],
    ['reader.webp', 'Reader', 'Read and mark up, with several papers open in tabs; notes live in your library, so the PDF file stays as it was.'],
    ['groups.webp', 'Groups', 'Topics found from the citations alone, offered as collections.'],
  ];
  const appTabs = $$('[data-tabs="app"] [data-tab]');
  appTabs.forEach((b) => b.addEventListener('click', () => {
    const [shot, alt, caption] = app[+b.dataset.tab];
    select(appTabs, b);
    $('#app-shot').src = IMG + shot;
    $('#app-shot').alt = alt;
    $('#app-caption').textContent = caption;
  }));

  // How it fits.
  const tools = {
    zotero: { name: 'Zotero', keep: ['Keeping references and formatting citations in your writing'], adds: ['Reads your Zotero library and follows its papers\' references', 'Finds the papers your library cites most that you don\'t have'] },
    mendeley: { name: 'Mendeley', keep: ['Your reference collection and citations in Word'], adds: ['A citation map of your reading that keeps growing, on your computer'] },
    maps: { name: 'Citation-map websites', keep: ['Quick visual exploration in the browser'], adds: ['The map, the PDFs and their text kept on your computer, growing with each paper'] },
    ai: { name: 'AI research assistants', keep: ['Searching beyond your own reading'], adds: ['Answers from your own library, with pages you can check, through the assistant you already use'] },
    none: { name: 'Nothing yet', keep: [], adds: ['A library built from one paper, its PDFs, search and a map, in one free app'] },
  };
  const chosen = new Set(['zotero']);
  const tRow = $('#tools');
  const list = (ul, items) => ul.replaceChildren(...items.map((t) => el('li', '', t)));
  const renderTools = () => {
    $$('#tools .chip').forEach((b) => b.setAttribute('aria-pressed', String(chosen.has(b.dataset.key))));
    const keys = [...chosen];
    const keep = [...new Set(keys.flatMap((k) => tools[k].keep))];
    list($('#keep'), keep.length ? keep : ['Nothing to give up']);
    list($('#adds'), [...new Set(['Search across the PDFs you have, to the page', ...keys.flatMap((k) => tools[k].adds)])]);
  };
  Object.entries(tools).forEach(([k, t]) => {
    const b = el('button', 'chip', t.name);
    b.type = 'button';
    b.dataset.key = k;
    b.addEventListener('click', () => { chosen.has(k) ? chosen.delete(k) : chosen.add(k); renderTools(); });
    tRow.append(b);
  });
  renderTools();

  // Where ideas came from.
  const NS = 'http://www.w3.org/2000/svg';
  // Each line is a real citation; the layout is an illustration.
  const nodes = { A: [260, 50, 'Hochreiter 1997'], B: [110, 150, 'Bengio 1994'], C: [380, 150, 'Sutskever 2014'], E: [170, 260, 'Graves 2013'], F: [330, 260, 'Bahdanau 2014'], G: [460, 260, 'Cho 2014'], H: [250, 360, 'Vaswani 2017'], I: [430, 360, 'Luong 2015'] };
  const links = [['A', 'B'], ['A', 'C'], ['C', 'F'], ['C', 'G'], ['F', 'H'], ['F', 'I'], ['E', 'H'], ['C', 'I']];
  const pairs = [
    { label: 'Vaswani 2017 → Hochreiter 1997', path: ['H', 'F', 'C', 'A'], text: 'Vaswani 2017 cites Bahdanau 2014, which cites Sutskever 2014, which cites Hochreiter 1997: from the Transformer back to the LSTM.' },
    { label: 'Luong 2015 → Hochreiter 1997', path: ['I', 'C', 'A'], text: 'Luong 2015 cites Sutskever 2014, which cites Hochreiter 1997: two steps.' },
    { label: 'Hochreiter 1997 → Bengio 1994', path: ['A', 'B'], text: 'The LSTM paper cites Bengio 1994, which showed why long-term dependencies are hard to learn: one step.' },
  ];
  const svg = $('#stemma');
  const svgEl = (tag, attrs) => { const e = document.createElementNS(NS, tag); Object.entries(attrs).forEach(([k, v]) => e.setAttribute(k, v)); return e; };
  const lines = links.map(([a, b]) => { const l = svgEl('line', { x1: nodes[a][0], y1: nodes[a][1], x2: nodes[b][0], y2: nodes[b][1] }); l.dataset.ends = a + b; svg.append(l); return l; });
  const dots = Object.entries(nodes).map(([k, [x, y, label]]) => {
    const c = svgEl('circle', { cx: x, cy: y, r: 9 }); c.dataset.key = k; svg.append(c);
    const t = svgEl('text', { x, y: y + 26, 'text-anchor': 'middle' }); t.textContent = label; svg.append(t);
    return c;
  });
  const pRow = $('#pairs');
  const showPair = (i) => {
    const { path, text } = pairs[i];
    select($$('#pairs .chip'), $$('#pairs .chip')[i]);
    const on = new Set(path.slice(1).map((k, j) => [path[j], k].sort().join('')));
    lines.forEach((l) => l.classList.toggle('on', on.has([...l.dataset.ends].sort().join(''))));
    dots.forEach((c) => { const hit = path.includes(c.dataset.key); c.classList.toggle('on', hit); c.setAttribute('r', hit ? 11 : 8); });
    $('#path-text').textContent = text;
  };
  pairs.forEach((p, i) => { const b = el('button', 'chip', p.label); b.type = 'button'; b.setAttribute('role', 'tab'); b.addEventListener('click', () => showPair(i)); pRow.append(b); });
  showPair(0);

  // The command line, per system. The version follows the newest release.
  const cmds = {
    windows: (v) => 'Expand-Archive fil_' + v + '_windows_amd64.zip fil; .\\fil\\fil.exe version',
    macos: (v) => 'tar xzf fil_' + v + '_darwin_arm64.tar.gz && ./fil version',
    linux: (v) => 'tar xzf fil_' + v + '_linux_amd64.tar.gz && ./fil version',
  };
  const osTabs = $$('#os-tabs .chip');
  let osNow = 'windows';
  const showOS = (os) => {
    osNow = os;
    select(osTabs, osTabs.find((b) => b.dataset.os === os));
    $('#os-cmd').textContent = cmds[os](window.filVersion);
    const dl = $('#os-download');
    dl.dataset.os = os;
    dl.textContent = 'Download fil for ' + { windows: 'Windows', macos: 'macOS', linux: 'Linux' }[os];
    if (window.filAssets) dl.href = window.filAssets.cli[os] || dl.href;
  };
  osTabs.forEach((b) => b.addEventListener('click', () => showOS(b.dataset.os)));
  document.addEventListener('fil:version', () => showOS(osNow));
  showOS(window.filOS === 'macos' || window.filOS === 'linux' ? window.filOS : 'windows');
})();
