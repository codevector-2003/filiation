// AI assistants: which are on this computer, connecting them to fil mcp, and
// what they asked. Nothing in another program's settings changes until the
// person presses Add and connect.
'use strict';

const assist = { connecting: '', settings: '' };

const logoOf = {
  'claude-code': 'lg-claude', 'claude-desktop': 'lg-claude', vscode: 'lg-copilot', gemini: 'lg-gemini', cursor: 'lg-cursor',
};

const HOW_TO = 'https://github.com/codevector-2003/filiation/blob/main/docs/MCP.md';

function logo(name) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('width', '34');
  svg.setAttribute('height', '34');
  svg.setAttribute('aria-hidden', 'true');
  svg.classList.add('assist-logo');
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', '#' + name);
  svg.append(use);
  return svg;
}

screens.assistants = loadAssistants;

async function loadAssistants() {
  try {
    const a = await api('/api/assistants');
    const lib = state.lib || {};
    $('flow-lib').textContent = lib.name || 'My Library';
    $('flow-counts').textContent = (lib.works || 0).toLocaleString() + ' papers, ' + (lib.pdfs || 0).toLocaleString() + (lib.pdfs === 1 ? ' PDF' : ' PDFs');
    assist.settings = a.settings;
    const found = a.list.filter((x) => x.installed).length;
    $('assist-found').textContent = found + ' of the ' + a.list.length + ' assistants fil can set up ' + (found === 1 ? 'is' : 'are') + ' installed';
    $('assist-grid').replaceChildren(...a.list.map(assistantCard), chatgptCard());
    const grow = $('grow');
    grow.setAttribute('aria-checked', a.grow ? 'true' : 'false');
    renderSide(a);
  } catch (e) {
    $('assist-side').replaceChildren(el('div', 'error', e.message));
  }
}

function assistantCard(s) {
  const card = el('div', 'assist-item' + (s.installed || s.connected ? '' : ' absent'));
  card.append(logo(logoOf[s.id]));
  const text = el('span', 'assist-text');
  const detail = s.connected ? (s.readOnly ? 'Connected, read only' : 'Connected') : s.detail;
  text.append(el('span', 'assist-name', s.name), el('span', s.connected ? 'on' : 'muted', detail));
  if (s.problem) text.title = s.problem;
  card.append(text);
  if (s.connected) {
    card.append(button('Disconnect', 'btn small', () => change(s, 'disconnect')));
  } else if (s.installed) {
    card.append(button('Connect', 'btn small', () => {
      assist.connecting = s.id;
      loadAssistants();
    }));
  } else {
    card.append(button('How to', 'btn small', () => openOutside(HOW_TO)));
  }
  return card;
}

function chatgptCard() {
  const card = el('div', 'assist-item absent');
  card.append(logo('lg-openai'));
  const text = el('span', 'assist-text');
  text.append(el('span', 'assist-name', 'ChatGPT'), el('span', 'muted', "Connects to MCP servers on the web, so it can't start fil yet"));
  card.append(text);
  return card;
}

async function change(s, what) {
  try {
    await api('/api/assistants/' + s.id + '/' + what, 'POST');
    assist.connecting = '';
    await loadAssistants();
  } catch (e) {
    assist.connecting = '';
    await loadAssistants();
    $('assist-side').prepend(el('div', 'error', e.message));
  }
}

function renderSide(a) {
  const side = $('assist-side');
  const parts = [];
  const s = a.list.find((x) => x.id === assist.connecting && !x.connected);
  if (s) {
    const box = el('section', 'assist-panel');
    box.append(el('h2', '', 'Connect ' + s.name));
    box.append(el('p', '', s.file
      ? 'One entry goes into ' + s.name + "'s settings. Everything else in the file stays, and the old file is kept as a backup."
      : 'Filiation runs this command for you.'));
    box.append(el('pre', '', s.entry));
    if (s.file) box.append(el('div', 'path-text', s.file));
    if (s.problem) box.append(el('div', 'error', s.problem));
    const actions = el('div', 'actions');
    const add = button('Add and connect', 'btn primary', () => change(s, 'connect'));
    add.disabled = !!s.problem;
    actions.append(add, button('Cancel', 'btn', () => {
      assist.connecting = '';
      loadAssistants();
    }));
    box.append(actions);
    if (s.restart) box.append(el('div', 'muted', s.restart));
    parts.push(box);
  }
  const log = el('section', 'assist-panel');
  log.append(el('h2', '', 'What your assistants asked'));
  if (a.calls.length === 0) log.append(el('div', 'muted', 'Nothing yet. Each question an assistant asks your library shows here. This record stays on this computer.'));
  for (const c of a.calls) {
    const row = el('div', 'call');
    const when = new Date(c.at);
    row.append(el('span', 'call-time', when.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })));
    const what = el('span', 'call-what');
    what.append(el('code', '', c.tool));
    if (c.args && c.args !== '{}') what.append(el('span', 'muted', c.args));
    if (c.failed) what.append(el('span', 'error', 'It failed'));
    row.append(what);
    log.append(row);
  }
  parts.push(log);
  side.replaceChildren(...parts);
}

$('grow').addEventListener('click', async () => {
  const on = $('grow').getAttribute('aria-checked') !== 'true';
  try {
    await api('/api/assistants/permission?grow=' + on, 'POST');
    await loadAssistants();
  } catch (e) {
    await loadAssistants();
    $('assist-side').prepend(el('div', 'error', e.message));
  }
});

$('copy-settings').addEventListener('click', async () => {
  try {
    if (window.runtime) await window.runtime.ClipboardSetText(assist.settings);
    else await navigator.clipboard.writeText(assist.settings);
    $('copied').textContent = 'Copied';
  } catch (e) {
    $('copied').textContent = assist.settings;
  }
});
