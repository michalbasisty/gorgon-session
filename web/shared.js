const $ = s => document.querySelector(s);
const $$ = s => Array.from(document.querySelectorAll(s));

function toast(msg, type) {
  const el = document.createElement('div');
  el.className = 'toast toast-' + (type || 'info');
  el.textContent = msg;
  document.body.appendChild(el);
  setTimeout(() => el.remove(), 3500);
}

const state = { 
  session: null, 
  currentView: 'dashboard', 
  summarySortMode: 'npc', 
  npcs: [], 
  disabledNPCs: new Set(), 
  shopNPCs: [],
  craftingRecipes: [],
  favorProgress: new Set(),
  playerPrices: {},
  traders: [],
  traderCapacity: {}, // npc_name → { limit, sold, remaining }
  hiddenAreas: new Set(),
  hiddenTraders: new Set(),
  showHiddenOnly: false,
  prioritizedNPCs: new Set(),
  notificationThreshold: 500,
  priceHistory: {}, // item name → { average, last, count }
  zoneNpcs: [],
  dropRates: null,
  dropZone: undefined, // active zone filter ('' = all zones); undefined = not chosen yet
  dropSource: '', // active enemy drill-down ('' = none)
  sessionTemplates: [] // from GET /api/config → session_templates
};

// Shared request state (must be initialized before any startup API calls)
var __inflightGet = new Map();
const tbody = $('#loot tbody');
const stateEl = $('#state');
const countEl = $('#count');
const emptyEl = $('#empty');
const elapsedEl = $('#elapsed');

let __trackerLootSig = '';
let __trackerEventsSig = '';
let __trackerNotesSig = '';
let __pricePollTick = 0;
let __tradersZoneSig = '';

// Load settings from localStorage
function loadSettings() {
  try {
    const disabled = JSON.parse(localStorage.getItem('disabledNPCs') || '[]');
    state.disabledNPCs = new Set(disabled);
    const shopNPCs = JSON.parse(localStorage.getItem('shopNPCs') || '[]');
    state.shopNPCs = Array.isArray(shopNPCs) ? shopNPCs : [];
    const craftingRecipes = JSON.parse(localStorage.getItem('craftingRecipes') || '[]');
    state.craftingRecipes = Array.isArray(craftingRecipes) ? craftingRecipes : [];
    const favorProgress = JSON.parse(localStorage.getItem('favorProgress') || '[]');
    state.favorProgress = new Set(favorProgress);
    const playerPrices = JSON.parse(localStorage.getItem('playerPrices') || '{}');
    state.playerPrices = playerPrices || {};
    const hiddenAreas = JSON.parse(localStorage.getItem('hiddenAreas') || '[]');
    state.hiddenAreas = new Set(hiddenAreas);
    const hiddenTraders = JSON.parse(localStorage.getItem('hiddenTraders') || '[]');
    state.hiddenTraders = new Set(hiddenTraders);
    const prioritizedNPCs = JSON.parse(localStorage.getItem('prioritizedNPCs') || '[]');
    state.prioritizedNPCs = new Set(prioritizedNPCs);
  } catch (e) {
    state.disabledNPCs = new Set();
    state.shopNPCs = [];
    state.craftingRecipes = [];
    state.favorProgress = new Set();
    state.playerPrices = {};
    state.hiddenAreas = new Set();
    state.hiddenTraders = new Set();
    state.prioritizedNPCs = new Set();
  }
}

function saveSettings() {
  localStorage.setItem('disabledNPCs', JSON.stringify([...state.disabledNPCs]));
  localStorage.setItem('shopNPCs', JSON.stringify(state.shopNPCs));
  localStorage.setItem('craftingRecipes', JSON.stringify(state.craftingRecipes));
  localStorage.setItem('favorProgress', JSON.stringify([...state.favorProgress]));
  localStorage.setItem('playerPrices', JSON.stringify(state.playerPrices));
  localStorage.setItem('hiddenAreas', JSON.stringify([...state.hiddenAreas]));
  localStorage.setItem('hiddenTraders', JSON.stringify([...state.hiddenTraders]));
  localStorage.setItem('prioritizedNPCs', JSON.stringify([...state.prioritizedNPCs]));
}

loadSettings();

async function loadTraderCapacity() {
  try {
    const areasResp = await api('/api/traders');
    const areas = Array.isArray(areasResp) ? areasResp : [];
    const cap = {};
    for (const area of areas) {
      const npcs = Array.isArray(area?.npcs) ? area.npcs : [];
      for (const npc of npcs) {
        const dur = npc.time_until_reset || '';
        const resetMinutes = parseResetDurationMinutes(dur);
        cap[npc.npc_name] = {
          limit: npc.weekly_limit || 0,
          sold: npc.sold_this_week || 0,
          remaining: Math.max(0, (npc.weekly_limit || 0) - (npc.sold_this_week || 0)),
          reset: dur,
          resetMinutes,
          daysUntilReset: Math.floor(resetMinutes / (24 * 60)),
          area: area.area || ''
        };
      }
    }
    state.traderCapacity = cap;
    if (state.currentView === 'dashboard') renderDashboard();
  } catch (e) {
    console.error('loadTraderCapacity failed', e);
  }
}
loadTraderCapacity();

function escapeHtml(s) {
  return String(s == null ? '' : s).replace(/[&<>"]/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}

function relTime(t) {
  const d = new Date(t);
  if (isNaN(d)) return '';
  return d.toLocaleTimeString([], { hour12: false });
}
function fmtElapsed(ms) {
  const s = Math.floor((ms || 0) / 1000);
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
  if (h) return `${h}h${String(m).padStart(2, '0')}m`;
  if (m) return `${m}m${String(sec).padStart(2, '0')}s`;
  return `${sec}s`;
}

function parseResetDurationMinutes(text) {
  const raw = String(text || '').trim().toLowerCase();
  if (!raw) return Number.POSITIVE_INFINITY;
  if (raw === 'now' || raw.includes('less than a minute')) return 0;

  const d = raw.match(/(\d+)\s*d/);
  const h = raw.match(/(\d+)\s*h/);
  const m = raw.match(/(\d+)\s*m/);

  const days = d ? Number(d[1]) : 0;
  const hours = h ? Number(h[1]) : 0;
  const mins = m ? Number(m[1]) : 0;

  const total = days * 24 * 60 + hours * 60 + mins;
  return Number.isFinite(total) && total >= 0 ? total : Number.POSITIVE_INFINITY;
}

function normalizeTargetKind(v) {
  const s = String(v || '').trim().toLowerCase();
  if (s === 'armor' || s === 'armour') return 'armor';
  return 'health';
}

function parseDirectPartsSpec(spec) {
  const out = [];
  const raw = String(spec || '').trim();
  if (!raw) return out;
  const chunks = raw.split(',').map(s => s.trim()).filter(Boolean);
  for (const chunk of chunks) {
    // Supported formats:
    // - Element:Damage
    // - Element:Damage@armor
    // - Element:Damage armor
    let m = chunk.match(/^([^:]+):\s*([0-9]+(?:\.[0-9]+)?)(?:\s*@\s*(health|armor|armour)|\s+(health|armor|armour))?$/i);
    if (!m) continue;
    const element = String(m[1] || '').trim();
    const damage = Number(m[2]);
    const target = normalizeTargetKind(m[3] || m[4] || 'health');
    if (!element || !Number.isFinite(damage) || damage < 0) continue;
    out.push({ element, damage, target });
  }
  return out;
}

function formatDirectPartsSpec(parts) {
  const arr = Array.isArray(parts) ? parts : [];
  return arr
    .filter(p => p && String(p.element || '').trim())
    .map(p => {
      const target = normalizeTargetKind(p.target || 'health');
      return `${String(p.element).trim()}:${Number(p.damage || 0).toFixed(0)}${target === 'armor' ? '@armor' : ''}`;
    })
    .join(', ');
}

function parseDotPartsSpec(spec) {
  const out = [];
  const raw = String(spec || '').trim();
  if (!raw) return out;
  const chunks = raw.split(',').map(s => s.trim()).filter(Boolean);
  for (const chunk of chunks) {
    // Supported formats (damage is TOTAL over duration, not per second):
    // - Element:Damage/Seconds
    // - Element:Damage over Seconds
    // Optional target suffix: @armor or @health
    // Examples: Poison:381/10, Fire:80 over 10s@armor
    let m = chunk.match(/^([^:]+):\s*([0-9]+(?:\.[0-9]+)?)\s*\/\s*([0-9]+(?:\.[0-9]+)?)\s*s?(?:\s*@\s*(health|armor|armour)|\s+(health|armor|armour))?$/i);
    if (!m) {
      m = chunk.match(/^([^:]+):\s*([0-9]+(?:\.[0-9]+)?)\s*over\s*([0-9]+(?:\.[0-9]+)?)\s*s?(?:\s*@\s*(health|armor|armour)|\s+(health|armor|armour))?$/i);
    }
    if (!m) continue;
    const element = String(m[1] || '').trim();
    const damage = Number(m[2]);
    const seconds = Number(m[3]);
    const target = normalizeTargetKind(m[4] || m[5] || 'health');
    if (!element || !Number.isFinite(damage) || damage < 0 || !Number.isFinite(seconds) || seconds <= 0) continue;
    out.push({ element, damage, seconds, target });
  }
  return out;
}

function formatDotPartsSpec(parts) {
  const arr = Array.isArray(parts) ? parts : [];
  return arr
    .filter(p => p && String(p.element || '').trim())
    .map(p => {
      const target = normalizeTargetKind(p.target || 'health');
      return `${String(p.element).trim()}:${Number(p.damage || 0).toFixed(0)}/${Number(p.seconds || 0).toFixed(1)}${target === 'armor' ? '@armor' : ''}`;
    })
    .join(', ');
}

// API helpers
async function api(path, method = 'GET', body = null) {
  const m = String(method || 'GET').toUpperCase();
  const isGet = m === 'GET' && !body;
  if (isGet && __inflightGet.has(path)) {
    return __inflightGet.get(path);
  }

  const p = (async () => {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 10000);
    try {
      const r = await fetch(path, {
        method: m,
        headers: body ? { 'Content-Type': 'application/json' } : undefined,
        body: body ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });
      if (!r.ok) {
        toast(`${path}: ${await r.text()}`, 'error');
        return null;
      }
      // ponytail: tolerate empty 2xx bodies (e.g. stale server, proxy, 204s)
      // instead of crashing on r.json(). Callers get null, same as an error.
      const text = await r.text();
      return text ? JSON.parse(text) : null;
    } catch (e) {
      if (e && e.name === 'AbortError') {
        console.warn('API timeout', path);
        return null;
      }
      throw e;
    } finally {
      clearTimeout(timeout);
    }
  })();

  if (isGet) {
    __inflightGet.set(path, p);
    p.finally(() => __inflightGet.delete(path));
  }
  return p;
}

// Catch unhandled promise rejections so they don't silently break the page
window.addEventListener('unhandledrejection', e => {
  toast('Error: ' + (e.reason?.message || e.reason || 'unknown'), 'error');
});

// Navigation (click + keyboard)
$$('.nav-item').forEach(item => {
  item.addEventListener('click', () => {
    const view = item.dataset.view;
    switchView(view);
  });
  item.addEventListener('keydown', e => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      switchView(item.dataset.view);
    }
  });
});

function updateNavAria(view) {
  $$('.nav-item').forEach(n => {
    const isActive = n.dataset.view === view;
    n.classList.toggle('active', isActive);
    n.setAttribute('aria-selected', String(isActive));
    n.setAttribute('tabindex', isActive ? '0' : '-1');
  });
}

function switchView(view) {
  state.currentView = view;
  updateNavAria(view);
  $('#view-dashboard').classList.toggle('hidden', view !== 'dashboard');
  $('#view-tracker').classList.toggle('hidden', view !== 'tracker');
  $('#view-summary').classList.toggle('hidden', view !== 'summary');
  $('#view-history').classList.toggle('hidden', view !== 'history');
  $('#view-history-detail').classList.toggle('hidden', view !== 'history-detail');
  $('#view-favor').classList.toggle('hidden', view !== 'favor');
  $('#view-traders').classList.toggle('hidden', view !== 'traders');
  $('#view-items').classList.toggle('hidden', view !== 'items');
  $('#view-warcache').classList.toggle('hidden', view !== 'warcache');
  $('#view-settings').classList.toggle('hidden', view !== 'settings');
  $('#view-skills').classList.toggle('hidden', view !== 'skills');
  $('#view-recipes').classList.toggle('hidden', view !== 'recipes');
  $('#view-drop-rates').classList.toggle('hidden', view !== 'drop-rates');

  const titles = { 
    dashboard: 'Dashboard',
    tracker: 'Tracker', 
    summary: 'Summary', 
    history: 'History',
    'history-detail': 'Session Details',
    favor: 'Favor Progress',
    traders: 'Shops & Traders',
    items: 'Item Catalog',
    warcache: 'Warcache Solver',
    skills: 'Skills Reference',
    recipes: 'Recipe Browser',
    'drop-rates': 'Drop Rates',
    settings: 'Settings'
  };
  $('#view-title').innerHTML = `${titles[view]} <small id="state">${state.session?.state || 'idle'}</small>`;

  if (view === 'dashboard') renderDashboard();
  if (view === 'tracker') loadRoutePlan();
  if (view === 'summary' && state.session) renderSummary(state.session);
  if (view === 'history') renderHistory();
  if (view === 'favor') renderFavorView();
  if (view === 'traders') { loadTraderCapacity().then(() => renderTradersView()); }
  if (view === 'items') renderItemsView();
  if (view === 'warcache') renderWarcacheView();
  if (view === 'skills') renderSkillsView();
  if (view === 'recipes') renderRecipesView();
  if (view === 'drop-rates') { state.dropZone = undefined; state.dropSource = ''; renderDropRatesView(); }
  if (view === 'settings') renderSettingsView();
}

