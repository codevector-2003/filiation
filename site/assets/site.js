// Shared by both pages: copy buttons, the phone menu, the star count, and
// download links that point at the newest release for the visitor's system.
// Every piece works without the network: links fall back to the releases page.
(() => {
  const REPO = 'codevector-2003/filiation';
  const RELEASES = 'https://github.com/' + REPO + '/releases/latest';

  // Copy buttons copy the text of the code beside them.
  document.addEventListener('click', (e) => {
    const btn = e.target.closest('.copy');
    if (!btn) return;
    const code = btn.parentElement.querySelector('code');
    if (!code || !navigator.clipboard) return;
    navigator.clipboard.writeText(code.textContent.trim()).then(() => {
      const was = btn.textContent;
      btn.textContent = 'Copied';
      setTimeout(() => { btn.textContent = was; }, 1400);
    }, () => {});
  });

  // The colour theme: as the system, light, or dark, in that order. The
  // choice is kept in this browser; the page's head applies it before drawing.
  const themes = ['system', 'light', 'dark'];
  const themeName = { system: 'as your system', light: 'light', dark: 'dark' };
  const themeBtn = document.querySelector('.theme-btn');
  const currentTheme = () => document.documentElement.dataset.theme || 'system';
  const labelTheme = () => {
    if (!themeBtn) return;
    const now = currentTheme();
    const next = themes[(themes.indexOf(now) + 1) % themes.length];
    themeBtn.setAttribute('aria-label', 'Colour theme: ' + themeName[now] + '. Switch to ' + themeName[next]);
    themeBtn.title = 'Theme: ' + themeName[now];
  };
  if (themeBtn) {
    themeBtn.addEventListener('click', () => {
      const next = themes[(themes.indexOf(currentTheme()) + 1) % themes.length];
      if (next === 'system') delete document.documentElement.dataset.theme;
      else document.documentElement.dataset.theme = next;
      try { if (next === 'system') localStorage.removeItem('fil-theme'); else localStorage.setItem('fil-theme', next); } catch (e) { /* private window */ }
      labelTheme();
    });
    labelTheme();
  }

  const menu = document.querySelector('.menu-btn');
  if (menu) {
    const nav = document.querySelector('.topnav');
    menu.addEventListener('click', () => {
      const open = nav.classList.toggle('open');
      menu.setAttribute('aria-expanded', String(open));
    });
    nav.addEventListener('click', (e) => { if (e.target.closest('a')) { nav.classList.remove('open'); menu.setAttribute('aria-expanded', 'false'); } });
  }

  // The system the visitor is on, as far as the browser says.
  const ua = (navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || navigator.userAgent || '';
  const os = /win/i.test(ua) ? 'windows' : /mac|iphone|ipad/i.test(ua) ? 'macos' : /linux|x11|cros/i.test(ua) ? 'linux' : '';
  document.documentElement.dataset.os = os;
  window.filOS = os;

  const get = (url) => fetch(url, { headers: { Accept: 'application/vnd.github+json' } }).then((r) => (r.ok ? r.json() : Promise.reject(r.status)));

  get('https://api.github.com/repos/' + REPO).then((repo) => {
    const n = repo.stargazers_count;
    if (typeof n !== 'number') return;
    const text = n >= 1000 ? (n / 1000).toFixed(n >= 10000 ? 0 : 1) + 'k' : String(n);
    document.querySelectorAll('[data-stars]').forEach((el) => { el.textContent = text; });
    document.querySelectorAll('.star').forEach((el) => el.setAttribute('aria-label', 'Star Filiation on GitHub, ' + n + ' stars'));
  }).catch(() => {});

  // Download links: [data-download="app|cli"], optionally with
  // data-os="windows|macos|linux" to pin a system instead of the visitor's.
  const fallback = () => document.querySelectorAll('[data-download]').forEach((a) => { a.href = RELEASES; });
  fallback();
  get('https://api.github.com/repos/' + REPO + '/releases/latest').then((rel) => {
    const version = String(rel.tag_name || '').replace(/^v/, '');
    const assets = rel.assets || [];
    const find = (re) => assets.find((a) => re.test(a.name));
    const cli = {
      windows: find(/^fil_.*_windows_amd64\.zip$/),
      macos: find(/^fil_.*_darwin_arm64\.tar\.gz$/),
      linux: find(/^fil_.*_linux_amd64\.tar\.gz$/),
    };
    const app = { windows: find(/^Filiation-Setup-.*-windows-amd64\.exe$/) };
    const url = (set) => Object.fromEntries(Object.entries(set).filter(([, a]) => a).map(([k, a]) => [k, a.browser_download_url]));
    window.filAssets = { cli: url(cli), app: url(app) };
    document.querySelectorAll('[data-download]').forEach((a) => {
      const which = a.dataset.os || os;
      const asset = (a.dataset.download === 'app' ? app : cli)[which];
      if (asset) a.href = asset.browser_download_url;
    });
    if (version) {
      window.filVersion = version;
      document.querySelectorAll('[data-version]').forEach((el) => { el.textContent = el.dataset.version.replace('{v}', version); });
      document.dispatchEvent(new CustomEvent('fil:version', { detail: version }));
    }
  }).catch(() => {});
})();
