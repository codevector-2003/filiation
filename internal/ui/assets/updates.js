// Updating the app (D38). Once a day, and when asked from the Help menu, the
// app asks GitHub for the latest version. A newer one shows a note in the
// title bar; on Windows, where Setup installed the app, Update and restart
// downloads it, checks it, saves the tabs, and lets Setup put the new version
// in place and open it. Elsewhere the note offers the download page.
'use strict';

const upd = {
  info: null,    // the last check that found a newer version
  timer: 0,      // following a download
  failed: '',    // what went wrong, shown in the title bar
};

const DAY = 24 * 60 * 60 * 1000;
const autoUpdates = () => pref('updates.off') !== '1';

function mb(bytes) {
  return Math.max(1, Math.round(bytes / (1 << 20)));
}

// checkUpdates asks for the latest version. Asked for by hand, it always
// answers; by itself it says nothing unless there is something new, and
// nothing about a version the person chose to skip.
async function checkUpdates(byHand) {
  let res;
  try {
    res = await api('/api/update');
  } catch (e) {
    if (byHand) modal('Could not check for updates', [para(e.message)], [{ label: 'OK', primary: true }]);
    return;
  }
  if (res.unavailable) {
    if (byHand) modal('Updates', [para('This copy of Filiation does not check for updates.')], [{ label: 'OK', primary: true }]);
    return;
  }
  setPref('updates.checked', String(Date.now()));
  if (!res.newer) {
    upd.info = null;
    drawUpdate();
    if (byHand) modal('Filiation is up to date', [para('You have ' + res.current + ', the latest version.')], [{ label: 'OK', primary: true }]);
    return;
  }
  if (!byHand && pref('updates.skip') === res.latest) return;
  upd.info = res;
  upd.failed = '';
  drawUpdate();
  if (byHand) updatePanel();
}

// startUpdates checks once a day: at start, and while the app stays open.
function startUpdates() {
  welcomeUpdate();
  const due = () => autoUpdates() && Date.now() - Number(pref('updates.checked') || 0) > DAY;
  if (due()) checkUpdates(false);
  setInterval(() => { if (due()) checkUpdates(false); }, 60 * 60 * 1000);
}

function toggleAutoUpdates() {
  setPref('updates.off', autoUpdates() ? '1' : '');
  tell(autoUpdates() ? 'Filiation checks for updates once a day.' : 'Automatic update checks are off. Help, Check for Updates… still works.');
}

// welcomeUpdate says, after an update, that it worked.
function welcomeUpdate() {
  const from = pref('updates.from');
  if (!from) return;
  setPref('updates.from', '');
  if (bar.version && bar.version !== from) {
    tellWith('Filiation is up to date: ' + bar.version + '.', 'What is new', () => openOutside('https://github.com/codevector-2003/filiation/releases/tag/v' + bar.version));
  }
}

// drawUpdate draws the title bar's note: a newer version, a download under
// way, or what went wrong.
function drawUpdate(state) {
  const box = $('tb-update');
  if (upd.failed) {
    box.className = 'tb-job tb-update';
    box.replaceChildren(el('span', 'tb-job-title', upd.failed.title), el('span', 'tb-job-note error', upd.failed.why));
    box.append(button(upd.failed.label, 'link', upd.failed.run));
    const x = button('', 'tb-job-x', () => { upd.failed = ''; drawUpdate(); });
    x.setAttribute('aria-label', 'Dismiss');
    x.innerHTML = '<svg viewBox="0 0 10 10" aria-hidden="true"><path d="M1 1l8 8M9 1L1 9"/></svg>';
    box.append(x);
    box.hidden = false;
    return;
  }
  if (state && (state.phase === 'downloading' || state.phase === 'checking')) {
    box.className = 'tb-job tb-update';
    const barBox = el('div', 'bar');
    const fill = el('div', 'fill');
    fill.style.width = (state.phase === 'checking' || !state.total ? 100 : Math.round((100 * state.done) / state.total)) + '%';
    fill.style.background = 'var(--accent)';
    barBox.append(fill);
    box.replaceChildren(
      el('span', 'tb-job-title', state.phase === 'checking' ? 'Checking the download' : 'Downloading the update'),
      barBox,
      el('span', 'tb-job-count', state.phase === 'checking' ? '' : mb(state.done) + ' of ' + mb(state.total) + ' MB'),
      button('Cancel', 'link', cancelUpdate));
    box.hidden = false;
    return;
  }
  if (!upd.info) {
    box.hidden = true;
    return;
  }
  box.className = 'tb-update';
  const chip = button('', 'tb-update-chip', updatePanel);
  chip.append(el('span', 'tb-update-dot'), document.createTextNode(upd.info.canInstall ? 'Update to ' + upd.info.latest : upd.info.latest + ' is out'));
  chip.title = 'A new version of Filiation is ready';
  box.replaceChildren(chip);
  box.hidden = false;
}