function renderSession(s) {
  state.session = s;
  if (!s) return;
  const stateBadge = $('#state');
  if (stateBadge) {
    stateBadge.textContent = s.state;
    stateBadge.className = s.state;
  }
  // Sidebar status badge
  const sb = $('#status-badge');
  if (sb) { sb.textContent = s.state; sb.className = 'session-badge ' + s.state; }
  $('#start').disabled = s.state === 'running';
  $('#stop').disabled = s.state !== 'running';
  if (s.dungeon && $('#dungeon').value === '' && s.state !== 'idle') $('#dungeon').value = s.dungeon;

  if (s.state === 'running') {
    elapsedEl.textContent = ' · ' + fmtElapsed(Date.now() - new Date(s.started_at).getTime());
  } else if (s.state === 'stopped') {
    elapsedEl.textContent = ' · ended';
  } else {
    elapsedEl.textContent = '';
  }

  // Tracker view: avoid full DOM rewrites every poll to prevent flicker/jumps.
  if (state.currentView === 'tracker') {
    const loot = Array.isArray(s.loot) ? s.loot : [];
    const lastLoot = loot.length ? loot[loot.length - 1] : null;
    const lootSig = [
      s.state,
      loot.length,
      lastLoot?.name || '',
      lastLoot?.count || 0,
      lastLoot?.last_seen || '',
      ($('#loot-search')?.value || '').toLowerCase(),
      $('#loot-filter')?.value || '',
    ].join('|');
    if (lootSig !== __trackerLootSig && !document.querySelector('.loot-note-input')) {
      __trackerLootSig = lootSig;
      renderLootTable(s);
      renderSellChecklist();
      // Reuse the tracker refresh cycle: keep the current-session plan in sync.
      if (($('#route-session-select')?.value || '') === '') renderRoutePlan(s);
    }

    const notesSig = `${s.state}|${s.notes || ''}`;
    const editingNotes = !!$('#tracker-notes-input');
    if (notesSig !== __trackerNotesSig && !editingNotes) {
      __trackerNotesSig = notesSig;
      renderTrackerNotes(s);
    }

    const eventsSig = [
      s.zone || '',
      (s.zone_history || []).length,
      (s.xp_gains || []).length,
      (s.deaths || []).length,
      (s.kills || []).length,
      s.total_gold || 0,
      (s.gathering || []).length,
      (s.level_ups || []).length,
    ].join('|');
    if (eventsSig !== __trackerEventsSig) {
      __trackerEventsSig = eventsSig;
      renderSessionEvents(s);
    }

    const startNotes = $('#tracker-start-notes');
    if (startNotes) startNotes.style.display = s.state === 'idle' ? '' : 'none';
  }

  if (state.currentView === 'summary') {
    renderSummary(s);
  }
  if (state.currentView === 'dashboard') {
    renderDashboard();
  }
  if (state.currentView === 'traders') {
    const zoneSig = String(s.zone || '').trim().toLowerCase();
    if (zoneSig !== __tradersZoneSig) {
      __tradersZoneSig = zoneSig;
      renderTradersView();
    }
  }
}

function renderTrackerNotes(s) {
  const el = $('#tracker-notes');
  if (!el) return;
  if (s.state !== 'running') { el.innerHTML = ''; return; }
  const text = s.notes || '';
  el.innerHTML = text
    ? `<span class="notes-text">📝 ${escapeHtml(text)}</span><button class="notes-edit-btn" onclick="editTrackerNotes()">✏️</button>`
    : `<span class="notes-empty">No session notes</span><button class="notes-edit-btn" onclick="editTrackerNotes()">✏️</button>`;
}

window.editTrackerNotes = function() {
  const el = $('#tracker-notes');
  if (!el) return;
  const current = state.session?.notes || '';
  el.innerHTML = `<input class="notes-edit-input" id="tracker-notes-input" value="${escapeHtml(current)}" placeholder="session notes...">
    <button class="notes-save-btn" onclick="saveTrackerNotes()">Save</button>
    <button class="notes-cancel-btn" onclick="renderTrackerNotes(state.session)">Cancel</button>`;
  $('#tracker-notes-input').focus();
};

function skillCategory(name) {
  if (!state.skills) return '';
  const key = Object.keys(state.skills).find(k => k.toLowerCase() === name.toLowerCase());
  if (!key) return '';
  const s = state.skills[key];
  return s.Combat ? 'Combat' : 'Non-Combat';
}

function zonePath(zoneName) {
  if (!state.areas || !zoneName) return zoneName;
  // Try to find the friendly name via reverse lookup
  const key = zoneName.toLowerCase();
  for (const internalKey of Object.keys(state.areas)) {
    const friendly = state.areas[internalKey];
    if (friendly.toLowerCase() === key) return friendly;
  }
  // Check short friendly names if available via fallback
  // If no match, return the raw name
  return zoneName;
}

function renderSessionEvents(s) {
  const el = $('#tracker-events');
  if (!el) return;

  let html = '';

  // Zone timeline — always shown (persists after session stops)
  const zHist = s.zone_history || [];
  if (zHist.length > 1) {
    const recent = zHist.slice(-6);
    html += `<div class="tracker-zone-timeline">📍 ${recent.map((z, i) => {
      const name = zonePath(z.zone);
      if (i === recent.length - 1) return `<strong>${escapeHtml(name)}</strong>`;
      return `<span title="${relTime(z.time)}">${escapeHtml(name)}</span>`;
    }).join(' <span class="tz-arrow">→</span> ')} <span class="tz-count">(${zHist.length} zones)</span></div>`;
  } else if (zHist.length === 1) {
    html += `<div class="tracker-zone-timeline">📍 <strong>${escapeHtml(zonePath(zHist[0].zone))}</strong> <span class="tz-count">(${relTime(zHist[0].time)})</span></div>`;
  }

  // Events bar — live session data
  if (s.state === 'running') {
    const parts = [];
    if (s.zone) parts.push(`📍 ${escapeHtml(zonePath(s.zone))}`);
    const xp = s.xp_gains || [];
    if (xp.length) {
      const bySkill = {};
      for (const g of xp) { bySkill[g.skill] = (bySkill[g.skill] || 0) + g.amount; }
      const skills = Object.entries(bySkill).sort((a, b) => b[1] - a[1]).slice(0, 5);
      parts.push(`XP: ${skills.map(([sk, am]) => {
        const cat = skillCategory(sk);
        return cat ? `${sk}(${cat}) +${am}` : `${sk}+${am}`;
      }).join(', ')}${skills.length < Object.keys(bySkill).length ? '…' : ''}`);
    }
    const deaths = (s.deaths || []).length;
    if (deaths) parts.push(`💀 ${deaths} death${deaths > 1 ? 's' : ''}`);
    const kills = (s.kills || []).length;
    if (kills) parts.push(`⚔ ${kills} kill${kills > 1 ? 's' : ''}`);
    const gold = s.total_gold || 0;
    if (gold) parts.push(`💰 ${gold}g`);
    const gather = (s.gathering || []).length;
    if (gather) parts.push(`🪓 ${gather} gather${gather > 1 ? 's' : ''}`);
    const levels = (s.level_ups || []).length;
    if (levels) parts.push(`⬆ ${levels} level${levels > 1 ? 's' : ''}`);

    if (parts.length) {
      html = `<div class="tracker-events-bar">${parts.join(' &middot; ')}</div>` + html;
    }
  }

  el.innerHTML = html;
}

window.saveTrackerNotes = async function() {
  const notes = $('#tracker-notes-input')?.value || '';
  const s = await api('/api/session', 'PATCH', { notes });
  if (s) {
    state.session = s;
    toast('Notes saved', 'success');
    renderTrackerNotes(s);
  }
};

function renderLootTable(s) {
  tbody.innerHTML = '';
  const allLoot = s.loot || [];
  countEl.textContent = allLoot.length;

  // Apply filters
  const search = ($('#loot-search')?.value || '').toLowerCase();
  const filter = $('#loot-filter')?.value || '';
  
  const filtered = allLoot.filter(e => {
    if (search && !e.name.toLowerCase().includes(search)) return false;
    if (filter && e.decision.verdict !== filter) return false;
    return true;
  });
  
  emptyEl.style.display = filtered.length ? 'none' : 'block';
  for (const e of filtered) addRow(e);
}

function addRow(e) {
  const tr = document.createElement('tr');
  tr.dataset.name = e.name;
  const isMat = isTrackedMaterial(e.name, getTrackedIngredients());
  if (isMat) tr.classList.add('loot-material');
  const iconHtml = e.icon_url ? `<img src="${e.icon_url}" alt="" class="item-icon" onerror="this.style.display='none'">` : '';
  tr.innerHTML = `
    <td class="time">${relTime(e.last_seen)}</td>
    <td>${iconHtml}${escapeHtml(e.name)}${isMat ? ' <span class="material-badge">mat</span>' : ''}</td>
    <td class="count">${e.count}</td>
    <td><span class="verdict ${e.decision.verdict}">${e.decision.verdict.replace(/_/g, ' ')}</span></td>
    <td class="route"><a href="#" class="route-link" title="Plan a sell route for this item" onclick="event.preventDefault();planRoute('${escapeHtml(e.name).replace(/'/g, "\\'")}')">${routeText(e.decision)}</a></td>
    <td><button class="loot-del-btn" onclick="deleteLootItem('${escapeHtml(e.name).replace(/'/g, "\\'")}')">×</button></td>
    <td class="loot-note">
      <span class="loot-note-text" ondblclick="editLootNote(this,'${escapeHtml(e.name).replace(/'/g, "\\'")}')">${escapeHtml(e.note || '') || '<span style="opacity:0.3;cursor:help" title="double-click to add note">—</span>'}</span>
    </td>`;
  tbody.insertBefore(tr, tbody.firstChild);
}

window.editLootNote = function(span, name) {
  const current = span.textContent === '—' ? '' : span.textContent;
  const input = document.createElement('input');
  input.type = 'text';
  input.className = 'loot-note-input';
  input.value = current;
  input.placeholder = 'note...';
  input.style.cssText = 'width:100%;font-size:13px;padding:4px 8px;background:var(--bg);color:var(--text);border:1px solid var(--accent);border-radius:3px;outline:none';
  span.replaceWith(input);
  input.focus();
  input.onblur = async function() {
    const note = input.value.trim();
    await api('/api/loot-note', 'PATCH', { name, note });
    refreshAll();
  };
  input.onkeydown = function(ev) {
    if (ev.key === 'Enter') input.blur();
    if (ev.key === 'Escape') { input.value = current; input.blur(); }
  };
};

window.deleteLootItem = async function(name) {
  if (!confirm(`Remove "${name}" from this session?`)) return;
  const res = await api(`/api/loot?name=${encodeURIComponent(name)}`, 'DELETE');
  if (res) refreshAll();
};

// Jump from a tracked item's route cell to the Route Plan panel, focused on that item.
window.planRoute = function(name) {
  __routePlanHighlight = name;
  const sel = $('#route-session-select');
  if (sel) sel.value = '';
  switchView('tracker'); // tracker entry triggers loadRoutePlan → renderRoutePlan
  const panel = $('#route-plan-panel');
  if (panel && !panel.open) panel.open = true;
  const results = $('#route-plan-results');
  if (results) results.scrollIntoView({ behavior: 'smooth', block: 'start' });
};

// ── Route Plan panel (Tracker view): per-session gift/sell/keep plan ──
let __routeSessionsLoaded = false;
let __routePlanHighlight = null;
let __routePlannerCache = {}; // item → { routes }; planner data changes rarely, cache per page load

function routeSessionLabel(id) {
  // "session-20260731-124047" → "07-31 12:40"
  const m = String(id || '').match(/(\d{4})(\d{2})(\d{2})-(\d{2})(\d{2})/);
  return m ? `${m[2]}-${m[3]} ${m[4]}:${m[5]}` : String(id || '');
}

async function loadRoutePlan() {
  const sel = $('#route-session-select');
  if (!sel) return;
  if (!__routeSessionsLoaded) {
    const sessions = await api('/api/sessions');
    if (Array.isArray(sessions)) {
      const cur = sel.value;
      sel.innerHTML = '<option value="">Current session</option>' + sessions.map(s => {
        const id = s && s.id;
        if (!id) return '';
        return `<option value="${escapeHtml(id)}">${escapeHtml(routeSessionLabel(id))}</option>`;
      }).join('');
      if (cur) sel.value = cur;
      __routeSessionsLoaded = true;
    }
  }
  renderRoutePlan();
}

let __routePlanRenderToken = 0;     // guards against stale async renders racing newer ones

