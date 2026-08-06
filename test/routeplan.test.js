// Checks the trader-centric route plan render + item-centric fallback in web/shared.js.
// Run: node test/routeplan.test.js
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
    CanvasRenderingContext2D: { prototype: {} }, // shared.js polyfills roundRect at load
  };
  vm.runInNewContext(code, sandbox, { filename: 'shared.js' });
  vm.runInNewContext('state.currentView = "tracker"', sandbox);
  return { sandbox, resultsEl };
}

const httpRes = (body, ok = true) => ({ ok, status: ok ? 200 : 404, json: async () => body });

const SESSION = {
  loot: [
    { name: 'Apples', count: 3, value: 50, decision: { verdict: 'sell_vendor', sell_reason: 'vendor' } },
    { name: 'Steel', count: 1, value: 200, decision: { verdict: 'sell_consignment', sell_reason: '' } },
    { name: 'Meat', count: 2, value: 5, decision: { verdict: 'favor', favor_targets: [{ npc: 'Farmer Fred', area: 'Cocoon', score: 3.5 }] } },
    { name: 'Junk', count: 1, value: 1, decision: { verdict: 'keep' } },
  ],
};

async function main() {
  // 1) trader-centric: nearest first, distance shown, sell/favor totals, keep block preserved
  const t1 = makeSandbox(async path => {
    if (path.startsWith('/api/route-planner?trader=')) {
      const name = decodeURIComponent(path.split('trader=')[1]);
      const map = {
        'Farmer Fred': {
          trader: 'Farmer Fred', area: 'Cocoon', distance_km: 12.5,
          sell_items: [{ name: 'Apples', count: 3, value: 50 }, { name: 'Steel', count: 1, value: 200 }],
          favor_items: [{ name: 'Meat', count: 2, favor_score: 3.5 }],
        },
        'Distant Dan': { trader: 'Distant Dan', area: 'Serbule' }, // no distance_km
      };
      return httpRes(map[name] || null);
    }
    if (path.startsWith('/api/route-planner?item=')) {
      const item = decodeURIComponent(path.split('item=')[1]).split('&')[0];
      const routes = item === 'Apples'
        ? [{ trader: 'Farmer Fred', area: 'Cocoon' }, { trader: 'Distant Dan', area: 'Serbule' }]
        : [{ trader: 'Farmer Fred', area: 'Cocoon' }];
      return httpRes({ item, routes });
    }
    if (path === '/api/session') return httpRes(SESSION);
    return httpRes([]);
  });
  await vm.runInNewContext('renderRoutePlan()', t1.sandbox);
  const html1 = t1.resultsEl.innerHTML;
  assert(html1.includes('📍 Farmer Fred') && html1.includes('(12.5 km)'), 'nearest trader with distance');
  assert(html1.includes('📍 Distant Dan') && !html1.includes('25.0 km'), 'trader without distance shows no km');
  assert(html1.indexOf('Farmer Fred') < html1.indexOf('Distant Dan'), 'sorted by distance, nearest first');
  assert(html1.includes('💰 Sell (2 items, 350g)'), 'sell section with total value');
  assert(html1.includes('❤️ Favor (1 item, +3.5 score)'), 'favor section with total score');
  assert(html1.includes('x3') && html1.includes('x2'), 'item counts shown');
  assert(html1.includes('📦 Keep') && html1.includes('Junk'), 'keep block preserved');

  // 2) ?trader= endpoint missing → item-centric fallback + API marked unavailable
  const t2 = makeSandbox(async path => {
    if (path.startsWith('/api/route-planner?trader=')) return httpRes(null, false);
    if (path.startsWith('/api/route-planner?item=')) return httpRes({ item: 'x', routes: [] });
    if (path === '/api/session') return httpRes(SESSION);
    return httpRes([]);
  });
  await vm.runInNewContext('renderRoutePlan()', t2.sandbox);
  const html2 = t2.resultsEl.innerHTML;
  assert(html2.includes('route-plan-block-title favor') && html2.includes('route-plan-block-title sell'), 'item-centric fallback rendered');
  assert(vm.runInNewContext('__traderApiUnavailable', t2.sandbox) === true, 'API marked unavailable after total failure');

  // 3) after the flag, no trader fetches are attempted anymore
  let traderCalls = 0;
  const t3 = makeSandbox(async path => {
    if (path.startsWith('/api/route-planner?trader=')) { traderCalls++; return httpRes(null, false); }
    if (path.startsWith('/api/route-planner?item=')) return httpRes({ item: 'x', routes: [] });
    if (path === '/api/session') return httpRes(SESSION);
    return httpRes([]);
  });
  await vm.runInNewContext('renderRoutePlan()', t3.sandbox);
  assert(traderCalls === 1, 'first render tried the trader endpoint once');
  await vm.runInNewContext('renderRoutePlan()', t3.sandbox);
  assert(traderCalls === 1, 'no trader fetches after unavailable flag set');

  console.log('route plan tests passed');
}

main().catch(e => { console.error(e.message); process.exit(1); });
