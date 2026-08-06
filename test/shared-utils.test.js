// Unit tests for pure/rendering helpers in web/shared.js, web/settings-warcache.js, web/favor-traders.js.
// These files execute top-level DOM/event code on load, so they're loaded into a Node VM sandbox
// with a stubbed DOM (same approach as test/routeplan.test.js). Only pure functions are exercised.
// Run: node test/shared-utils.test.js
const fs = require('fs');
const vm = require('vm');
const path = require('path');
const assert = require('assert');

const web = p => path.join(__dirname, '..', 'web', p);

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

function loadSandbox() {
  const document = new Proxy({}, {
    get(t, prop) {
      if (prop === 'querySelector') return () => stubEl();
      if (prop === 'querySelectorAll') return () => [];
      if (prop === 'createElement') return () => stubEl();
      if (prop === 'addEventListener') return () => {};
      if (prop === 'body') return stubEl();
      return undefined;
    },
  });
  const sandbox = {
    document,
    // window === the context global, so `window.calcProfit = f` (and similar) create
    // real globals the scripts reference by bare name at load time (e.g. line 637).
    addEventListener() {},
    location: { search: '', origin: 'http://test' },
    localStorage: { getItem: () => null, setItem() {} },
    fetch: async () => ({ ok: true, status: 200, json: async () => [] }),
    console, AbortController, setTimeout, clearTimeout,
    setInterval: () => 0, Date,
    CanvasRenderingContext2D: { prototype: {} }, // shared.js polyfills roundRect at load
  };
  sandbox.window = sandbox;
  // shared.js must load first: it defines $/$$, state, api, toast, escapeHtml, …
  vm.runInNewContext(fs.readFileSync(web('shared.js'), 'utf8'), sandbox, { filename: 'shared.js' });
  // Avoid load-time renders (loadTraderCapacity awaits api, then may call renderDashboard).
  vm.runInNewContext('state.currentView = "tracker"', sandbox);
  vm.runInNewContext(fs.readFileSync(web('settings-warcache.js'), 'utf8'), sandbox, { filename: 'settings-warcache.js' });
  vm.runInNewContext(fs.readFileSync(web('favor-traders.js'), 'utf8'), sandbox, { filename: 'favor-traders.js' });
  return sandbox;
}

// vm contexts have their own Array.prototype; normalize to this realm before deep equality.
const j = x => JSON.parse(JSON.stringify(x));
const q = x => JSON.stringify(x); // build a JS literal arg for the vm context

let passed = 0;
function check(name, fn) { fn(); passed++; console.log('  ✓ ' + name); }