async function renderRoutePlan(preloaded) {
  const results = $('#route-plan-results');
  const sel = $('#route-session-select');
  if (!results || !sel) return;
  const token = ++__routePlanRenderToken;
  const sid = sel.value;
  const s = (preloaded && !sid) ? preloaded
    : (sid ? await api('/api/session/' + encodeURIComponent(sid)) : await api('/api/session'));
  if (!s) return;

  const loot = Array.isArray(s.loot) ? s.loot : [];
  const byName = new Map();
  for (const e of loot) {
    const name = String(e.name || '').trim() || '(unnamed)';
    const row = byName.get(name) || { name, count: 0, decision: e.decision || {} };
    row.count += Number(e.count) || 1;
    row.value = Math.max(row.value || 0, Number(e.value) || 0);
    byName.set(name, row);
  }
  const items = [...byName.values()];

  if (!items.length) {
    if (token === __routePlanRenderToken) results.innerHTML = '<div class="route-plan-empty">no items in this session</div>';
    return;
  }

  const favor = items.filter(i => i.decision.verdict === 'favor');
  const sell = items.filter(i => i.decision.verdict === 'sell_vendor' || i.decision.verdict === 'sell_consignment');
  const keep = items.filter(i => i.decision.verdict !== 'favor' && i.decision.verdict !== 'sell_vendor' && i.decision.verdict !== 'sell_consignment');
  const keepHtml = keep.length ? '<h4 class="route-plan-block-title keep">📦 Keep</h4>' + keep.map(planKeepRow).join('') : '';

  // One list at a time: tabs switch between the sell and favor blocks; keep is
  // informational (no routes) and stays visible under whichever mode is active.
  const mode = __routePlanMode === 'favor' ? 'favor' : 'sell';
  let listHtml;
  if (mode === 'favor') {
    listHtml = '<h4 class="route-plan-block-title favor">🎁 Give as favor</h4>' +
      (favor.length ? buildFavorRouteHtml(favor) : '<div class="route-plan-empty">no favor items</div>');
  } else {
    const withRoutes = await Promise.all(sell.slice(0, 10).map(async it => ({ it, routes: await fetchRoutesFor(it) })));
    listHtml = '<h4 class="route-plan-block-title sell">💰 Sell route</h4>' +
      (sell.length ? buildSellRouteHtml(withRoutes) : '<div class="route-plan-empty">no sell items</div>');
  }

  results.innerHTML = `<div class="route-plan-tabs">
      <button class="route-plan-tab${mode === 'sell' ? ' active' : ''}" data-mode="sell">💰 Sell</button>
      <button class="route-plan-tab${mode === 'favor' ? ' active' : ''}" data-mode="favor">🎁 Favor</button>
    </div>
    <div class="route-plan-list">${listHtml}${keepHtml}</div>`;
  applyRoutePlanHighlight(results);

  results.querySelectorAll('.route-plan-tab').forEach(btn => {
    btn.addEventListener('click', () => {
      __routePlanMode = btn.dataset.mode;
      renderRoutePlan(); // full re-render so only the active mode's list is built
    });
  });

  // Per-row ⇄ pickers for the ACTIVE mode only (sell needs fetch, favor doesn't).
  if (mode === 'favor') {
    await Promise.all(favor.map(it => planFavorRoutesFor(it)));
  } else {
    await Promise.all(sell.slice(0, 10).map(it => planRoutesFor(it)));
  }
}

function applyRoutePlanHighlight(results) {
  if (!__routePlanHighlight) return;
  const row = [...results.querySelectorAll('.route-plan-item')].find(r => r.dataset.name === __routePlanHighlight);
  if (row) {
    row.classList.add('highlight');
    row.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  }
  __routePlanHighlight = null;
}

// Routes for one item via the existing ?item= endpoint (cached).
async function fetchRoutesFor(it) {
  if (!(it.name in __routePlannerCache)) {
    __routePlannerCache[it.name] = await api('/api/route-planner?item=' + encodeURIComponent(it.name) + '&sort=distance') || null;
  }
  return Array.isArray(__routePlannerCache[it.name]?.routes) ? __routePlannerCache[it.name].routes : [];
}

function planFavorRow(it) {
  return `<div class="route-plan-item" data-name="${escapeHtml(it.name)}">
    <span class="route-plan-item-name">${escapeHtml(it.name)} <span class="route-plan-count">x${it.count}</span></span>
    <span class="route-plan-item-detail"><span class="route-plan-favor-routes"></span></span>
  </div>`;
}

function planSellRow(it) {
  const reason = it.decision.sell_reason
    ? `<span class="route-plan-reason">${escapeHtml(it.decision.sell_reason)}</span>`
    : '<span class="route-plan-muted">sell to vendor</span>';
  return `<div class="route-plan-item" data-name="${escapeHtml(it.name)}">
    <span class="route-plan-item-name">${escapeHtml(it.name)} <span class="route-plan-count">x${it.count}</span></span>
    <span class="route-plan-item-detail">${reason}<span class="route-plan-routes"></span></span>
  </div>`;
}

function planKeepRow(it) {
  return `<div class="route-plan-item" data-name="${escapeHtml(it.name)}">
    <span class="route-plan-item-name route-plan-muted">${escapeHtml(it.name)} <span class="route-plan-count">x${it.count}</span></span>
    <span class="route-plan-item-detail route-plan-muted">keep</span>
  </div>`;
}

// Per-item chosen trader: item name → trader name (user switches via the ⇄ button).
let __routeTraderPick = {};
// Per-item chosen favor target: item name → NPC name (user switches via the ⇄ button).
let __routeFavorPick = {};
// Active Route Plan mode: 'sell' or 'favor' (toggled via the tabs).
let __routePlanMode = 'sell';

// Shared link/meta helpers for trader names and route stats.
function traderLinkHtml(name) {
  const n = String(name || '').trim();
  return n
    ? `<a href="#" class="route-plan-link" onclick="event.preventDefault();showTraderHistory('${escapeHtml(n).replace(/'/g, "\\'")}')">${escapeHtml(n)}</a>`
    : '<span class="route-plan-muted">?</span>';
}
const distHtml = r => r.distance_km == null ? '' : ` · ${Number(r.distance_km).toFixed(1)} km`;
const refreshHtml = r => r.refresh_in_hours ? ` · ${Number(r.refresh_in_hours).toFixed(1)}h` : '';

// Sell items grouped by their effective trader (user pick or nearest); trader groups nested
// under map sections (one per area, ordered by nearest map first, unknown-area last).
function buildSellRouteHtml(entries) {
  const groups = new Map();
  const noRoute = [];
  for (const { it, routes } of entries) {
    if (!routes.length) { noRoute.push(it); continue; }
    const pick = __routeTraderPick[it.name] || routes[0].trader;
    const r = routes.find(x => x.trader === pick) || routes[0];
    let g = groups.get(r.trader);
    if (!g) {
      g = { trader: r.trader, area: r.area || '', distance: r.distance_km ?? null, refresh: r.refresh_in_hours ?? null, capacity: r.remaining_capacity_g ?? 0, items: [] };
      groups.set(r.trader, g);
    }
    g.items.push(it);
  }
  const ordered = [...groups.values()].sort((a, b) => {
    const ad = a.distance != null, bd = b.distance != null;
    if (ad !== bd) return ad ? -1 : 1;
    if (ad) return a.distance - b.distance;
    return String(a.trader).localeCompare(String(b.trader));
  });
  // Bucket trader groups by map; each map renders one section holding its trader groups.
  const mapName = g => String(g.area || '').trim() || 'unknown area';
  const maps = new Map();
  for (const g of ordered) {
    const name = mapName(g);
    if (!maps.has(name)) maps.set(name, []);
    maps.get(name).push(g);
  }
  const groupHtml = g => {
    const dist = g.distance == null ? '' : ` · ${Number(g.distance).toFixed(1)} km`;
    const refresh = g.refresh ? ` · ${Number(g.refresh).toFixed(1)}h` : '';
    const cap = g.capacity > 0 ? ` · ${Math.round(g.capacity).toLocaleString()} g` : '';
    return `<div class="route-plan-group">
    <div class="route-plan-group-header">
      <span class="route-plan-trader">${traderLinkHtml(g.trader)}${dist}${refresh}${cap}</span>
    </div>
    <div class="route-plan-group-items">${g.items.map(planSellRow).join('')}</div>
  </div>`;
  };
  const mapHtml = ([name, gs]) => `<div class="route-plan-map">
    <div class="route-plan-map-header">📍 ${escapeHtml(name)}</div>
    ${gs.map(groupHtml).join('')}
  </div>`;
  const mapDistance = gs => {
    const ds = gs.filter(g => g.distance != null).map(g => g.distance);
    return ds.length ? Math.min(...ds) : null;
  };
  const orderedMaps = [...maps.entries()].sort((a, b) => {
    const ad = mapDistance(a[1]), bd = mapDistance(b[1]);
    if ((ad == null) !== (bd == null)) return ad == null ? 1 : -1;
    if (ad != null) return ad - bd;
    return String(a[0]).localeCompare(String(b[0]));
  });
  const noRouteHtml = noRoute.length
    ? `<div class="route-plan-group">
    <div class="route-plan-group-header">
      <span class="route-plan-muted">no traders found</span>
    </div>
    <div class="route-plan-group-items">${noRoute.map(planSellRow).join('')}</div>
  </div>`
    : '';
  return orderedMaps.map(mapHtml).join('') + noRouteHtml;
}

async function planRoutesFor(it) {
  const routes = await fetchRoutesFor(it);
  if (!routes.length) return;
  const row = [...document.querySelectorAll('.route-plan-item')]
    .find(r => r.dataset.name === it.name);
  const container = row?.querySelector('.route-plan-routes');
  if (!container) return;

  // Trader + distance live in the group header; this only renders the ⇄ picker.
  if (routes.length <= 1) { container.innerHTML = ''; return; }

  const pick = __routeTraderPick[it.name] || routes[0].trader;
  const chosen = routes.find(r => r.trader === pick) || routes[0];

  container.innerHTML = `<button class="route-plan-switch" title="choose another trader">⇄</button>
    <span class="route-plan-picker" hidden>${routes.map(r => `
      <button class="route-plan-option" data-trader="${escapeHtml(r.trader)}">${escapeHtml(r.trader)} <span class="route-plan-area">${escapeHtml(r.area || '')}</span>${distHtml(r)}${refreshHtml(r)}${r.trader === chosen.trader ? ' ✓' : ''}</button>`).join('')}</span>`;

  container.querySelector('.route-plan-switch')?.addEventListener('click', () => {
    const picker = container.querySelector('.route-plan-picker');
    if (picker) picker.hidden = !picker.hidden;
  });
  container.querySelectorAll('.route-plan-option').forEach(btn => {
    btn.addEventListener('click', () => {
      __routeTraderPick[it.name] = btn.dataset.trader;
      renderRoutePlan(); // re-render so the item moves to the correct trader group
    });
  });
}
// Favor items grouped by their effective target NPC (user pick or best score); target groups
// nested under map sections (one per area, ordered by nearest map first, score as tiebreak,
// unknown-area maps last).
function buildFavorRouteHtml(items) {
  const groups = new Map();
  const noTargets = [];
  for (const it of items) {
    const targets = [...(it.decision.favor_targets || [])].sort((a, b) => (b.score || 0) - (a.score || 0));
    if (!targets.length) { noTargets.push(it); continue; }
    const pick = __routeFavorPick[it.name] || targets[0].npc;
    const t = targets.find(x => x.npc === pick) || targets[0];
    let g = groups.get(t.npc);
    if (!g) {
      g = { npc: t.npc, area: t.area || '', score: t.score ?? null, distance: t.distance_km ?? null, items: [] };
      groups.set(t.npc, g);
    }
    g.items.push(it);
  }
  const ordered = [...groups.values()].sort((a, b) => {
    const as = a.score != null, bs = b.score != null;
    if (as !== bs) return as ? -1 : 1;
    if (as) return b.score - a.score;
    return String(a.npc).localeCompare(String(b.npc));
  });
  // Bucket target groups by map; each map renders one section holding its target groups.
  const mapName = g => String(g.area || '').trim() || 'unknown area';
  const maps = new Map();
  for (const g of ordered) {
    const name = mapName(g);
    if (!maps.has(name)) maps.set(name, []);
    maps.get(name).push(g);
  }
  const groupHtml = g => {
    const dist = g.distance == null ? '' : ` · ${Number(g.distance).toFixed(1)} km`;
    const score = g.score == null ? '' : ` · +${g.score}`;
    return `<div class="route-plan-group">
    <div class="route-plan-group-header">
      <span class="route-plan-trader">${traderLinkHtml(g.npc)}${dist}${score}</span>
    </div>
    <div class="route-plan-group-items">${g.items.map(planFavorRow).join('')}</div>
  </div>`;
  };
  const mapHtml = ([name, gs]) => `<div class="route-plan-map">
    <div class="route-plan-map-header">📍 ${escapeHtml(name)}</div>
    ${gs.map(groupHtml).join('')}
  </div>`;
  const mapDistance = gs => {
    const ds = gs.filter(g => g.distance != null).map(g => g.distance);
    return ds.length ? Math.min(...ds) : null;
  };
  const mapScore = gs => {
    const ss = gs.filter(g => g.score != null).map(g => g.score);
    return ss.length ? Math.max(...ss) : null;
  };
  const orderedMaps = [...maps.entries()].sort((a, b) => {
    const ad = mapDistance(a[1]), bd = mapDistance(b[1]);
    if ((ad == null) !== (bd == null)) return ad == null ? 1 : -1;
    if (ad != null) return ad - bd;
    const as = mapScore(a[1]), bs = mapScore(b[1]);
    if ((as == null) !== (bs == null)) return as == null ? 1 : -1;
    if (as != null) return bs - as;
    return String(a[0]).localeCompare(String(b[0]));
  });
  const noTargetsHtml = noTargets.length
    ? `<div class="route-plan-group">
    <div class="route-plan-group-header">
      <span class="route-plan-muted">no favor targets</span>
    </div>
    <div class="route-plan-group-items">${noTargets.map(planFavorRow).join('')}</div>
  </div>`
    : '';
  return orderedMaps.map(mapHtml).join('') + noTargetsHtml;
}

