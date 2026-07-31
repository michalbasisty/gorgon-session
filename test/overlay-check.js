// Drives headless Edge via CDP: loads /?overlay=1 at 460px, clicks the Tracker
// tab, waits for the route plan to render, then reports panel/card state and
// horizontal overflow. Run: node test/overlay-check.js
const http = require('http');

const CDP_PORT = 9223;

function httpJson(path) {
  return new Promise((resolve, reject) => {
    http.get({ host: '127.0.0.1', port: CDP_PORT, path }, res => {
      let d = '';
      res.on('data', c => (d += c));
      res.on('end', () => resolve(JSON.parse(d)));
    }).on('error', reject);
  });
}

async function main() {
  const targets = await httpJson('/json/list');
  const page = targets.find(t => t.type === 'page');
  if (!page) throw new Error('no page target');
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  let id = 0;
  const pending = new Map();
  const send = (method, params = {}) => new Promise((resolve, reject) => {
    const mid = ++id;
    pending.set(mid, { resolve, reject });
    ws.send(JSON.stringify({ id: mid, method, params }));
  });
  ws.onmessage = ev => {
    const m = JSON.parse(ev.data);
    if (m.id && pending.has(m.id)) { pending.get(m.id).resolve(m.result); pending.delete(m.id); }
  };
  await new Promise(r => (ws.onopen = r));

  await send('Page.enable');
  await send('Runtime.enable');
  await send('Emulation.setDeviceMetricsOverride', { width: 460, height: 800, deviceScaleFactor: 1, mobile: false });
  await send('Page.navigate', { url: 'http://127.0.0.1:7777/?overlay=1' });
  await new Promise(r => setTimeout(r, 6000)); // let app boot + polls settle

  const evalJs = async expr => {
    const r = await send('Runtime.evaluate', { expression: expr, returnByValue: true, awaitPromise: true });
    if (!r) return 'NO RESULT';
    const res = r.result;
    if (res && res.exceptionDetails) {
      console.error('EVAL ERROR:', JSON.stringify(res.exceptionDetails.exception?.description || res.exceptionDetails.text));
      return undefined;
    }
    return res && res.result ? res.result.value : JSON.stringify(r).slice(0, 300);
  };

  // Sanity probe before clicking.
  const sanity = await evalJs(`(() => ({ title: document.title, hasTracker: !!document.querySelector('.overlay-nav[data-view="tracker"]') }))()`);
  console.log('SANITY:', JSON.stringify(sanity));

  // Click the Tracker nav item (overlay top bar).
  await evalJs(`(() => { const b = document.querySelector('.overlay-nav[data-view="tracker"]'); if (b) { b.click(); return 'clicked'; } return 'no-tracker-btn'; })()`);
  await new Promise(r => setTimeout(r, 6000)); // trader fetches + render

  const report = await evalJs(`(() => {
    const results = document.getElementById('route-plan-results');
    const panel = document.getElementById('route-plan-panel');
    const html = document.documentElement;
    return JSON.stringify({
      overlayMode: document.body.classList.contains('overlay-mode'),
      view: document.getElementById('view-tracker') ? (document.getElementById('view-tracker').classList.contains('hidden') ? 'not-active' : 'active') : 'missing',
      panel: !!panel,
      panelOpen: panel ? panel.open : false,
      cards: document.querySelectorAll('.route-trader-card').length,
      sellSections: document.querySelectorAll('.route-section-title.sell').length,
      favorSections: document.querySelectorAll('.route-section-title.favor').length,
      kmBadges: document.querySelectorAll('.route-trader-distance').length,
      firstCard: results ? (results.querySelector('.route-trader-head')?.textContent || '') : 'no-results',
      docScrollWidth: html.scrollWidth,
      docClientWidth: html.clientWidth,
      noSidewaysOverflow: html.scrollWidth <= html.clientWidth,
      cardOverflow: Array.from(document.querySelectorAll('.route-trader-card')).map(c => c.scrollWidth > c.clientWidth).some(Boolean)
    });
  })()`);
  console.log('REPORT:', report);
  ws.close();
}

main().catch(e => { console.error('FAIL:', e.message); process.exit(1); });