async function main() {
  const s = loadSandbox();
  const run = code => vm.runInNewContext(code, s);

  console.log('settings-warcache.js — warcache solver');
  check('warcacheGenerateAll: 12^4 ordered possibilities', () => {
    const all = j(run('warcacheGenerateAll()'));
    assert.strictEqual(all.length, Math.pow(12, 4));
    assert.deepStrictEqual(all[0], [0, 0, 0, 0]);
    assert.deepStrictEqual(all[all.length - 1], [11, 11, 11, 11]);
    assert.ok(all.every(p => p.length === 4));
  });

  check('warcacheComputeFeedback: mastermind counts', () => {
    assert.deepStrictEqual(j(run('warcacheComputeFeedback([0,0,0,0],[0,0,0,0])')), [4, 0]);
    assert.deepStrictEqual(j(run('warcacheComputeFeedback([0,1,2,3],[0,1,2,4])')), [3, 0]);
    assert.deepStrictEqual(j(run('warcacheComputeFeedback([0,1,2,3],[3,2,1,0])')), [0, 4]);
    // duplicates: 2 in place, 2 misplaced
    assert.deepStrictEqual(j(run('warcacheComputeFeedback([1,1,2,2],[1,2,1,2])')), [2, 2]);
    // no shared symbols
    assert.deepStrictEqual(j(run('warcacheComputeFeedback([0,0,0,0],[1,1,1,1])')), [0, 0]);
  });

  check('warcacheFilter: [4,0] narrows to exactly the guess', () => {
    run('warcachePossibilities = warcacheGenerateAll()');
    run('warcacheFilter([0,0,0,0],[4,0])');
    assert.deepStrictEqual(j(run('warcachePossibilities')), [[0, 0, 0, 0]]);
  });

  check('warcacheFilter: [0,0] keeps only 4-tuples without symbol 0', () => {
    run('warcachePossibilities = warcacheGenerateAll()');
    run('warcacheFilter([0,0,0,0],[0,0])');
    assert.strictEqual(run('warcachePossibilities.length'), Math.pow(11, 4)); // 14641
    assert.ok(run('warcachePossibilities.every(p => !p.includes(0))'));
  });

  check('warcacheFilter: resets the cached guess', () => {
    run('warcachePossibilities = warcacheGenerateAll()');
    run('warcacheCachedGuess = [9,9,9,9]');
    run('warcacheFilter([0,0,0,0],[4,0])');
    assert.strictEqual(run('warcacheCachedGuess'), null);
  });

  check('warcacheSuggest: empty set → null', () => {
    run('warcachePossibilities = []');
    assert.strictEqual(run('warcacheSuggest()'), null);
  });

  check('warcacheSuggest: single possibility returned as-is', () => {
    run('warcachePossibilities = [[2,3,4,5]]');
    assert.deepStrictEqual(j(run('warcacheSuggest()')), [2, 3, 4, 5]);
  });

  check('warcacheSuggest: minimax picks first of tied best splits', () => {
    run('warcachePossibilities = [[0,0,0,0],[1,1,1,1],[2,2,2,2],[3,3,3,3]]');
    assert.deepStrictEqual(j(run('warcacheSuggest()')), [0, 0, 0, 0]);
  });

  check('warcacheSuggest: large space uses cached random pick, no minimax', () => {
    run('warcachePossibilities = warcacheGenerateAll()');
    run('warcacheCachedGuess = null');
    const g1 = j(run('warcacheSuggest()'));
    const g2 = j(run('warcacheSuggest()'));
    assert.deepStrictEqual(g1, g2); // cached across calls
    assert.ok(run(`warcachePossibilities.some(p => p[0]===${g1[0]} && p[1]===${g1[1]} && p[2]===${g1[2]} && p[3]===${g1[3]})`));
  });

  console.log('shared.js — pure helpers');
  check('escapeHtml escapes &<>" and coerces null/undefined to ""', () => {
    assert.strictEqual(run(`escapeHtml(${q('<b>&"quoted"</b>')})`), '&lt;b&gt;&amp;&quot;quoted&quot;&lt;/b&gt;');
    assert.strictEqual(run('escapeHtml(null)'), '');
    assert.strictEqual(run('escapeHtml(undefined)'), '');
    assert.strictEqual(run('escapeHtml(0)'), '0');
  });

  check('fmtElapsed formats s/m/h', () => {
    assert.strictEqual(run('fmtElapsed(0)'), '0s');
    assert.strictEqual(run('fmtElapsed(999)'), '0s');
    assert.strictEqual(run('fmtElapsed(1000)'), '1s');
    assert.strictEqual(run('fmtElapsed(65000)'), '1m05s');
    assert.strictEqual(run('fmtElapsed(3600000)'), '1h00m');
    assert.strictEqual(run('fmtElapsed(3661000)'), '1h01m');
  });

  check('parseResetDurationMinutes parses "Nd Nh Nm" reset text', () => {
    assert.strictEqual(run('parseResetDurationMinutes("2d 3h 4m")'), 3064);
    assert.strictEqual(run('parseResetDurationMinutes("1d")'), 1440);
    assert.strictEqual(run('parseResetDurationMinutes("2h")'), 120);
    assert.strictEqual(run('parseResetDurationMinutes("90m")'), 90);
    assert.strictEqual(run('parseResetDurationMinutes("now")'), 0);
    assert.strictEqual(run('parseResetDurationMinutes("less than a minute")'), 0);
    assert.strictEqual(run('parseResetDurationMinutes("")'), Infinity);
    assert.strictEqual(run('parseResetDurationMinutes(undefined)'), Infinity);
  });

  check('routeSessionLabel formats session ids', () => {
    assert.strictEqual(run('routeSessionLabel("session-20260731-124047")'), '07-31 12:40');
    assert.strictEqual(run('routeSessionLabel("other")'), 'other');
    assert.strictEqual(run('routeSessionLabel("")'), '');
  });

  check('parseDotPartsSpec / formatDotPartsSpec round-trip', () => {
    const spec = 'Poison:381/10, Fire:80 over 10s@armor, Ice:30/5s';
    const parts = j(run(`parseDotPartsSpec(${q(spec)})`));
    assert.deepStrictEqual(parts, [
      { element: 'Poison', damage: 381, seconds: 10, target: 'health' },
      { element: 'Fire', damage: 80, seconds: 10, target: 'armor' },
      { element: 'Ice', damage: 30, seconds: 5, target: 'health' },
    ]);
    const fmt = run(`formatDotPartsSpec(${q(parts)})`);
    assert.strictEqual(fmt, 'Poison:381/10.0, Fire:80/10.0@armor, Ice:30/5.0');
    assert.deepStrictEqual(j(run(`parseDotPartsSpec(${q(fmt)})`)), parts);
    // invalid chunks are dropped
    assert.deepStrictEqual(j(run('parseDotPartsSpec("Bogus, Fire:80/10")')),
      [{ element: 'Fire', damage: 80, seconds: 10, target: 'health' }]);
  });

  check('parseDirectPartsSpec / formatDirectPartsSpec + target normalization', () => {
    assert.deepStrictEqual(j(run('parseDirectPartsSpec("Fire:40@armor, Ice:15, Bogus")')), [
      { element: 'Fire', damage: 40, target: 'armor' },
      { element: 'Ice', damage: 15, target: 'health' },
    ]);
    assert.strictEqual(
      run('formatDirectPartsSpec([{element:"Fire",damage:40,target:"armour"},{element:"Ice",damage:15,target:"health"}])'),
      'Fire:40@armor, Ice:15');
    assert.strictEqual(run('normalizeTargetKind("armour")'), 'armor');
    assert.strictEqual(run('normalizeTargetKind("health")'), 'health');
    assert.strictEqual(run('normalizeTargetKind("")'), 'health');
  });

  console.log('favor-traders.js — map-name matching');
  check('normalizeMapKey / isSameMapName', () => {
    assert.strictEqual(run('normalizeMapKey("Serbule Keep!")'), 'serbulekeep');
    assert.strictEqual(run('normalizeMapKey("")'), '');
    assert.ok(run('isSameMapName("Serbule", "serbule")'));
    assert.ok(run('isSameMapName("Serbule", "Serbule Hills")')); // substring match
    assert.ok(run('isSameMapName("Serbule Hills", "Serbule")'));
    assert.ok(!run('isSameMapName("Eltibule", "Serbule")'));
    assert.ok(!run('isSameMapName("", "Serbule")'));
  });

  // Let the load-time async chains (loadTraderCapacity, checkRefreshNotifications) settle so
  // nothing is left pending when the process exits.
  await new Promise(r => setImmediate(r));
  await new Promise(r => setImmediate(r));

  console.log(`\n${passed} checks passed`);
}

main().catch(e => { console.error(e); process.exit(1); });