async function planFavorRoutesFor(it) {
  const targets = [...(it.decision.favor_targets || [])].sort((a, b) => (b.score || 0) - (a.score || 0));
  if (!targets.length) return;
  const row = [...document.querySelectorAll('.route-plan-item')]
    .find(r => r.dataset.name === it.name);
  const container = row?.querySelector('.route-plan-favor-routes');
  if (!container) return;

  // Best target + score live in the group header; this only renders the ⇄ picker.
  if (targets.length <= 1) { container.innerHTML = ''; return; }

  const pick = __routeFavorPick[it.name] || targets[0].npc;
  const chosen = targets.find(t => t.npc === pick) || targets[0];

  container.innerHTML = `<button class="route-plan-switch" title="choose another target">⇄</button>
    <span class="route-plan-picker" hidden>${targets.map(t => `
      <button class="route-plan-option" data-npc="${escapeHtml(t.npc)}">${escapeHtml(t.npc)} <span class="route-plan-area">${escapeHtml(t.area || '')}</span> +${t.score ?? 0}${t.npc === chosen.npc ? ' ✓' : ''}</button>`).join('')}</span>`;

  container.querySelector('.route-plan-switch')?.addEventListener('click', () => {
    const picker = container.querySelector('.route-plan-picker');
    if (picker) picker.hidden = !picker.hidden;
  });
  container.querySelectorAll('.route-plan-option').forEach(btn => {
    btn.addEventListener('click', () => {
      __routeFavorPick[it.name] = btn.dataset.npc;
      renderRoutePlan(); // re-render so the item moves to the correct target group
    });
  });
}

