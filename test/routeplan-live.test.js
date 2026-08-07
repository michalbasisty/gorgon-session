// Live end-to-end check: runs web/shared.js renderRoutePlan against the real
// running server (http://127.0.0.1:7777) with the real session + real item
// endpoints, then inspects the rendered item-centric HTML (one trader per sell
// item + ⇄ switch).
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
  // Sell rows are found via document.querySelectorAll('.route-plan-item');
  // materialize stubs from the items already rendered into resultsEl, each with
  // a .route-plan-routes child whose innerHTML records planRoutesFor output.
  const routesStubs = {};
  const document = new Proxy({}, {
    get(t, prop) {
      if (prop === 'querySelector') return sel => (sel === '#route-plan-results' ? resultsEl : stubEl());
      if (prop === 'querySelectorAll') return sel => {
        if (sel !== '.route-plan-item') return [];
        const names = [...resultsEl.innerHTML.matchAll(/data-name="([^"]+)"/g)].map(m => m[1]);
        return names.map(name => {
          const routes = stubEl();
          routesStubs[name] = routes;
          const row = stubEl();
          row.dataset.name = name;
          row.querySelector = s => (s === '.route-plan-routes' ? routes : stubEl());
          return row;
        });
      };
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
  return { sandbox, resultsEl, routesStubs };
}

async function main() {
  const liveFetch = async (url, opts) => {
    const res = await fetch('http://127.0.0.1:7777' + url, opts);
    const text = await res.text();
    return { ok: res.ok, status: res.status, json: async () => { try { return JSON.parse(text); } catch { return null; } }, text: async () => text };
  };

  const t = makeSandbox(liveFetch);
  await vm.runInNewContext('renderRoutePlan()', t.sandbox);
  const html = t.resultsEl.innerHTML;

  console.log('--- rendered HTML (first 1200 chars) ---');
  console.log(html.slice(0, 1200));
  console.log('----------------------------------------');

  // Live session (seeded): Leather Armor/Mushroom/Steel Ore → sell, Maple Wood → favor.
  const hasSell = html.includes('route-plan-block-title sell');
  const hasFavor = html.includes('route-plan-block-title favor');
  const hasKeep = html.includes('route-plan-block-title keep');

  console.log('sell block rendered:', hasSell);
  console.log('favor block rendered:', hasFavor);
  console.log('keep block rendered:', hasKeep);

  assert(hasSell, 'expected sell block in live render');
  assert(hasFavor, 'expected favor block in live render');
  assert(html.includes('route-plan-item'), 'expected item rows in live render');

  // Trader rows are data-dependent (sell items must match trader keywords in the
  // live favor DB); the deterministic mock test covers that. Report, don't assert.
  const anyTraderRow = Object.values(t.routesStubs).some(r => r.innerHTML.includes('route-plan-link'));
  const hasSwitch = Object.values(t.routesStubs).some(r => r.innerHTML.includes('route-plan-switch'));
  console.log('trader suggestion rows:', anyTraderRow);
  console.log('switch buttons present:', hasSwitch);

  console.log('LIVE ROUTE PLAN RENDER OK');
}

main().catch(e => { console.error('FAIL:', e.message); process.exit(1); });
