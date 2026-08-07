// Checks the aggregated route plan render (sell items grouped by trader, ⇄ switch per row) in web/shared.js.
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

function makeSandbox(fetchImpl, names = ['Apples', 'Steel']) {
  const resultsEl = stubEl();
  const checklistEl = stubEl();
  // Sell rows; each carries a .route-plan-routes child whose
  // innerHTML records what planRoutesFor renders into it.
  // Favor rows carry a .route-plan-favor-routes child instead; the
  // same stub is returned for either selector, so the innerHTML also
  // records what planFavorRoutesFor renders.
  const routesStubs = {};
  const rowStubs = names.map(name => {
    const routes = stubEl();
    routesStubs[name] = routes;
    const row = stubEl();
    row.dataset.name = name;
    row.querySelector = sel => (sel === '.route-plan-routes' || sel === '.route-plan-favor-routes' ? routes : stubEl());
    return row;
  });
  const document = new Proxy({}, {
    get(t, prop) {
      if (prop === 'querySelector') return sel => (sel === '#route-plan-results' ? resultsEl : sel === '#sell-checklist-body' ? checklistEl : stubEl());
      if (prop === 'querySelectorAll') return sel => (sel === '.route-plan-item' ? rowStubs : []);
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
  return { sandbox, resultsEl, routesStubs, checklistEl };
}

const httpRes = (body, ok = true) => ({ ok, status: ok ? 200 : 404, json: async () => body, text: async () => (body === undefined ? '' : JSON.stringify(body)) });

const SESSION = {
  loot: [
    { name: 'Apples', count: 3, value: 50, decision: { verdict: 'sell_vendor', sell_reason: 'vendor' } },
    { name: 'Steel', count: 1, value: 200, decision: { verdict: 'sell_consignment', sell_reason: '' } },
    { name: 'Meat', count: 2, value: 5, decision: { verdict: 'favor', favor_targets: [{ npc: 'Freda Farmhand', area: 'Cocoon', score: 3.5, distance_km: 12.5 }] } },
    { name: 'Blueberries', count: 1, value: 1, decision: { verdict: 'favor', favor_targets: [{ npc: 'Foodie Joe', area: 'Serbule', score: 15, distance_km: 150 }, { npc: 'Freda Farmhand', area: 'Cocoon', score: 5, distance_km: 12.5 }] } },
    { name: 'Tome', count: 1, value: 1, decision: { verdict: 'favor', favor_targets: [{ npc: 'Corey The Croaker', area: 'Winter Nexus', score: 2 }] } },
    { name: 'Bone', count: 1, value: 1, decision: { verdict: 'favor', favor_targets: [] } },
    { name: 'Junk', count: 1, value: 1, decision: { verdict: 'keep' } },
  ],
};

async function main() {
  // 1) aggregated sell route: items grouped by nearest trader, group header shows
  //    trader + distance, per-row ⇄ switch + picker listing all traders
  let traderDetailCalls = 0; // the old ?trader= endpoint must never be hit
  const t1 = makeSandbox(async path => {
    if (path.startsWith('/api/route-planner?trader=')) { traderDetailCalls++; return httpRes(null, false); }
    if (path.startsWith('/api/route-planner?item=')) {
      const item = decodeURIComponent(path.split('item=')[1]).split('&')[0];
      const routes = item === 'Apples'
        ? [
          { trader: 'Farmer Fred', area: 'Cocoon', distance_km: 12.5 },
          { trader: 'Distant Dan', area: 'Serbule' }, // no distance_km
        ]
        : [{ trader: 'Farmer Fred', area: 'Cocoon' }];
      return httpRes({ item, routes });
    }
    if (path === '/api/session') return httpRes(SESSION);
    return httpRes([]);
  }, ['Apples', 'Steel', 'Meat', 'Blueberries', 'Tome', 'Bone']);
  // 1a) default render is SELL mode: tabs present, sell block + maps, no favor
  await vm.runInNewContext('renderRoutePlan()', t1.sandbox);
  const html1 = t1.resultsEl.innerHTML;
  const applesRoutes = t1.routesStubs.Apples.innerHTML;
  assert(traderDetailCalls === 0, 'no ?trader= detail fetches in item-centric view');
  assert(html1.includes('route-plan-tabs'), 'mode tabs present');
  assert(html1.includes('data-mode="sell"') && html1.includes('route-plan-tab active'), 'sell tab active by default');
  assert(html1.includes('route-plan-block-title sell'), 'sell block rendered in sell mode');
  assert(!html1.includes('route-plan-block-title favor'), 'favor block hidden in sell mode');
  assert(html1.includes('📦 Keep') && html1.includes('Junk'), 'keep block visible in sell mode');
  assert(html1.includes('route-plan-group'), 'sell items wrapped in trader groups');
  assert(html1.includes('route-plan-group-header') && html1.includes('Farmer Fred') && html1.includes('12.5 km'), 'group header shows nearest trader with distance');
  assert(html1.includes('route-plan-group-items') && html1.includes('Apples') && html1.includes('Steel'), 'items grouped under the same nearest trader');
  const sellRegion = html1.slice(html1.indexOf('💰 Sell route'));
  assert(sellRegion.includes('route-plan-map') && sellRegion.includes('📍 Cocoon') && sellRegion.includes('12.5 km'), 'sell trader groups nested under a map section');
  assert(applesRoutes.includes('route-plan-switch'), '⇄ switch button present for items with multiple traders');
  assert(applesRoutes.includes('route-plan-picker') && applesRoutes.includes('Distant Dan'), 'picker lists all traders for the item');
  assert(applesRoutes.includes('hidden'), 'picker collapsed by default');

  // 1b) switch to FAVOR mode: favor block + maps, sell hidden, keep still visible
  await vm.runInNewContext(`__routePlanMode = 'favor'; renderRoutePlan();`, t1.sandbox);
  const htmlF = t1.resultsEl.innerHTML;
  const meatRoutes = t1.routesStubs.Meat.innerHTML;
  const blueberryRoutes = t1.routesStubs.Blueberries.innerHTML;
  assert(htmlF.includes('route-plan-tabs') && htmlF.includes('data-mode="favor"') && htmlF.includes('route-plan-tab active'), 'favor tab active in favor mode');
  assert(htmlF.includes('route-plan-block-title favor') && htmlF.includes('Freda Farmhand') && htmlF.includes('Cocoon'), 'favor block rendered in favor mode');
  assert(!htmlF.includes('route-plan-block-title sell'), 'sell block hidden in favor mode');
  assert(htmlF.includes('📦 Keep') && htmlF.includes('Junk'), 'keep block visible in favor mode');

  // favor items grouped by target NPC; maps sorted by distance (Cocoon 12.5 km
  // before Serbule 150 km), unknown-distance map (Winter Nexus) last
  assert(htmlF.includes('route-plan-group') && htmlF.includes('+3.5') && htmlF.includes('Freda Farmhand'), 'Meat grouped under a Freda Farmhand favor group with score');
  assert(htmlF.includes('12.5 km · +3.5'), 'favor group header shows distance and score');
  const favorRegion = htmlF.slice(htmlF.indexOf('Give as favor'), htmlF.indexOf('📦 Keep'));
  const cocoonMapIdx = favorRegion.indexOf('📍 Cocoon'), serbuleMapIdx = favorRegion.indexOf('📍 Serbule'), nexusMapIdx = favorRegion.indexOf('📍 Winter Nexus');
  const fredaIdx = favorRegion.indexOf('Freda Farmhand'), joeIdx = favorRegion.indexOf('Foodie Joe'), coreyIdx = favorRegion.indexOf('Corey The Croaker');
  assert(cocoonMapIdx !== -1 && serbuleMapIdx !== -1 && nexusMapIdx !== -1, 'all three favor maps present');
  assert(cocoonMapIdx < serbuleMapIdx && serbuleMapIdx < nexusMapIdx, 'favor maps ordered by distance: Cocoon (12.5) < Serbule (150) < Winter Nexus (unknown)');
  assert(fredaIdx !== -1 && joeIdx !== -1 && coreyIdx !== -1 && fredaIdx < joeIdx && joeIdx < coreyIdx, 'nearest-map target groups before farther ones');
  assert((favorRegion.match(/route-plan-map-header/g) || []).length === 3, 'favor block has three map sections');
  assert((favorRegion.match(/📍 Cocoon/g) || []).length === 1, 'single Cocoon map section in favor block');
  assert(favorRegion.indexOf('📍 Winter Nexus') < favorRegion.indexOf('Corey The Croaker'), 'unknown-area map still holds its item group');
  assert(htmlF.includes('no favor targets') && htmlF.includes('Bone'), 'no-target favor item rendered in trailing group');
  assert(meatRoutes === '', 'single-target favor item renders no ⇄ switch');
  assert(blueberryRoutes.includes('route-plan-switch'), '⇄ switch present for multi-target favor item');
  assert(blueberryRoutes.includes('route-plan-picker') && blueberryRoutes.includes('hidden'), 'favor picker collapsed by default');
  assert(blueberryRoutes.includes('Foodie Joe') && blueberryRoutes.includes('Freda Farmhand'), 'favor picker lists both targets');
  assert(blueberryRoutes.includes('+15 ✓') && !blueberryRoutes.includes('+5 ✓'), '✓ marks the best-score target (Foodie Joe)');
  assert((blueberryRoutes.match(/✓/g) || []).length === 1, 'exactly one ✓ in the favor picker');

  // switching the favor pick moves the item into the chosen target's group
  await vm.runInNewContext(`__routeFavorPick['Blueberries'] = 'Freda Farmhand'; renderRoutePlan();`, t1.sandbox);
  const html1b = t1.resultsEl.innerHTML;
  assert(!html1b.includes("showTraderHistory('Foodie Joe')"), 'Foodie Joe group header gone after pick switches away');
  assert(html1b.indexOf('Freda Farmhand') !== -1 && html1b.indexOf('Blueberries') > html1b.indexOf('Freda Farmhand'), 'Blueberries regrouped under Freda Farmhand');
  const favorRegionB = html1b.slice(html1b.indexOf('Give as favor'), html1b.indexOf('📦 Keep'));
  assert((favorRegionB.match(/route-plan-map-header/g) || []).length === 2, 'two map sections left in favor block after pick (Cocoon + Winter Nexus)');
  assert(favorRegionB.indexOf('📍 Cocoon') !== -1 && favorRegionB.indexOf('📍 Serbule') === -1 && favorRegionB.indexOf('Blueberries') > favorRegionB.indexOf('Meat'), 'Blueberries joins Meat in the same Cocoon map section');

  // 2) single-trader item: no switch button, no picker
  const t2 = makeSandbox(async path => {
    if (path.startsWith('/api/route-planner?item=')) {
      return httpRes({ item: 'x', routes: [{ trader: 'Farmer Fred', area: 'Cocoon' }] });
    }
    if (path === '/api/session') return httpRes(SESSION);
    return httpRes([]);
  });
  await vm.runInNewContext('renderRoutePlan()', t2.sandbox);
  const html2 = t2.resultsEl.innerHTML;
  assert(!t2.routesStubs.Steel.innerHTML.includes('route-plan-switch'), 'no switch button when only one trader');

  // 3) group ordering: nearest map first (one map section per area), unknown-area
  //    map after known maps, 'no traders found' group trailing
  const t3 = makeSandbox(async path => {
    if (path.startsWith('/api/route-planner?item=')) {
      const item = decodeURIComponent(path.split('item=')[1]).split('&')[0];
      const routes = {
        Gem: [
          { trader: 'Distant Dan', area: 'Serbule', distance_km: 5 }, // nearest → Gem's effective trader
          { trader: 'Farmer Fred', area: 'Cocoon', distance_km: 50 },
        ],
        Sword: [{ trader: 'Farmer Fred', area: 'Cocoon', distance_km: 12 }],
        Ring: [{ trader: 'Nearby Nell', area: 'Serbule', distance_km: 8 }], // second Serbule trader → shared map section
        Pebble: [{ trader: 'Mystery Mike', area: '' }], // empty area → unknown-area map
        Trinket: [], // no traders
      }[item];
      return httpRes({ item, routes });
    }
    if (path === '/api/session') return httpRes({
      loot: [
        { name: 'Gem', count: 1, value: 10, decision: { verdict: 'sell_vendor' } },
        { name: 'Sword', count: 1, value: 20, decision: { verdict: 'sell_vendor' } },
        { name: 'Ring', count: 1, value: 15, decision: { verdict: 'sell_vendor' } },
        { name: 'Pebble', count: 1, value: 1, decision: { verdict: 'sell_vendor' } },
        { name: 'Trinket', count: 1, value: 1, decision: { verdict: 'sell_vendor' } },
      ],
    });
    return httpRes([]);
  }, ['Gem', 'Sword', 'Ring', 'Pebble', 'Trinket']);
  await vm.runInNewContext('renderRoutePlan()', t3.sandbox);
  const html3 = t3.resultsEl.innerHTML;
  const danIdx = html3.indexOf('Distant Dan'), fredIdx = html3.indexOf('Farmer Fred'), noTraderIdx = html3.indexOf('no traders found');
  const serbuleIdx = html3.indexOf('📍 Serbule'), cocoonIdx = html3.indexOf('📍 Cocoon'), mysteryIdx = html3.indexOf('Mystery Mike'), unknownIdx = html3.indexOf('unknown area');
  assert(danIdx !== -1 && fredIdx !== -1 && noTraderIdx !== -1, 'all three groups present');
  assert((html3.match(/📍 Serbule/g) || []).length === 1, 'one Serbule map section for both Serbule traders');
  assert(serbuleIdx !== -1 && html3.indexOf('Distant Dan') > serbuleIdx && html3.indexOf('Nearby Nell') > serbuleIdx && cocoonIdx > serbuleIdx, 'both Serbule traders share one map section before the Cocoon map');
  assert(danIdx < fredIdx, 'nearest map first (Serbule 5 km before Cocoon 12 km)');
  assert(fredIdx < noTraderIdx, 'no-traders group trails the map sections');
  assert(unknownIdx !== -1 && mysteryIdx > unknownIdx && mysteryIdx < noTraderIdx, 'empty-area trader under unknown-area map, before no-traders group');
  assert(html3.includes('route-plan-group-items') && html3.includes('Trinket'), 'no-route item rendered in trailing group');

  // 4) sell checklist: trader rows nested under map sections, nearest map first
  const t4 = makeSandbox(async path => {
    if (path.startsWith('/api/route-planner?item=')) {
      const item = decodeURIComponent(path.split('item=')[1]).split('&')[0];
      const routes = {
        'Iron Ore': [{ trader: 'Mira', area: 'Serbule', distance_km: 5 }],
        'Maple Wood': [{ trader: 'Farmer Fred', area: 'Cocoon', distance_km: 12.5 }, { trader: 'Mira', area: 'Serbule', distance_km: 5 }],
        'Bone Meal': [{ trader: 'Mystery Mike', area: '' }],
      }[item];
      return httpRes({ item, routes: routes || [] });
    }
    if (path === '/api/session') return httpRes({
      loot: [
        { name: 'Iron Ore', count: 1, value: 100, decision: { verdict: 'sell_vendor' } },
        { name: 'Maple Wood', count: 1, value: 80, decision: { verdict: 'sell_vendor' } },
        { name: 'Bone Meal', count: 1, value: 60, decision: { verdict: 'sell_vendor' } },
      ],
    });
    return httpRes([]);
  }, []);
  await vm.runInNewContext('renderSellChecklist()', t4.sandbox);
  const checkHtml = t4.checklistEl.innerHTML;
  const serbuleMap = checkHtml.indexOf('📍 Serbule'), cocoonMap = checkHtml.indexOf('📍 Cocoon'), unknownMap = checkHtml.indexOf('📍 unknown area');
  assert(serbuleMap !== -1 && cocoonMap !== -1 && unknownMap !== -1, 'all three checklist map sections present');
  assert(serbuleMap < cocoonMap && cocoonMap < unknownMap, 'nearest checklist map (Serbule 5 km) first, unknown-area last');
  assert(checkHtml.includes('sell-check-row') && checkHtml.includes('data-trader="Mira"') && checkHtml.indexOf('Mira') > serbuleMap, 'checkbox rows rendered inside their map sections');
  assert((checkHtml.match(/sell-check-row/g) || []).length === 3, 'one checkbox row per trader');

  console.log('route plan tests passed');
}

main().catch(e => { console.error(e.message); process.exit(1); });