function routeText(d) {
  let base = '';
  if (d.verdict === 'favor') {
    let targets = (d.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
    if (!targets.length) base = 'no available NPC';
    else {
      targets = [...targets].sort((a, b) => {
        const aPri = state.prioritizedNPCs.has(a.npc) ? 0 : 1;
        const bPri = state.prioritizedNPCs.has(b.npc) ? 0 : 1;
        return aPri - bPri || b.score - a.score;
      });
      base = targets.map(t => {
        const cap = state.traderCapacity[t.npc];
        const broke = cap && cap.remaining <= 0 && cap.limit > 0;
        const pri = state.prioritizedNPCs.has(t.npc) ? '★ ' : '';
        return `${pri}${t.npc} (${t.area}) +${t.score}${broke ? ' ⚠ 0g' : ''}`;
      }).join(' · ') || 'gift';
    }
  } else if (d.player_price) {
    base = `player price: ${d.player_price.toFixed(0)}g`;
  } else {
    base = d.sell_reason || '';
  }
  // Append price history if available
  const ph = state.priceHistory?.[d.name];
  if (ph && ph.count > 1) {
    base += ` <span class="price-hint">avg ${ph.average.toFixed(0)}g · med ${ph.median.toFixed(0)}g</span>`;
  }
  return base;
}

// Controls
$('#start').addEventListener('click', async () => {
  const dungeon = $('#dungeon').value.trim() || 'unnamed';
  const notes = $('#notes').value.trim();
  const s = await api('/api/session/start', 'POST', { dungeon, notes });
  if (s) {
    switchView('tracker');
    renderSession(s);
  }
});
$('#stop').addEventListener('click', async () => {
  const s = await api('/api/session/stop', 'POST');
  if (s) {
    renderSession(s);
    switchView('summary');
  }
});

// Session templates: picking one pre-fills dungeon/zone + notes (still editable).
$('#session-template-select')?.addEventListener('change', e => {
  const t = templatePrefill(state.sessionTemplates, e.target.value);
  if (!t) return;
  if (t.zone) $('#dungeon').value = t.zone;
  if (t.notes) $('#notes').value = t.notes;
});

// ── Tracked recipes panel (Recipes view): material shortfall vs active loot ──
function renderTrackedRecipesPanel() {
  const body = $('#tracked-recipes-body');
  if (!body) return;
  const names = getTrackedRecipes();
  if (!names.length) {
    body.innerHTML = '<div class="route-plan-empty">No tracked recipes — use Track on a search result below.</div>';
    return;
  }
  const loot = Array.isArray(state.session?.loot) ? state.session.loot : null;
  const ing = getTrackedIngredients();
  const noSession = loot === null ? '<div class="route-plan-empty">no active session</div>' : '';
  body.innerHTML = noSession + names.map(name => {
    const ings = Array.isArray(ing[name]) ? ing[name] : [];
    const rows = ings.length
      ? trackedMaterialShortfall(ings, loot || []).map(m => {
          const ok = m.have >= m.qty;
          return `<div class="mat-shortfall-row ${ok ? 'ok' : 'missing'}">
            <span class="mat-shortfall-name">${escapeHtml(m.name)}</span>
            <span class="mat-shortfall-count">${m.have} / ${m.qty}${ok ? ' <span class="mat-ok">✓</span>' : ''}</span>
          </div>`;
        }).join('')
      : '<div class="route-plan-empty">no ingredient data saved</div>';
    return `<div class="tracked-recipe">
      <div class="tracked-recipe-head">${escapeHtml(name)}</div>
      ${rows}
    </div>`;
  }).join('');
}

// ── Sell Checklist (Tracker view): traders to visit for this session's loot ──
async function renderSellChecklist() {
  const body = $('#sell-checklist-body');
  if (!body) return;
  const s = await api('/api/session');
  const loot = s && Array.isArray(s.loot) ? s.loot : [];
  if (!s || !loot.length) {
    body.innerHTML = '<div class="route-plan-empty">no active session with loot</div>';
    return;
  }

  // Sellable unique items, most valuable first; cap planner calls at 8.
  const byName = new Map();
  for (const e of loot) {
    if (!(Number(e.value) > 0)) continue;
    const name = String(e.name || '').trim();
    if (!name) continue;
    const row = byName.get(name) || { name, count: 0, value: Number(e.value) || 0 };
    row.count += Number(e.count) || 1;
    row.value = Math.max(row.value, Number(e.value) || 0);
    byName.set(name, row);
  }
  const items = [...byName.values()].sort((a, b) => b.value - a.value).slice(0, 8);
  if (!items.length) {
    body.innerHTML = '<div class="route-plan-empty">no sellable loot (value &gt; 0) in this session</div>';
    return;
  }

  // Aggregate routes by trader; items with no routes are skipped silently.
  const byTrader = new Map();
  for (const it of items) {
    const routes = await fetchRoutesFor(it);
    for (const r of routes) {
      const t = String(r?.trader || '').trim();
      if (!t) continue;
      const row = byTrader.get(t) || {
        name: t,
        area: String(r.area || ''),
        distance: r.distance_km == null ? null : Number(r.distance_km),
        capacity: Number(r.remaining_capacity_g) || 0,
        items: []
      };
      if (!row.items.includes(it.name)) row.items.push(it.name);
      byTrader.set(t, row);
    }
  }
  const traders = [...byTrader.values()].sort(sortTradersByDistance);
  if (!traders.length) {
    body.innerHTML = '<div class="route-plan-empty">no traders found for this loot</div>';
    return;
  }

  const key = sellChecklistKey(s);
  let checks = {};
  try { checks = JSON.parse(localStorage.getItem('gorgon_sell_check_' + key) || '{}') || {}; } catch (e) { checks = {}; }

  // Bucket trader rows by map (nearest map first, unknown-area last); rows keep
  // their distance-sorted order within each section.
  const mapName = t => String(t.area || '').trim() || 'unknown area';
  const maps = new Map();
  for (const t of traders) {
    const name = mapName(t);
    if (!maps.has(name)) maps.set(name, []);
    maps.get(name).push(t);
  }
  const rowHtml = t => {
    const dist = t.distance != null ? t.distance.toFixed(1) + ' km' : '?';
    return `<label class="sell-check-row">
      <input type="checkbox" data-trader="${escapeHtml(t.name)}" ${checks[t.name] ? 'checked' : ''}>
      <span class="sell-check-name">${escapeHtml(t.name)}</span>
      <span class="sell-check-area">${escapeHtml(t.area)}</span>
      <span class="sell-check-items">${t.items.map(escapeHtml).join(', ')}</span>
      <span class="sell-check-dist">${dist}</span>
      <span class="sell-check-cap">${Math.round(t.capacity).toLocaleString()}g</span>
    </label>`;
  };
  const mapDistance = ts => {
    const ds = ts.filter(t => t.distance != null).map(t => t.distance);
    return ds.length ? Math.min(...ds) : null;
  };
  const orderedMaps = [...maps.entries()].sort((a, b) => {
    const ad = mapDistance(a[1]), bd = mapDistance(b[1]);
    if ((ad == null) !== (bd == null)) return ad == null ? 1 : -1;
    if (ad != null) return ad - bd;
    return String(a[0]).localeCompare(String(b[0]));
  });

  body.innerHTML = `<div class="sell-checklist-toolbar">
      <span class="muted">${traders.length} trader${traders.length > 1 ? 's' : ''} · ${items.length} item${items.length > 1 ? 's' : ''}</span>
      <button class="add-btn" id="sell-checklist-reset">Reset</button>
    </div>` +
    orderedMaps.map(([name, ts]) => `
      <div class="route-plan-map">
        <div class="route-plan-map-header">📍 ${escapeHtml(name)}</div>
        ${ts.map(rowHtml).join('')}
      </div>`).join('');

  body.querySelectorAll('input[type=checkbox]').forEach(cb => {
    cb.addEventListener('change', () => {
      checks[cb.dataset.trader] = cb.checked;
      localStorage.setItem('gorgon_sell_check_' + key, JSON.stringify(checks));
    });
  });
  body.querySelector('#sell-checklist-reset').addEventListener('click', () => {
    localStorage.removeItem('gorgon_sell_check_' + key);
    renderSellChecklist();
  });
}

// Skills view — XP per hour + NPC time tracker
function renderSkillsView() {
  const container = $('#skills-list');
  if (!container) return;
  const s = state.session;
  const gains = s?.xp_gains;
  if (!gains || gains.length === 0) {
    container.innerHTML = '<div class="summary-empty">No XP data yet. Start a session and gain some XP.</div>';
    return;
  }

  const start = new Date(s.started_at);
  const end = s.state === 'running' ? new Date() : new Date(s.ended_at);
  const elapsed = end - start;
  const hours = elapsed / 3600000;
  const mins = Math.floor(elapsed / 60000);

  // Group and sum per skill
  const bySkill = {};
  for (const g of gains) {
    bySkill[g.skill] = (bySkill[g.skill] || 0) + g.amount;
  }

  const entries = Object.entries(bySkill)
    .map(([skill, total]) => ({ skill, total, perHour: hours > 0 ? total / hours : total }))
    .sort((a, b) => b.perHour - a.perHour);

  const maxPerHour = entries[0]?.perHour || 1;

  const count = $('#skills-count');
  if (count) count.textContent = `${entries.length} skill${entries.length !== 1 ? 's' : ''}`;

  const elapsedStr = mins >= 60
    ? `${Math.floor(mins / 60)}h ${mins % 60}m`
    : `${mins}m`;

  // Summary stats
  const totalXP = entries.reduce((sum, e) => sum + e.total, 0);
  const totalGold = (s.loot || []).reduce((sum, l) => sum + (l.value || 0) * (l.count || 0), 0);
  const kills = (s.kills || []).length;
  const deaths = (s.deaths || []).length;
  const levels = s.level_ups || [];

  const widgets = [
    { label: 'Session', value: elapsedStr },
    { label: 'Total XP', value: totalXP.toLocaleString() },
    { label: 'XP /hr', value: Math.round(totalXP / hours).toLocaleString() },
    { label: 'Gold', value: Math.round(totalGold).toLocaleString() },
    { label: 'Kills', value: kills.toLocaleString() },
    { label: 'Deaths', value: deaths.toLocaleString() },
  ];
  let html = `<div style="display:flex;flex-wrap:wrap;gap:8px;margin-bottom:16px">`;
  for (const w of widgets) {
    html += `<div style="flex:1;min-width:100px;background:var(--card-bg);border:1px solid var(--border);border-radius:8px;padding:10px 14px;text-align:center">
      <div style="font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px">${w.label}</div>
      <div style="font-size:18px;font-weight:700;color:var(--text);margin-top:2px">${escapeHtml(w.value)}</div>
    </div>`;
  }
  html += `</div>`;

  // Level-ups
  if (levels.length > 0) {
    html += `<div style="margin-bottom:12px;padding:10px 14px;background:var(--card-bg);border:1px solid var(--border);border-radius:8px">
      <div style="font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px;margin-bottom:6px">Level Ups</div>
      <div style="display:flex;flex-wrap:wrap;gap:6px">`;
    for (const lv of levels) {
      html += `<span style="background:var(--bg);padding:3px 10px;border-radius:4px;font-size:13px;font-weight:600">${escapeHtml(lv.skill)} → ${lv.level}</span>`;
    }
    html += `</div></div>`;
  }

  // (dashboard widgets live on the Dashboard view, not here)

  html += `<div style="margin-bottom:8px;font-size:12px;color:var(--muted)">XP per skill (sorted by rate)</div>`;
  container.innerHTML = html;

  for (const e of entries) {
    const pct = e.perHour / maxPerHour;
    const card = document.createElement('div');
    card.className = 'history-card';
    card.style.cssText = 'cursor:default;padding:12px';

    card.innerHTML = `<div class="history-card-body">
      <div class="history-card-header" style="margin-bottom:4px">
        <div class="history-card-dungeon">${escapeHtml(e.skill)}</div>
        <div class="history-card-date">${Math.round(e.perHour).toLocaleString()} /hr</div>
      </div>
      <div style="display:flex;align-items:baseline;gap:6px;margin-bottom:8px">
        <span style="font-size:22px;font-weight:700;color:var(--text)">+${e.total.toLocaleString()}</span>
        <span style="font-size:12px;color:var(--muted)">XP total</span>
      </div>
      <div style="height:6px;background:var(--bg);border-radius:3px;overflow:hidden">
        <div style="height:100%;width:${(pct * 100).toFixed(1)}%;background:var(--accent);border-radius:3px;transition:width .3s"></div>
      </div>
    </div>`;
    container.appendChild(card);
  }
}

// Recipes view
function renderRecipesView() {
  const container = $('#recipes-list');
  if (!container) return;
  renderTrackedRecipesPanel();
  const recipes = state.recipes;
  if (!recipes) {
    container.innerHTML = '<div class="summary-empty">Loading recipes...</div>';
    return;
  }

  const search = ($('#recipes-search')?.value || '').toLowerCase();
  const skillFilter = $('#recipes-skill-filter')?.value || '';
  const maxLevel = parseInt($('#recipes-level')?.value) || 0;

  // Populate skill filter dropdown
  const select = $('#recipes-skill-filter');
  if (select && select.options.length <= 1) {
    const skills = new Set();
    for (const r of Object.values(recipes)) if (r.Skill) skills.add(r.Skill);
    for (const s of [...skills].sort()) {
      const opt = document.createElement('option');
      opt.value = s;
      opt.textContent = s;
      select.appendChild(opt);
    }
  }

  // Convert map to array for filtering
  const allRecipes = Object.values(recipes);
  const filtered = allRecipes.filter(r => {
    if (search && !(r.Name || '').toLowerCase().includes(search) && !(r.Skill || '').toLowerCase().includes(search)) return false;
    if (skillFilter && r.Skill !== skillFilter) return false;
    if (maxLevel > 0 && (r.SkillLevelReq || 0) > maxLevel) return false;
    return true;
  });

  const count = $('#recipes-count');
  if (count) count.textContent = `${filtered.length} recipe${filtered.length !== 1 ? 's' : ''} (${allRecipes.length} total)`;

  container.innerHTML = '';
  if (filtered.length === 0) {
    container.innerHTML = '<div class="summary-empty">No recipes match your filters</div>';
    return;
  }

  for (const r of filtered.slice(0, 200)) {
    const card = document.createElement('div');
    card.className = 'history-card';
    card.style.cursor = 'default';

    const mats = (r.Ingredients || []).map(m => {
      const name = state.itemNames?.[m.ItemCode] || `#${m.ItemCode}`;
      return `${escapeHtml(name)} x${m.StackSize}`;
    }).join(', ') || 'none';

    const results = (r.ResultItems || []).map(ri => {
      const name = state.itemNames?.[ri.ItemCode] || `#${ri.ItemCode}`;
      return `${escapeHtml(name)} x${ri.StackSize}`;
    }).join(', ');

    card.innerHTML = `<div class="history-card-body">
      <div class="history-card-header">
        <div class="history-card-dungeon">${escapeHtml(r.Name || 'Unnamed')}</div>
        <div class="history-card-date">${escapeHtml(r.Skill || '')} · Lv ${r.SkillLevelReq ?? '?'}</div>
      </div>
      ${r.Description ? `<div class="history-card-notes" style="margin-top:4px">${escapeHtml(r.Description)}</div>` : ''}
      <div class="history-card-stats" style="flex-wrap:wrap">
        <div class="history-stat"><div class="history-stat-label">Materials</div><div class="history-stat-value" style="font-size:12px">${mats}</div></div>
        ${results ? `<div class="history-stat"><div class="history-stat-label">Result</div><div class="history-stat-value" style="font-size:12px">${results}</div></div>` : ''}
      </div>
    </div>`;
    container.appendChild(card);
  }
  if (filtered.length > 200) {
    const more = document.createElement('div');
    more.className = 'summary-empty';
    more.textContent = `+ ${filtered.length - 200} more recipes (narrow your search)`;
    container.appendChild(more);
  }
}

// Debounced live search against /api/recipes/search when text/level filters
// are active; falls back to the local full-list render otherwise.
let __recipesSearchTimer = null;
function recipesLiveSearch() {
  const q = ($('#recipes-search')?.value || '').trim();
  const level = parseInt($('#recipes-level')?.value) || 0;
  const skill = $('#recipes-skill-filter')?.value || '';
  if (!q && !level) { renderRecipesView(); return; }
  clearTimeout(__recipesSearchTimer);
  __recipesSearchTimer = setTimeout(async () => {
    const container = $('#recipes-list');
    if (!container) return;
    const params = new URLSearchParams();
    if (q) params.set('q', q);
    if (skill) params.set('skill', skill);
    if (level) params.set('level', level);
    const data = await api('/api/recipes/search?' + params.toString());
    if (!data) return;
    const hits = Array.isArray(data.recipes) ? data.recipes : [];
    const count = $('#recipes-count');
    if (count) count.textContent = `${hits.length} result${hits.length !== 1 ? 's' : ''} (server search, max 50)`;
    container.innerHTML = '';
    if (hits.length === 0) {
      container.innerHTML = '<div class="summary-empty">No recipes match your filters</div>';
      return;
    }
    for (const r of hits) {
      const card = document.createElement('div');
      card.className = 'history-card';
      card.style.cursor = 'default';
      const tracked = getTrackedRecipes().includes(r.name);
      card.innerHTML = `<div class="history-card-body">
        <div class="history-card-header">
          <div class="history-card-dungeon">${escapeHtml(r.name || 'Unnamed')}</div>
          <div class="history-card-date">${escapeHtml(r.skill || '')} · Lv ${r.level ?? '?'}</div>
        </div>
        <div class="history-card-stats" style="flex-wrap:wrap">
          ${r.result_item ? `<div class="history-stat"><div class="history-stat-label">Result</div><div class="history-stat-value" style="font-size:12px">${escapeHtml(r.result_item)}</div></div>` : ''}
          <div class="history-stat"><div class="history-stat-label">Ingredients</div><div class="history-stat-value" style="font-size:12px">${(r.ingredients || []).map(i => escapeHtml(i)).join(', ') || '—'}</div></div>
        </div>
        <div class="history-card-actions">
          <button class="track-recipe-btn${tracked ? ' tracked' : ''}">${tracked ? 'Untrack' : 'Track'}</button>
        </div>
      </div>`;
      card.querySelector('.track-recipe-btn').addEventListener('click', () => {
        toggleTrackedRecipe(r.name, r.ingredients);
        recipesLiveSearch();
        renderTrackedRecipesPanel();
        if (state.currentView === 'tracker' && state.session) renderLootTable(state.session);
      });
      container.appendChild(card);
    }
  }, 300);
}

$('#recipes-search')?.addEventListener('input', () => {
  if (state.currentView === 'recipes') recipesLiveSearch();
});
$('#recipes-skill-filter')?.addEventListener('change', () => {
  if (state.currentView === 'recipes') recipesLiveSearch();
});
$('#recipes-level')?.addEventListener('input', () => {
  if (state.currentView === 'recipes') recipesLiveSearch();
});

// Poll feed replacement (avoids per-tab EventSource connection limits)
let __lastLootSig = '';
let __pollInFlight = false;
async function pollUpdates() {
  if (__pollInFlight) return;
  __pollInFlight = true;
  try {
    const s = await api('/api/session');
    if (!s) return;

    // Rare loot notification on change in most recent loot entry
    const loot = Array.isArray(s.loot) && s.loot.length ? s.loot[s.loot.length - 1] : null;
    if (loot) {
      const sig = `${loot.name}|${loot.count}|${loot.last_seen || ''}`;
      if (__lastLootSig && sig !== __lastLootSig) {
        const lootValue = Number(loot.value ?? loot.valor ?? 0);
        if (lootValue >= state.notificationThreshold) {
          showRareLootNotification(loot);
        }
      }
      __lastLootSig = sig;
    }

    renderSession(s);

    // Price history doesn't need 3s cadence; fetch less often to reduce UI churn.
    __pricePollTick++;
    if (__pricePollTick % 5 === 0) {
      const ph = await api('/api/prices');
      if (ph) state.priceHistory = ph;
    }

  } catch (e) {
    // keep silent; api() already toasts for hard failures
  } finally {
    __pollInFlight = false;
  }
}
setInterval(pollUpdates, 3000);

function showRareLootNotification(loot) {
  if (!('Notification' in window)) return;
  
  if (Notification.permission === 'granted') {
    const notification = new Notification('Rare Loot!', {
      body: `${loot.name} (${loot.value}g)`,
      icon: loot.icon_url || undefined,
      tag: `loot-${loot.item_id}`
    });
    
    // Auto-close after 5 seconds
    setTimeout(() => notification.close(), 5000);
  } else if (Notification.permission !== 'denied') {
    Notification.requestPermission().then(permission => {
      if (permission === 'granted') {
        showRareLootNotification(loot);
      }
    });
  }
}

let itemsCache = null;
async function renderItemsView() {
  const container = $('#items-list');
  if (!container) return;
  try {
    const search = ($('#items-search')?.value || '').toLowerCase();
    if (!itemsCache) {
      container.innerHTML = '<div class="summary-empty">Loading items...</div>';
      const itemsResp = await api('/api/items');
      if (!itemsResp) { container.innerHTML = '<div class="summary-empty">Failed to load items</div>'; return; }
      itemsCache = Array.isArray(itemsResp) ? itemsResp : [];
    }

    const filtered = search
      ? itemsCache.filter(i => (i.Name || '').toLowerCase().includes(search) || (Array.isArray(i.Keywords) ? i.Keywords : []).some(k => String(k).toLowerCase().includes(search)))
      : itemsCache;
    $('#items-count').textContent = `${filtered.length} item${filtered.length !== 1 ? 's' : ''} (${itemsCache.length} total)`;

    container.innerHTML = '';
    if (filtered.length === 0) {
      container.innerHTML = '<div class="summary-empty">No items match your search</div>';
      return;
    }

    for (const item of filtered.slice(0, 500)) {
      const card = document.createElement('div');
      card.className = 'item-card';
      card.innerHTML = `<div class="item-card-name">${escapeHtml(item.Name || 'Unknown')}</div>
        <div class="item-card-id">#${item.ItemID || '?'}</div>
        <div class="item-card-value">${item.Value || 0}g</div>
        ${item.Keywords?.length ? `<div class="item-card-keywords">${item.Keywords.slice(0, 3).map(k => '<span class="item-tag">' + escapeHtml(k) + '</span>').join(' ')}</div>` : ''}`;
      container.appendChild(card);
    }
    if (filtered.length > 500) {
      const more = document.createElement('div');
      more.className = 'summary-empty';
      more.textContent = `+ ${filtered.length - 500} more items (narrow your search)`;
      container.appendChild(more);
    }
  } catch (e) {
    console.error('renderItemsView failed', e);
    container.innerHTML = `<div class="summary-empty">Items render error: ${escapeHtml(e?.message || String(e))}</div>`;
  }
}

// Items search
$('#items-search')?.addEventListener('input', () => {
  if (state.currentView === 'items') renderItemsView();
});

async function refreshAll() {
  const s = await api('/api/session');
  renderSession(s);
  // Load price history
  const ph = await api('/api/prices');
  if (ph) state.priceHistory = ph;
}

function defaultDashLayout() {
  return [
    { type: 'quick-stats', title: 'Quick Stats', size: 'full', visible: true },
    { type: 'session-status', title: 'Session', size: 'half', visible: true },
    { type: 'recent-sessions', title: 'Recent Sessions', size: 'half', visible: true },
    { type: 'value-chart', title: 'Value Trend', size: 'half', visible: true },
    { type: 'favor-chart', title: 'Favor Trend', size: 'half', visible: true },
    { type: 'reset-alerts', title: '⏳ Nearing Reset', size: 'half', visible: true },
  ];
}
function loadDashLayout() {
  let layout;
  try { layout = JSON.parse(localStorage.getItem('dashLayout') || 'null'); } catch { layout = null; }
  if (!layout) return defaultDashLayout();
  // Merge in any new default widgets that aren't in the saved layout
  const existing = new Set(layout.map(w => w.type));
  for (const def of defaultDashLayout()) {
    if (!existing.has(def.type)) {
      layout.push(def);
      existing.add(def.type);
    }
  }
  return layout;
}
function saveDashLayout() { localStorage.setItem('dashLayout', JSON.stringify(state.dashLayout)); }

/* widget renderers — each takes (widgetConfig, sessions) and returns HTML string or appends to parent */
const widgetRenderers = {};

widgetRenderers['quick-stats'] = function(w, sessions) {
  const totalSessions = sessions.length;
  const totalValue = sessions.reduce((s, s2) => s + s2.total_value, 0);
  const totalFavor = sessions.reduce((s, s2) => s + (s2.favor_items || 0), 0);
  const totalSell = sessions.reduce((s, s2) => s + (s2.sell_items || 0), 0);
  return `<div class="dashboard-stats">
    <div class="stat-card"><div class="stat-label">Total Sessions</div><div class="stat-value">${totalSessions}</div></div>
    <div class="stat-card"><div class="stat-label">Total Loot Value</div><div class="stat-value">${totalValue.toFixed(0)}g</div></div>
    <div class="stat-card"><div class="stat-label">Items Favor</div><div class="stat-value">${totalFavor}</div></div>
    <div class="stat-card"><div class="stat-label">Items Sell</div><div class="stat-value">${totalSell}</div></div>
  </div>`;
};

widgetRenderers['session-status'] = function(w, sessions) {
  const info = state.session;
  if (info && info.state === 'running') {
    const elapsed = fmtElapsed(Date.now() - new Date(info.started_at).getTime());
    return `<div class="dash-info-box"><div class="dash-active">
      <div><div class="dash-dungeon">${escapeHtml(info.dungeon || 'unnamed')}</div>
      <div class="dash-elapsed">Started ${new Date(info.started_at).toLocaleTimeString()} · ${elapsed}</div></div>
      <div><span class="session-badge running">Running</span></div>
    </div></div>`;
  }
  if (info && info.state === 'stopped') {
    return `<div class="dash-info-box"><div class="dash-active">
      <div><div class="dash-dungeon">${escapeHtml(info.dungeon || 'unnamed')}</div>
      <div class="dash-elapsed">Ended ${new Date(info.ended_at).toLocaleTimeString()}</div></div>
      <div><span class="session-badge stopped">Stopped</span></div>
    </div></div>`;
  }
  return '<div class="dash-info-box"><span class="muted">No active session</span></div>';
};

widgetRenderers['recent-sessions'] = function(w, sessions) {
  const list = sessions.slice(0, 8);
  if (list.length === 0) return '<div class="dash-recent-list"><div class="summary-empty">No sessions yet</div></div>';
  let html = '<div class="dash-recent-list">';
  for (const s of list) {
    const date = new Date(s.started_at);
    const dateStr = date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], {hour12: false, hour: '2-digit', minute: '2-digit'});
    html += `<div class="dash-recent-item" onclick="switchView('history');if(typeof loadSessionDetail==='function')loadSessionDetail('${s.id}')">
      <div><div class="dash-item-dungeon">${escapeHtml(s.dungeon)}</div>
      <div class="dash-item-meta">${dateStr} · ${s.total_items} items</div></div>
      <div class="dash-item-value">${s.total_value.toFixed(0)}g</div>
    </div>`;
  }
  html += '</div>';
  return html;
};