function updatePanel() {
  const i = upd.info;
  if (!i) return;
  const parts = [el('h3', 'upd-title', 'Filiation ' + i.latest + (i.canInstall ? ' is ready' : ' is out'))];
  const when = i.published ? new Date(i.published).toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: 'numeric' }) : '';
  parts.push(el('span', 'muted', 'You have ' + i.current + '.' + (when ? ' Released ' + when + '.' : '')));
  const what = el('div', 'upd-notes');
  if (i.notes && i.notes.length) {
    what.append(el('span', 'side-head', 'WHAT IS NEW'), ...i.notes.map((n) => el('span', '', n)));
  }
  what.append(button('Read the release notes on GitHub', 'link', () => openOutside(i.page)));
  parts.push(what);
  const actions = el('div', 'upd-actions');
  if (i.canInstall) {
    parts.push(el('span', '', 'Your library, settings and open tabs are kept. The app closes, updates and opens again by itself, in under a minute.'));
    actions.append(button('Update and restart', 'btn primary', () => { closePop(); startUpdate(); }));
  } else {
    parts.push(el('span', '', bar.platform === 'windows'
      ? 'This copy was not installed by Setup, so it cannot update itself: download the new version, and install it over this one.'
      : 'On this system the app cannot update itself yet: download the new version, and replace this one with it.'));
    actions.append(button('Open the download page', 'btn primary', () => { closePop(); openOutside(i.page); }));
  }
  actions.append(button('Later', 'btn', closePop));
  const skip = button('Skip this version', 'link quiet', () => {
    setPref('updates.skip', i.latest);
    upd.info = null;
    closePop();
    drawUpdate();
    tell('Skipped ' + i.latest + '. You will be told when a later version comes out.');
  });
  actions.append(skip);
  parts.push(actions);
  const r = $('tb-update').getBoundingClientRect();
  openPop(r.right - 380, r.bottom + 6, 'form upd-pop', parts, 'Update Filiation');
}

async function startUpdate() {
  upd.failed = '';
  try {
    drawUpdate(await api('/api/update/download', 'POST'));
  } catch (e) {
    return failUpdate('The update did not download', e.message, 'Try again', startUpdate);
  }
  clearInterval(upd.timer);
  upd.timer = setInterval(followUpdate, 500);
  return undefined;
}

async function followUpdate() {
  let s;
  try {
    s = await api('/api/update/status');
  } catch (e) {
    return undefined;
  }
  if (s.phase === 'downloading' || s.phase === 'checking') return drawUpdate(s);
  clearInterval(upd.timer);
  if (s.phase === 'failed') {
    const sum = /checksum/.test(s.error);
    return failUpdate(sum ? 'The update was not installed' : 'The update did not download', s.error, 'Try again', startUpdate);
  }
  if (s.phase === 'ready') return installUpdate();
  return drawUpdate(); // cancelled
}

async function cancelUpdate() {
  clearInterval(upd.timer);
  await api('/api/update/cancel', 'POST').catch(() => {});
  drawUpdate();
  tell('Update cancelled. Nothing was changed.');
}

function failUpdate(title, why, label, run) {
  upd.failed = { title, why: why.charAt(0).toUpperCase() + why.slice(1), label, run: () => { upd.failed = ''; run(); } };
  drawUpdate();
}

// installUpdate saves the tabs, hands over to Setup, and closes the window.
async function installUpdate() {
  await flushTabs();
  tabs.frozen = true; // nothing is saved while Setup replaces the app
  setPref('updates.from', bar.version);
  modal('Restarting Filiation', [para('Your tabs are saved. The window closes now and the new version opens by itself in a moment.')], []);
  try {
    await api('/api/update/install', 'POST');
  } catch (e) {
    $('modal').close();
    tabs.frozen = false;
    setPref('updates.from', '');
    return failUpdate('The update did not finish', e.message + ' You still have ' + bar.version + '.', 'Download it yourself', () => openOutside(upd.info ? upd.info.page : 'https://github.com/codevector-2003/filiation/releases/latest'));
  }
  setTimeout(() => { if (window.runtime) window.runtime.Quit(); }, 400);
  return undefined;
}

// aboutUpdates is the line About gives updates.
function aboutUpdates() {
  if (!autoUpdates()) return 'Checked only when you ask (Help, Check for Updates…)';
  const at = Number(pref('updates.checked') || 0);
  if (!at) return 'Checked once a day';
  const d = new Date(at);
  const today = new Date().toDateString() === d.toDateString();
  return 'Checked once a day; last ' + (today ? 'today at ' + d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString());
}
