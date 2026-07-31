// Live end-to-end check: runs web/shared.js renderRoutePlan against the real
// running server (http://127.0.0.1:7777) with the real session + real trader
// endpoints, then inspects the rendered trader-centric HTML.
// Run: node test/routeplan-live.test.js
const fs = require('fs');
const vm = require('vm');
const path = require('path');
const assert = require('assert');

const code = fs.readFileSync(path.join(__dirname, '..', 'web', 'shared.js'), 'utf8');

function stubEl() {
  return {
    innerHTML: '', textContent: '', value: '', style: {},
    dataset: {}, open: false,
    classList: { add() {}, toggle() {}, contains: () => false },
    addEventListener() {}, appendChild() {}, querySelector: () => stubEl(),
    querySelectorAll: () => [], setAttribute() {}, remove() {}, focus() {},
    scrollIntoView() {},
  };
}

function makeSandbox(fetchImpl) {
  const resultsEl = stubEl();
  const document = new Proxy({}, {
    get(t, prop) {
      if (prop === 'querySelector') return sel => (sel === '#route-plan-results' ? resultsEl : stubEl());
      if (prop === 'querySelectorAll') return () => [];
      if (prop === 'createElement') return () => stubEl();
      if (prop === 'addEventListener') return () => {};
      if (prop === 'body') return stubEl();
      return undefined;
    },
  });
  const sandbox = {
    document,
    window: { addEventListener() {}, location: { search: '' } },
    localStorage: { getItem: () => null, setItem() {} },
    fetch: fetchImpl,
    console, AbortController, setTimeout, clearTimeout,
    setInterval: () => 0, Date,
    CanvasRenderingContext2D: { prototype: {} },
  };
  vm.runInNewContext(code, sandbox, { filename: 'shared.js' });
  vm.runInNewContext('state.currentView = "tracker"', sandbox);
  return { sandbox, resultsEl };
}

async function main() {
  const liveFetch = async (url, opts) => {
    const res = await fetch('http://127.0.0.1:7777' + url, opts);
    const text = await res.text();
    return { ok: res.ok, status: res.status, json: async () => { try { return JSON.parse(text); } catch { return null; } } };
  };

  const t = makeSandbox(liveFetch);
  await vm.runInNewContext('renderRoutePlan()', t.sandbox);
  const html = t.resultsEl.innerHTML;

  console.log('--- rendered HTML (first 1200 chars) ---');
  console.log(html.slice(0, 1200));
  console.log('----------------------------------------');

  // Live session (seeded): Leather Armor/Mushroom/Steel Ore → sell, Maple Wood → favor.
  const hasCards = html.includes('route-trader-card');
  const hasSell = html.includes('route-section-title sell');
  const hasFavor = html.includes('route-section-title favor');
  const hasKm = /\(\d+(\.\d+)? km\)/.test(html);

  console.log('trader cards rendered:', hasCards);
  console.log('sell sections rendered:', hasSell);
  console.log('favor sections rendered:', hasFavor);
  console.log('distance badges (km):', hasKm);

  assert(hasCards, 'expected trader-centric cards in live render');
  assert(hasSell, 'expected sell sections');
  assert(hasFavor, 'expected favor sections (Maple Wood → Kohan etc)');
  assert(html.includes('Christina Fells'), 'expected Christina Fells card (Leather Armor seller)');
  assert(html.includes('Leather Armor'), 'expected Leather Armor item listed');

  // distance_km is null against the real CDN (no coords) → no (x.x km) badges.
  assert(!hasKm, 'no distance badges expected: real areas.json has no coordinates');

  console.log('LIVE ROUTE PLAN RENDER OK');
}

main().catch(e => { console.error('FAIL:', e.message); process.exit(1); });