widgetRenderers['value-chart'] = function(w, sessions) {
  // defer to canvas renderer — return a placeholder canvas
  return `<canvas id="value-chart" height="160" style="width:100%;border-radius:var(--radius);background:var(--row);border:1px solid var(--border);box-shadow:var(--shadow)"></canvas>`;
};

widgetRenderers['favor-chart'] = function(w, sessions) {
  return `<canvas id="favor-chart" height="160" style="width:100%;border-radius:var(--radius);background:var(--row);border:1px solid var(--border);box-shadow:var(--shadow)"></canvas>`;
};

widgetRenderers['reset-alerts'] = function(w, sessions) {
  const caps = state.traderCapacity || {};
  const sorted = Object.entries(caps)
    .filter(([_, c]) => (c.limit || 0) > 0)
    .sort((a, b) => {
      const am = Number.isFinite(a[1].resetMinutes) ? a[1].resetMinutes : Number.POSITIVE_INFINITY;
      const bm = Number.isFinite(b[1].resetMinutes) ? b[1].resetMinutes : Number.POSITIVE_INFINITY;
      if (am !== bm) return am - bm;
      return (a[1].remaining || 0) - (b[1].remaining || 0);
    })
    .slice(0, 10);
  if (sorted.length === 0) return '<div class="dash-info-box"><span class="muted">No trader limits tracked yet</span></div>';
  let html = '<div class="dash-recent-list">';
  for (const [name, cap] of sorted) {
    const mins = Number.isFinite(cap.resetMinutes) ? cap.resetMinutes : Number.POSITIVE_INFINITY;
    const color = mins <= 24 * 60 ? '#e74c3c' : mins <= 3 * 24 * 60 ? 'var(--sell-vendor)' : 'var(--muted)';
    html += `<div class="dash-recent-item" style="cursor:default">
      <div><div class="dash-item-dungeon">${escapeHtml(name)}</div>
      <div class="dash-item-meta">${escapeHtml(cap.area)}</div></div>
      <div class="dash-item-value" style="color:${color}">${escapeHtml(cap.reset || '?')} · ${Math.round(cap.remaining || 0).toLocaleString()}g</div>
    </div>`;
  }
  html += '</div>';
  return html;
};

widgetRenderers['trader-alerts'] = function(w, sessions) {
  const caps = state.traderCapacity || {};
  const alerts = Object.entries(caps)
    .filter(([_, c]) => c.limit > 0 && c.remaining <= 0)
    .sort((a, b) => a[1].remaining - b[1].remaining);
  if (alerts.length === 0) return '<div class="dash-info-box"><span class="muted">No trader alerts</span></div>';
  let html = '<div class="dash-recent-list">';
  for (const [name, cap] of alerts) {
    html += `<div class="dash-recent-item" style="cursor:default">
      <div><div class="dash-item-dungeon">${escapeHtml(name)}</div>
      <div class="dash-item-meta">Reset: ${cap.reset || '?'}</div></div>
      <div class="dash-item-value" style="color:#e74c3c">0g left</div>
    </div>`;
  }
  html += '</div>';
  return html;
};

widgetRenderers['top-items'] = function(w, sessions) {
  const top = [...sessions].sort((a, b) => b.total_value - a.total_value).slice(0, 5);
  if (top.length === 0) return '<div class="dash-info-box"><span class="muted">No data yet</span></div>';
  let html = '<div class="dash-recent-list">';
  for (const s of top) {
    const date = new Date(s.started_at);
    const dateStr = date.toLocaleDateString();
    html += `<div class="dash-recent-item" style="cursor:default">
      <div><div class="dash-item-dungeon">${escapeHtml(s.dungeon)}</div>
      <div class="dash-item-meta">${dateStr} · ${s.total_items} items</div></div>
      <div class="dash-item-value">${s.total_value.toFixed(0)}g</div>
    </div>`;
  }
  html += '</div>';
  return html;
};


async function renderDashboard() {
  const sessions = await api('/api/sessions');
  if (!sessions) return;

  state.dashLayout = loadDashLayout();
  const container = $('#dash-widgets');
  if (!container) return;
  container.innerHTML = '';

  const visible = state.dashLayout.filter(w => w.visible);
  if (visible.length === 0) {
    container.innerHTML = '<div class="summary-empty">All widgets hidden. Click ⚙ to customize.</div>';
    return;
  }

  for (const widget of visible) {
    const fn = widgetRenderers[widget.type];
    if (!fn) continue;
    const el = document.createElement('div');
    el.className = `dash-widget size-${widget.size || 'half'}`;
    el.innerHTML = `<div class="dash-widget-header"><span class="dash-widget-title">${escapeHtml(widget.title)}</span></div>
      <div class="dash-widget-body" id="dw-${widget.type}"></div>`;
    container.appendChild(el);

    const body = el.querySelector('.dash-widget-body');
    try {
      const result = fn(widget, sessions);
      if (result && typeof result.then === 'function') {
        body.innerHTML = '<div class="summary-empty">Loading...</div>';
        result.then(html => { body.innerHTML = html; }).catch(e => { body.innerHTML = `<div class="summary-empty" style="color:var(--error)">Error: ${e.message}</div>`; });
      } else {
        body.innerHTML = result;
      }
    } catch(e) { body.innerHTML = `<div class="summary-empty" style="color:var(--error)">Error: ${e.message}</div>`; }

    // After rendering, draw charts for canvas widgets
    if (widget.type === 'value-chart' || widget.type === 'favor-chart') {
      const recentSessions = sessions.slice(0, 8);
      if (widget.type === 'value-chart') renderBarChart('#value-chart', recentSessions, s => s.total_value, '#5b93ff', '#2ecc71');
      if (widget.type === 'favor-chart') renderBarChart('#favor-chart', recentSessions, s => s.favor_items, '#e67e22', '#f1c40f');
    }
  }
}

function renderBarChart(canvasId, sessions, extract, colorA, colorB) {
  const canvas = $(canvasId);
  if (!canvas) return;
  const ctx = canvas.getContext('2d');
  const dpr = window.devicePixelRatio || 1;
  const rect = canvas.getBoundingClientRect();
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  canvas.width = rect.width * dpr;
  canvas.height = rect.height * dpr;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  const w = rect.width;
  const h = rect.height;

  ctx.clearRect(0, 0, w, h);
  if (sessions.length === 0) {
    ctx.fillStyle = '#7a7f8a';
    ctx.font = '12px monospace';
    ctx.textAlign = 'center';
    ctx.fillText('No session data yet', w/2, h/2);
    return;
  }

  const values = sessions.map(s => extract(s)).reverse();
  const max = Math.max(...values, 1);
  const pad = { t: 8, r: 8, b: 20, l: 8 };
  const chartW = w - pad.l - pad.r;
  const chartH = h - pad.t - pad.b;
  const barW = Math.min(24, chartW / values.length - 4);

  ctx.fillStyle = '#1e2128';
  ctx.fillRect(0, 0, w, h);

  for (let i = 0; i < values.length; i++) {
    const barH = (values[i] / max) * chartH;
    const x = pad.l + i * (chartW / values.length) + (chartW / values.length - barW) / 2;
    const y = pad.t + chartH - barH;
    const gradient = ctx.createLinearGradient(x, y, x, pad.t + chartH);
    gradient.addColorStop(0, colorA);
    gradient.addColorStop(1, colorB);
    ctx.fillStyle = gradient;
    ctx.beginPath();
    ctx.roundRect(x, y, barW, barH, [3, 3, 0, 0]);
    ctx.fill();

    ctx.fillStyle = '#7a7f8a';
    ctx.font = '9px monospace';
    ctx.textAlign = 'center';
    ctx.fillText(values[i].toFixed(0), x + barW/2, y - 4);
  }
}

function renderMultiLineDamageChart(canvasId, series, durationSec, options = {}) {
  const canvas = $(canvasId);
  if (!canvas) return;
  const ctx = canvas.getContext('2d');
  const dpr = window.devicePixelRatio || 1;
  const rect = canvas.getBoundingClientRect();
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  canvas.width = rect.width * dpr;
  canvas.height = rect.height * dpr;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

  const w = rect.width;
  const h = rect.height;
  ctx.clearRect(0, 0, w, h);
  ctx.fillStyle = '#1e2128';
  ctx.fillRect(0, 0, w, h);

  if (!series || series.length === 0) {
    ctx.fillStyle = '#7a7f8a';
    ctx.font = '12px monospace';
    ctx.textAlign = 'center';
    ctx.fillText('No chart data yet', w / 2, h / 2);
    return;
  }

  const pad = { t: 12, r: 12, b: 24, l: 42 };
  const chartW = Math.max(10, w - pad.l - pad.r);
  const chartH = Math.max(10, h - pad.t - pad.b);
  const len = Math.max(...series.map(s => s.values.length), 0);
  const maxYRaw = Math.max(0, ...series.flatMap(s => s.values));
  const maxY = Math.max(1, maxYRaw * 1.1);

  ctx.strokeStyle = '#2a2f39';
  ctx.lineWidth = 1;
  for (let i = 0; i <= 4; i++) {
    const gy = pad.t + (chartH * i) / 4;
    ctx.beginPath();
    ctx.moveTo(pad.l, gy);
    ctx.lineTo(pad.l + chartW, gy);
    ctx.stroke();
  }

  const xAt = i => pad.l + (i / Math.max(1, len - 1)) * chartW;
  const yAt = v => pad.t + chartH - (v / maxY) * chartH;

  const palette = ['#5b93ff', '#2ecc71', '#f1c40f', '#e67e22', '#9b59b6', '#e74c3c', '#1abc9c', '#95a5a6', '#f39c12', '#8e44ad', '#16a085', '#3498db'];
  series.forEach((s, idx) => {
    const vals = s.values;
    if (!vals || vals.length === 0) return;
    ctx.strokeStyle = palette[idx % palette.length];
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(xAt(0), yAt(vals[0] || 0));
    for (let i = 1; i < vals.length; i++) ctx.lineTo(xAt(i), yAt(vals[i] || 0));
    ctx.stroke();
  });

  const fmtT = sec => {
    const ss = Math.max(0, Math.floor(sec));
    const m = Math.floor(ss / 60);
    const r = ss % 60;
    return `${m}:${String(r).padStart(2, '0')}`;
  };

  ctx.fillStyle = '#9aa3b2';
  ctx.font = '10px monospace';
  ctx.textAlign = 'left';
  ctx.fillText('0', 4, pad.t + chartH + 1);
  ctx.fillText(maxY.toFixed(0), 4, pad.t + 9);
  ctx.textAlign = 'left';
  ctx.fillText(fmtT(0), pad.l, h - 6);
  ctx.textAlign = 'center';
  ctx.fillText(fmtT(durationSec / 2), pad.l + chartW / 2, h - 6);
  ctx.textAlign = 'right';
  ctx.fillText(fmtT(durationSec), pad.l + chartW, h - 6);

  const legendEl = options.legendId ? $(options.legendId) : null;
  if (legendEl) {
    legendEl.innerHTML = series.map((s, idx) => `<span style="display:inline-flex;align-items:center;gap:5px;margin-right:10px;margin-bottom:4px"><span style="width:10px;height:2px;background:${palette[idx % palette.length]};display:inline-block"></span>${escapeHtml(s.name)}</span>`).join('');
  }
}

// Widget settings
window.toggleDashSettings = function() {
  const panel = $('#dash-settings');
  if (!panel) return;
  panel.classList.toggle('hidden');
  if (!panel.classList.contains('hidden')) renderDashWidgetList();
};

window.saveDashChanges = function() {
  saveDashLayout();
  renderDashboard();
  $('#dash-settings')?.classList.add('hidden');
};

function renderDashWidgetList() {
  const list = $('#dash-widget-list');
  if (!list) return;
  state.dashLayout = loadDashLayout();
  list.innerHTML = '';
  state.dashLayout.forEach((w, i) => {
    const row = document.createElement('div');
    row.className = 'dash-widget-row';
    row.innerHTML = `
      <label class="dash-widget-toggle">
        <input type="checkbox" ${w.visible ? 'checked' : ''} data-idx="${i}">
        <span>${escapeHtml(w.title)}</span>
      </label>
      <div class="dash-widget-arrows">
        <button class="dash-arr" data-idx="${i}" data-dir="up" ${i === 0 ? 'disabled' : ''}>▲</button>
        <button class="dash-arr" data-idx="${i}" data-dir="down" ${i === state.dashLayout.length - 1 ? 'disabled' : ''}>▼</button>
      </div>`;
    list.appendChild(row);
  });
  list.querySelectorAll('input[type=checkbox]').forEach(cb => {
    cb.addEventListener('change', () => {
      const idx = parseInt(cb.dataset.idx);
      state.dashLayout[idx].visible = cb.checked;
      saveDashLayout();
    });
  });
  list.querySelectorAll('.dash-arr').forEach(btn => {
    btn.addEventListener('click', () => {
      const idx = parseInt(btn.dataset.idx);
      const dir = btn.dataset.dir;
      const swap = dir === 'up' ? idx - 1 : idx + 1;
      if (swap < 0 || swap >= state.dashLayout.length) return;
      [state.dashLayout[idx], state.dashLayout[swap]] = [state.dashLayout[swap], state.dashLayout[idx]];
      saveDashLayout();
      renderDashWidgetList();
      renderDashboard();
    });
  });
}

