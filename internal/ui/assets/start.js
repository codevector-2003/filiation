// Starts the app, once every screen's script has loaded.
'use strict';

fetch('logos.svg')
  .then((r) => r.text())
  .then((svg) => {
    // Our own embedded file, so it is safe to put in the page as markup.
    const box = $('logos');
    box.innerHTML = svg;
    box.hidden = false;
  })
  .catch(() => {});

// Dropped PDFs go straight to the Inbox.
if (window.runtime && window.runtime.OnFileDrop) {
  window.runtime.OnFileDrop((x, y, paths) => { dropPDFs(paths); }, false);
}

startTitleBar()
  .then(() => loadLibrary())
  .then((ready) => {
    if (!ready) return undefined;
    pollJob();
    return loadPapers();
  })
  .catch((e) => { $('rows').replaceChildren(el('div', 'note error', e.message)); });