// Check canvas roundRect support
if (!CanvasRenderingContext2D.prototype.roundRect) {
  CanvasRenderingContext2D.prototype.roundRect = function(x, y, w, h, r) {
    if (!Array.isArray(r)) r = [r, r, r, r];
    const [tl, tr, br, bl] = r.map(v => Math.min(v || 0, Math.min(w, h) / 2));
    this.moveTo(x + tl, y);
    this.lineTo(x + w - tr, y);
    this.quadraticCurveTo(x + w, y, x + w, y + tr);
    this.lineTo(x + w, y + h - br);
    this.quadraticCurveTo(x + w, y + h, x + w - br, y + h);
    this.lineTo(x + bl, y + h);
    this.quadraticCurveTo(x, y + h, x, y + h - bl);
    this.lineTo(x, y + tl);
    this.quadraticCurveTo(x, y, x + tl, y);
    this.closePath();
  };
}

// Ticking elapsed counter
setInterval(() => {
  if (state.session && state.session.state === 'running') {
    elapsedEl.textContent = ' · ' + fmtElapsed(Date.now() - new Date(state.session.started_at).getTime());
  }
}, 1000);

// Zones view — show zone history from session snapshot

// ---- Session tags + drop-rate confidence helpers (pure) ----
function fmtZoneTime(sec) {
  const s = Math.max(0, Math.round(Number(sec) || 0));
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(r).padStart(2, '0')}`
    : `${m}:${String(r).padStart(2, '0')}`;
}

// Compact confidence suffix for a drop source/zone entry: sample count (or a
// low-sample warning) plus the 95% Wilson lower bound when present.
function dropConfidenceSuffix(e) {
  if (!e) return '';
  const kills = Number(e.kills) || 0;
  const conf = Number(e.conf_lower);
  if (!e.low_sample && !kills && !conf) return '';
  let bits = [];
  if (e.low_sample) bits.push(`⚠ low sample (n=${kills})`);
  else if (kills) bits.push(`· ${kills} samples`);
  if (conf > 0) bits.push(`≥${conf.toFixed(1)}%`);
  return ' <span class="muted">' + bits.join(' ') + '</span>';
}

function addSessionTag(tags, tag) {
  const t = String(tag || '').trim();
  if (!t) return Array.isArray(tags) ? tags.slice() : [];
  const list = Array.isArray(tags) ? tags.slice() : [];
  if (list.some(x => String(x).toLowerCase() === t.toLowerCase())) return list;
  list.push(t);
  return list;
}

function removeSessionTag(tags, tag) {
  const t = String(tag || '').toLowerCase();
  return (Array.isArray(tags) ? tags : []).filter(x => String(x).toLowerCase() !== t);
}

function sessionTagsMatch(tags, query) {
  const q = String(query || '').toLowerCase();
  if (!q) return true;
  return (Array.isArray(tags) ? tags : []).some(x => String(x).toLowerCase().includes(q));
}

// ---- Tracked recipes (localStorage) ----
const TRACKED_RECIPES_KEY = 'gorgon_tracked_recipes';
const TRACKED_INGREDIENTS_KEY = 'gorgon_tracked_recipe_ingredients';

function getTrackedRecipes() {
  try {
    const v = JSON.parse(localStorage.getItem(TRACKED_RECIPES_KEY) || '[]');
    return Array.isArray(v) ? v : [];
  } catch (e) { return []; }
}

function getTrackedIngredients() {
  try {
    const v = JSON.parse(localStorage.getItem(TRACKED_INGREDIENTS_KEY) || '{}');
    return v && typeof v === 'object' && !Array.isArray(v) ? v : {};
  } catch (e) { return {}; }
}

// Toggle a recipe in the tracked set. ingredients = raw "Name x2" strings from
// the search API (stored so the shortfall panel and loot highlight survive reloads).
function toggleTrackedRecipe(name, ingredients) {
  const names = getTrackedRecipes();
  const ing = getTrackedIngredients();
  const idx = names.indexOf(name);
  if (idx >= 0) {
    names.splice(idx, 1);
    delete ing[name];
  } else {
    names.push(name);
    if (Array.isArray(ingredients) && ingredients.length) ing[name] = ingredients.slice();
  }
  localStorage.setItem(TRACKED_RECIPES_KEY, JSON.stringify(names));
  localStorage.setItem(TRACKED_INGREDIENTS_KEY, JSON.stringify(ing));
  return names;
}

// Parse a pre-formatted ingredient string "Iron Ingot x2" → {name, qty}.
// Splits on the LAST " x" so item names containing "x" still parse; missing or
// malformed quantity defaults to 1; empty input → null.
function parseIngredient(str) {
  const raw = String(str || '').trim();
  if (!raw) return null;
  const idx = raw.lastIndexOf(' x');
  if (idx < 0) return { name: raw, qty: 1 };
  const name = raw.slice(0, idx).trim();
  const qty = parseInt(raw.slice(idx + 2).trim(), 10);
  return { name: name || raw, qty: Number.isFinite(qty) && qty > 0 ? qty : 1 };
}

function ingredientNames(ingredients) {
  return (Array.isArray(ingredients) ? ingredients : [])
    .map(parseIngredient).filter(Boolean).map(i => i.name);
}

// "Have" counts per ingredient from active-session loot (case-insensitive match).
function trackedMaterialShortfall(ingredients, loot) {
  const have = new Map();
  for (const l of Array.isArray(loot) ? loot : []) {
    const n = String(l.name || '').trim().toLowerCase();
    if (!n) continue;
    have.set(n, (have.get(n) || 0) + (Number(l.count) || 0));
  }
  return (Array.isArray(ingredients) ? ingredients : [])
    .map(parseIngredient).filter(Boolean)
    .map(ing => ({ name: ing.name, qty: ing.qty, have: have.get(ing.name.toLowerCase()) || 0 }));
}

function isTrackedMaterial(itemName, trackedIngredients) {
  const n = String(itemName || '').trim().toLowerCase();
  if (!n) return false;
  return Object.values(trackedIngredients || {}).some(list =>
    (Array.isArray(list) ? list : []).some(x => String(x).trim().toLowerCase() === n));
}

// Sell checklist: nearest traders first, unknown-distance traders after.
function sortTradersByDistance(a, b) {
  const ad = a.distance != null, bd = b.distance != null;
  if (ad !== bd) return ad ? -1 : 1;
  if (ad) return a.distance - b.distance;
  return String(a.name || '').localeCompare(String(b.name || ''));
}

// localStorage key suffix for a session's sell-checklist state.
function sellChecklistKey(s) {
  const t = s && (s.started_at || s.first_seen);
  return t ? String(t).replace(/[^0-9A-Za-z]/g, '') : 'current';
}

// Template pre-fill helper: find a template by name → {notes, zone} (zone optional).
function templatePrefill(templates, name) {
  const t = (Array.isArray(templates) ? templates : []).find(x => x && x.name === name);
  if (!t) return null;
  return { notes: String(t.notes || ''), zone: String(t.zone || '') };
}

function populateTemplateSelect() {
  const sel = $('#session-template-select');
  if (!sel) return;
  const cur = sel.value;
  sel.innerHTML = '<option value="">No template</option>' +
    (state.sessionTemplates || []).map(t =>
      `<option value="${escapeHtml(t.name)}">${escapeHtml(t.name)}</option>`).join('');
  sel.value = cur;
}

// Drop Rates view — aggregated across sessions
async function renderDropRatesView() {
  const container = $('#drop-list');
  if (!container) return;
  container.innerHTML = '<div class="summary-empty">Loading drop rates...</div>';

  // Default the zone filter to the player's current zone on first render of this view.
  if (state.dropZone === undefined) state.dropZone = String(state.session?.zone || '').trim();
  const zone = state.dropZone;
  const source = String(state.dropSource || '').trim();

  const qs = new URLSearchParams();
  if (zone) qs.set('zone', zone);
  if (source) qs.set('source', source);
  const q = qs.toString();

  const raw = await api('/api/drop-rates' + (q ? '?' + q : ''));
  // Backend contract: { items:[...], zones:[...] }; accept a bare array too
  // in case the new response shape hasn't shipped yet.
  const items = Array.isArray(raw) ? raw : (Array.isArray(raw?.items) ? raw.items : []);
  const allZones = Array.isArray(raw) ? [] : (Array.isArray(raw?.zones) ? raw.zones : []);
  renderDropZoneOptions(allZones, zone);

  if (!items.length) {
    container.innerHTML = '<div class="summary-empty">No session data yet. Complete sessions to see drop rates.</div>';
    $('#drop-count').textContent = '0 items';
    renderDropRateBanner(zone, source);
    return;
  }
  state.dropRates = items;
  const search = ($('#drop-search')?.value || '').toLowerCase();

  const filtered = search ? items.filter(d => d.name.toLowerCase().includes(search)) : items;
  filtered.sort((a, b) => (b.now_chance || 0) - (a.now_chance || 0));

  const latestKillMob = Array.isArray(state.session?.kills) && state.session.kills.length
    ? state.session.kills[state.session.kills.length - 1].mob
    : '';
  const dungeonCtx = String(state.session?.dungeon || '').trim();
  const cleanedDungeonCtx = ['unnamed', 'unknown', 'test dungeon'].includes(dungeonCtx.toLowerCase()) ? '' : dungeonCtx;
  const currentCtx = latestKillMob || cleanedDungeonCtx || 'current context';
  $('#drop-count').textContent = `${filtered.length} item${filtered.length !== 1 ? 's' : ''} (${items.length} unique) · chance context: ${currentCtx}`;

  let html = '<table style="width:100%;border-collapse:collapse"><thead><tr>' +
    '<th style="text-align:left;padding:6px 8px;border-bottom:1px solid var(--border)">Item</th>' +
    '<th style="padding:6px 8px;border-bottom:1px solid var(--border)">Drops From</th>' +
    '<th style="text-align:right;padding:6px 8px;border-bottom:1px solid var(--border)">Chance Now</th>' +
    '<th style="text-align:right;padding:6px 8px;border-bottom:1px solid var(--border)">Overall Chance</th>' +
    '</tr></thead><tbody>';
  for (const d of filtered.slice(0, 500)) {
    const src = (Array.isArray(d.sources) && d.sources.length > 0) ? d.sources[0] : null;
    const srcName = src ? src.name : (d.primary_source || '');
    const srcCell = srcName && srcName !== 'Unknown'
      ? `<button class="drop-source-link" data-source="${escapeHtml(srcName)}">${escapeHtml(srcName)}</button>${src ? ` (${(src.chance || 0).toFixed(1)}%)${dropConfidenceSuffix(src)}` : ''}`
      : 'Unknown';
    html += `<tr>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row)"><button class="drop-toggle" data-toggle="1" aria-expanded="false" title="Show drop details">▸</button> ${escapeHtml(d.name)}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row)">${srcCell}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row);text-align:right">${(d.now_chance || 0).toFixed(1)}%</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row);text-align:right">${(d.overall_chance || 0).toFixed(1)}%</td>
    </tr>
    <tr class="drop-detail"><td colspan="4">${dropDetailHtml(d)}</td></tr>`;
  }
  html += '</tbody></table>';
  if (filtered.length > 500) {
    html += `<div class="summary-empty">+ ${filtered.length - 500} more items (narrow your search)</div>`;
  }
  container.innerHTML = html;
  renderDropRateBanner(zone, source);
}

function dropDetailHtml(d) {
  const sources = Array.isArray(d.sources) ? d.sources : [];
  const zones = Array.isArray(d.zones) ? d.zones : [];
  if (sources.length === 0 && zones.length === 0) {
    return '<span class="drop-detail-count">no source data yet</span>';
  }
  let html = '';
  if (sources.length) {
    html += `<div class="drop-detail-block"><span class="drop-detail-label">Drops from</span> ` +
      sources.map(s => `<button class="drop-source-link" data-source="${escapeHtml(s.name)}">${escapeHtml(s.name)}</button> <span class="drop-detail-count">(${s.count || 0}, ${(s.chance || 0).toFixed(1)}%)</span>${dropConfidenceSuffix(s)}`).join(', ') +
      '</div>';
  }
  if (zones.length) {
    html += `<div class="drop-detail-block"><span class="drop-detail-label">Zones</span> ` +
      zones.map(z => `${escapeHtml(z.name)} <span class="drop-detail-count">(${z.count || 0}, ${(z.chance || 0).toFixed(1)}%)</span>${dropConfidenceSuffix(z)}`).join(', ') +
      '</div>';
  }
  return html;
}

function renderDropZoneOptions(zones, activeZone) {
  const sel = $('#drop-zone-filter');
  if (!sel) return;
  const cur = String(state.session?.zone || '').trim();
  const seen = new Set();
  if (cur) seen.add(cur.toLowerCase());
  let opts = '';
  if (cur) opts += `<option value="${escapeHtml(cur)}">Current zone: ${escapeHtml(zonePath(cur))}</option>`;
  opts += '<option value="">All zones</option>';
  for (const z of zones) {
    const n = String(z && z.name || '').trim();
    if (!n || seen.has(n.toLowerCase())) continue;
    seen.add(n.toLowerCase());
    opts += `<option value="${escapeHtml(n)}">${escapeHtml(zonePath(n))}</option>`;
  }
  sel.innerHTML = opts;
  sel.value = activeZone;
  if (sel.value !== activeZone) state.dropZone = ''; // active zone no longer offered
}

function renderDropRateBanner(zone, source) {
  const el = $('#drop-filter-banner');
  if (!el) return;
  let html = '';
  if (source) html += `<span class="drop-banner">Drops from: <strong>${escapeHtml(source)}</strong> <button class="drop-banner-x" data-clear="source" title="Clear filter">✕</button></span>`;
  if (zone) html += `<span class="drop-banner">Zone: ${escapeHtml(zonePath(zone))} <button class="drop-banner-x" data-clear="zone" title="Clear filter">✕</button></span>`;
  el.innerHTML = html;
}

// Drop search listener
$('#drop-search')?.addEventListener('input', () => {
  if (state.currentView === 'drop-rates') renderDropRatesView();
});

// Zone filter listener
$('#drop-zone-filter')?.addEventListener('change', (e) => {
  state.dropZone = e.target.value;
  if (state.currentView === 'drop-rates') renderDropRatesView();
});

// Delegated: row expand/collapse, source drill-down, filter-banner clear.
// Containers are re-rendered, so listeners live on them and match by data attr.
$('#drop-list')?.addEventListener('click', (e) => {
  const btn = e.target.closest('button');
  if (!btn) {
    // Overlay only: the whole row is the expand target (the ▸ is a tiny target
    // in a 460px window). Mirrors the toggle the button branch does below.
    if (!document.body.classList.contains('overlay-mode')) return;
    const row = e.target.closest('tr');
    const detail = row && row.nextElementSibling;
    if (detail && detail.classList.contains('drop-detail')) {
      const open = detail.classList.toggle('open');
      const t = row.querySelector('.drop-toggle');
      if (t) {
        t.textContent = open ? '▾' : '▸';
        t.setAttribute('aria-expanded', open ? 'true' : 'false');
      }
    }
    return;
  }
  if (btn.dataset.toggle) {
    const detail = btn.closest('tr')?.nextElementSibling;
    if (detail && detail.classList.contains('drop-detail')) {
      const open = detail.classList.toggle('open');
      btn.textContent = open ? '▾' : '▸';
      btn.setAttribute('aria-expanded', open ? 'true' : 'false');
    }
    return;
  }
  if (btn.dataset.source) {
    state.dropSource = btn.dataset.source;
    if (state.currentView === 'drop-rates') renderDropRatesView();
  }
});

$('#drop-filter-banner')?.addEventListener('click', (e) => {
  const btn = e.target.closest('button[data-clear]');
  if (!btn) return;
  if (btn.dataset.clear === 'zone') state.dropZone = '';
  else state.dropSource = '';
  if (state.currentView === 'drop-rates') renderDropRatesView();
});

// NPC Settings functions
async function loadNPCList() {
  const npcs = await api('/api/npcs');
  if (npcs) {
    state.npcs = npcs;
    if (state.currentView === 'favor') renderFavorView();
  }
}

// Loot table search/filter
$('#loot-search')?.addEventListener('input', () => {
  if (state.session) renderLootTable(state.session);
});

$('#loot-filter')?.addEventListener('change', () => {
  if (state.session) renderLootTable(state.session);
});

$('#route-session-select')?.addEventListener('change', () => {
  __routePlanHighlight = null;
  renderRoutePlan();
});

// Manual loot entry
$('#manual-loot-add')?.addEventListener('click', async () => {
  const name = $('#manual-loot-name').value.trim();
  if (!name) { toast('Enter an item name', 'error'); return; }
  const value = parseFloat($('#manual-loot-value').value) || 0;
  const count = parseInt($('#manual-loot-count').value) || 1;
  const res = await api('/api/loot', 'POST', { name, value, count });
  if (res) {
    toast(`Added "${name}" x${count}`, 'success');
    $('#manual-loot-name').value = '';
    $('#manual-loot-value').value = '';
    $('#manual-loot-count').value = '1';
    refreshAll();
  }
});

// Open the native always-on-top HUD overlay (spawned as a detached process).
// The browser URL is only a fallback for machines that can't run the native window.
window.openOverlay = async function() {
  const res = await api('/api/overlay/spawn', 'POST');
  if (res && res.ok) toast('Overlay opened', 'info');
  else if (res && res.error) toast(res.error, 'error');
};

// Overlay-window controls (bar is hidden outside overlay mode). The native
// host binds overlayToggleClickThrough/overlayClose via WebView2; in a plain
// browser tab these are no-ops. See-through has no bar button — toggle via
// Ctrl+F9 (native) or the 'o' key (below).
let __overlaySeeThrough = false;
window.toggleOverlaySeeThrough = function() {
  __overlaySeeThrough = !__overlaySeeThrough;
  document.body.classList.toggle('clickthrough', __overlaySeeThrough);
  window.overlayToggleClickThrough?.(__overlaySeeThrough);
};
$('#overlay-close')?.addEventListener('click', () => { window.overlayClose?.(); });
// Drag: mousedown on the top or bottom menu bar starts a native window move,
// unless the click lands on a button, link, input, or the resize grip (those
// keep working normally). No-op in a plain browser tab.
function makeDragListener(el) {
  el?.addEventListener('mousedown', e => {
    if (e.button !== 0) return;
    const interactive = e.target.closest('button, a, input, select, textarea, #overlay-resize-grip');
    if (interactive) return;
    e.preventDefault(); // no text selection while dragging
    window.overlayStartDrag?.();
  });
}
makeDragListener($('#overlay-bar'));
makeDragListener($('#overlay-bottom'));
// Resize grip (bottom-right corner): drag resizes the native window. The host
// binds overlayResize via WebView2; no-op in a plain browser tab. Minimums
// (320x400) are enforced natively too — clamping here just avoids useless calls.
$('#overlay-resize-grip')?.addEventListener('mousedown', e => {
  if (e.button !== 0) return;
  e.preventDefault(); // no text selection while resizing
  const startX = e.clientX, startY = e.clientY;
  const startW = window.innerWidth, startH = window.innerHeight;
  const onMove = ev => {
    window.overlayResize?.(
      Math.max(320, startW + (ev.clientX - startX)),
      Math.max(400, startH + (ev.clientY - startY))
    );
  };
  const onUp = () => {
    document.removeEventListener('mousemove', onMove);
    document.removeEventListener('mouseup', onUp);
  };
  document.addEventListener('mousemove', onMove);
  document.addEventListener('mouseup', onUp);
});
// Overlay brand = Home button: same view switch the nav items use (no active state needed).
$('#overlay-home')?.addEventListener('click', () => switchView('dashboard'));

// Keyboard shortcuts
document.addEventListener('keydown', e => {
  if (e.ctrlKey && e.key === 'Enter') {
    e.preventDefault();
    const btn = state.session?.state === 'running' ? $('#stop') : $('#start');
    if (btn && !btn.disabled) btn.click();
  }
  if (e.key === '/' && !['INPUT', 'TEXTAREA', 'SELECT'].includes(e.target.tagName)) {
    e.preventDefault();
    const search = state.currentView === 'tracker' ? $('#loot-search') : $('#favor-search');
    if (search) search.focus();
  }
  if (e.key === 'o' && !['INPUT', 'TEXTAREA', 'SELECT'].includes(e.target.tagName)) {
    e.preventDefault();
    if (document.body.classList.contains('overlay-mode')) {
      toggleOverlaySeeThrough(); // in the overlay, 'o' toggles see-through instead of spawning another window
    } else {
      window.openOverlay();
    }
  }
  if (e.key === 'r' && !['INPUT', 'TEXTAREA', 'SELECT'].includes(e.target.tagName)) {
    e.preventDefault();
    refreshAll();
    toast('Refreshed', 'info');
  }
});

// Refresh button
$('#refresh-btn')?.addEventListener('click', () => { refreshAll(); toast('Refreshed', 'info'); });

// ── Shared item list renderers (used by summary.js & history.js) ──

window.sharedRenderFavorList = function(container, items) {
  container.innerHTML = '';
  if (!items.length) { container.innerHTML = '<div class="summary-empty">no favor items</div>'; return; }

  const byNPC = new Map();
  for (const e of items) {
    const targets = (e.decision.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
    if (!targets.length) {
      if (!byNPC.has('Disabled/Unknown')) byNPC.set('Disabled/Unknown', []);
      byNPC.get('Disabled/Unknown').push(e);
      continue;
    }
    const primary = targets[0];
    const key = `${primary.npc} (${primary.area})`;
    if (!byNPC.has(key)) byNPC.set(key, []);
    byNPC.get(key).push(e);
  }

  const sorted = [...byNPC.entries()].sort((a, b) => {
    const aPri = state.prioritizedNPCs.has(a[0].split(' (')[0]) ? 0 : 1;
    const bPri = state.prioritizedNPCs.has(b[0].split(' (')[0]) ? 0 : 1;
    if (aPri !== bPri) return aPri - bPri;
    return b[1].reduce((s, e) => s + ((e.decision.favor_targets||[]).filter(x=>!state.disabledNPCs.has(x.npc))[0]?.score||0) * e.count, 0)
         - a[1].reduce((s, e) => s + ((e.decision.favor_targets||[]).filter(x=>!state.disabledNPCs.has(x.npc))[0]?.score||0) * e.count, 0);
  });

  for (const [npc, entries] of sorted) {
    const group = document.createElement('div');
    group.className = 'summary-group';
    const totalFavor = entries.reduce((sum, e) => {
      const t = (e.decision.favor_targets||[]).filter(x => !state.disabledNPCs.has(x.npc));
      return sum + (t[0]?.score||0) * e.count;
    }, 0);
    const npcName = npc.split(' (')[0];
    const cap = state.traderCapacity[npcName];
    const broke = cap && cap.remaining <= 0 && cap.limit > 0;
    const isPri = state.prioritizedNPCs.has(npcName);

    group.innerHTML = `<div class="summary-group-header npc">
      <button class="pri-btn${isPri?' active':''}" onclick="togglePrioritizeNPC('${escapeHtml(npcName).replace(/'/g, "\\'")}')" title="Prioritize">★</button>
      ${escapeHtml(npc)} <span style="float:right;color:var(--muted);font-weight:normal">${entries.length} items · ${totalFavor.toFixed(1)} favor${broke?' · <span style="color:#e74c3c">⚠ no gold left ('+cap.reset+')</span>':''}</span>
    </div>`;
    for (const e of entries) {
      const t = (e.decision.favor_targets||[]).filter(x => !state.disabledNPCs.has(x.npc));
      const score = t[0]?.score||0;
      const item = document.createElement('div');
      item.className = 'summary-item';
      item.innerHTML = `<span class="summary-item-name">${escapeHtml(e.name)}</span><span class="summary-item-count">x${e.count}</span><span class="summary-item-value">+${score.toFixed(1)} favor</span>`;
      group.appendChild(item);
    }
    container.appendChild(group);
  }
};

window.sharedRenderSellList = function(container, items, showSellReason) {
  container.innerHTML = '';
  if (!items.length) { container.innerHTML = '<div class="summary-empty">no sell items</div>'; return; }

  const vendors = items.filter(e => e.decision.verdict === 'sell_vendor');
  const consignment = items.filter(e => e.decision.verdict === 'sell_consignment');

  if (vendors.length) {
    const group = document.createElement('div');
    group.className = 'summary-group';
    const totalValue = vendors.reduce((sum, e) => sum + (e.value||0) * e.count, 0);
    group.innerHTML = `<div class="summary-group-header vendor">any vendor <span style="float:right;color:var(--muted);font-weight:normal">${vendors.length} items · ${totalValue.toFixed(0)}g</span></div>`;
    for (const e of vendors) {
      const item = document.createElement('div');
      item.className = 'summary-item';
      const extra = showSellReason && e.decision.sell_reason ? `<br><span style="color:var(--muted);font-size:11px">${escapeHtml(e.decision.sell_reason)}</span>` : '';
      item.innerHTML = `<span class="summary-item-name">${escapeHtml(e.name)}${extra}</span><span class="summary-item-count">x${e.count}</span><span class="summary-item-value">${(e.value||0).toFixed(0)}g</span>`;
      group.appendChild(item);
    }
    container.appendChild(group);
  }

  if (consignment.length) {
    const group = document.createElement('div');
    group.className = 'summary-group';
    const totalValue = consignment.reduce((sum, e) => sum + (e.value||0) * e.count, 0);
    group.innerHTML = `<div class="summary-group-header consignment">consignment NPC <span style="float:right;color:var(--muted);font-weight:normal">${consignment.length} items · ${totalValue.toFixed(0)}g</span></div>`;
    for (const e of consignment) {
      const item = document.createElement('div');
      item.className = 'summary-item';
      const extra = showSellReason && e.decision.sell_reason ? `<br><span style="color:var(--muted);font-size:11px">${escapeHtml(e.decision.sell_reason)}</span>` : '';
      item.innerHTML = `<span class="summary-item-name">${escapeHtml(e.name)}${extra}</span><span class="summary-item-count">x${e.count}</span><span class="summary-item-value">${(e.value||0).toFixed(0)}g</span>`;
      group.appendChild(item);
    }
    container.appendChild(group);
  }
};

window.sharedRenderKeepList = function(container, items) {
  container.innerHTML = '';
  if (!items.length) { container.innerHTML = '<div class="summary-empty">no keep items</div>'; return; }

  const totalValue = items.reduce((sum, e) => sum + (e.value||0) * e.count, 0);
  const group = document.createElement('div');
  group.className = 'summary-group';
  group.innerHTML = `<div class="summary-group-header" style="color:var(--keep)">manual decision <span style="float:right;color:var(--muted);font-weight:normal">${items.length} items · ${totalValue.toFixed(0)}g</span></div>`;
  for (const e of items) {
    const item = document.createElement('div');
    item.className = 'summary-item';
    item.innerHTML = `<span class="summary-item-name">${escapeHtml(e.name)}</span><span class="summary-item-count">x${e.count}</span><span class="summary-item-value">${(e.value||0).toFixed(0)}g</span>`;
    group.appendChild(item);
  }
  container.appendChild(group);
};
