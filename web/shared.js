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
  combatData: null,
  zoneNpcs: [],
  dropRates: null
};

// Shared request state (must be initialized before any startup API calls)
var __inflightGet = new Map();
const tbody = $('#loot tbody');
const stateEl = $('#state');
const countEl = $('#count');
const emptyEl = $('#empty');
const elapsedEl = $('#elapsed');

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
        const dMatch = dur.match(/(\d+)d/);
        const hMatch = dur.match(/(\d+)h/);
        const days = dMatch ? parseInt(dMatch[1]) : 99;
        cap[npc.npc_name] = {
          limit: npc.weekly_limit || 0,
          sold: npc.sold_this_week || 0,
          remaining: Math.max(0, (npc.weekly_limit || 0) - (npc.sold_this_week || 0)),
          reset: dur,
          daysUntilReset: days,
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
      return await r.json();
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
  $('#view-combat').classList.toggle('hidden', view !== 'combat');
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
    combat: 'Combat Log',
    'drop-rates': 'Drop Rates',
    settings: 'Settings'
  };
  $('#view-title').innerHTML = `${titles[view]} <small id="state">${state.session?.state || 'idle'}</small>`;

  if (view === 'dashboard') renderDashboard();
  if (view === 'summary' && state.session) renderSummary(state.session);
  if (view === 'history') renderHistory();
  if (view === 'favor') renderFavorView();
  if (view === 'traders') { loadTraderCapacity().then(() => renderTradersView()); }
  if (view === 'items') renderItemsView();
  if (view === 'warcache') renderWarcacheView();
  if (view === 'skills') renderSkillsView();
  if (view === 'recipes') renderRecipesView();
  if (view === 'combat') renderCombatView();
  if (view === 'drop-rates') renderDropRatesView();
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

  renderLootTable(s);
  renderTrackerNotes(s);
  renderSessionEvents(s);
  // Show start-notes input only when idle
  const startNotes = $('#tracker-start-notes');
  if (startNotes) startNotes.style.display = s.state === 'idle' ? '' : 'none';

  if (state.currentView === 'summary') {
    renderSummary(s);
  }
  if (state.currentView === 'dashboard') {
    renderDashboard();
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
  const iconHtml = e.icon_url ? `<img src="${e.icon_url}" alt="" class="item-icon" onerror="this.style.display='none'">` : '';
  tr.innerHTML = `
    <td class="time">${relTime(e.last_seen)}</td>
    <td>${iconHtml}${escapeHtml(e.name)}</td>
    <td class="count">${e.count}</td>
    <td><span class="verdict ${e.decision.verdict}">${e.decision.verdict.replace(/_/g, ' ')}</span></td>
    <td class="route">${routeText(e.decision)}</td>
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
  const recipes = state.recipes;
  if (!recipes) {
    container.innerHTML = '<div class="summary-empty">Loading recipes...</div>';
    return;
  }

  const search = ($('#recipes-search')?.value || '').toLowerCase();
  const skillFilter = $('#recipes-skill-filter')?.value || '';

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

$('#recipes-search')?.addEventListener('input', () => {
  if (state.currentView === 'recipes') renderRecipesView();
});
$('#recipes-skill-filter')?.addEventListener('change', () => {
  if (state.currentView === 'recipes') renderRecipesView();
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
    const ph = await api('/api/prices');
    if (ph) state.priceHistory = ph;
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
    { type: 'combat-stats', title: '⚔ Combat', size: 'half', visible: true },
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
    .sort((a, b) => a[1].daysUntilReset - b[1].daysUntilReset)
    .slice(0, 10);
  if (sorted.length === 0) return '<div class="dash-info-box"><span class="muted">No trader data</span></div>';
  let html = '<div class="dash-recent-list">';
  for (const [name, cap] of sorted) {
    const hasCap = cap.limit > 0;
    const color = cap.daysUntilReset <= 1 ? 'var(--danger)' : cap.daysUntilReset <= 3 ? 'var(--sell-vendor)' : 'var(--muted)';
    html += `<div class="dash-recent-item" style="cursor:default">
      <div><div class="dash-item-dungeon">${escapeHtml(name)}</div>
      <div class="dash-item-meta">${escapeHtml(cap.area)}</div></div>
      <div class="dash-item-value" style="color:${color}">${escapeHtml(cap.reset)}${hasCap ? ' · ' + Math.round(cap.remaining).toLocaleString() + 'g' : ''}</div>
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

widgetRenderers['combat-stats'] = async function(w, sessions) {
  const s = state.session;
  if (!s || s.state !== 'running') return '<div class="dash-info-box"><span class="muted">Session must be running</span></div>';
  try {
    const data = await api('/api/combat');
    if (!data || data.length === 0) return '<div class="dash-info-box"><span class="muted">No combat data yet</span></div>';
    const totalDPS = data.reduce((a, b) => a + (b.est_dps || 0), 0);
    const totalUses = data.reduce((a, b) => a + b.uses, 0);
    const totalHits = data.reduce((a, b) => a + (b.hits || 0), 0);
    const topAbils = data.sort((a, b) => (b.est_dps || 0) - (a.est_dps || 0)).slice(0, 5);
    let html = `<div class="dashboard-stats" style="margin-bottom:8px">
      <div class="stat-card"><div class="stat-label">Est. DPS</div><div class="stat-value">${totalDPS.toFixed(1)}</div></div>
      <div class="stat-card"><div class="stat-label">Abilities</div><div class="stat-value">${totalUses}</div></div>
      <div class="stat-card"><div class="stat-label">Hits</div><div class="stat-value">${totalHits}</div></div>
    </div><div style="font-size:11px;color:var(--muted);margin-bottom:4px">Top by DPS</div>`;
    for (const a of topAbils) {
      html += `<div style="display:flex;justify-content:space-between;padding:3px 8px;font-size:12px;border-bottom:1px solid var(--border)">
        <span>${escapeHtml(a.name)}</span><span style="color:var(--accent)">${(a.est_dps || 0).toFixed(1)} dps</span></div>`;
    }
    return html;
  } catch { return '<div class="dash-info-box"><span class="muted">Combat data unavailable</span></div>'; }
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
  canvas.width = rect.width * dpr;
  canvas.height = rect.height * dpr;
  ctx.scale(dpr, dpr);
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

// Combat view — ability usage + estimated DPS
async function renderCombatView() {
  const container = $('#combat-list');
  if (!container) return;
  const s = state.session;
  if (!s || !s.ability_uses || s.ability_uses.length === 0) {
    container.innerHTML = '<div class="summary-empty">Start a session and use abilities to see combat data here.</div>';
    $('#combat-count').textContent = 'No combat data';
    return;
  }
  const data = await api('/api/combat');
  if (!data) { container.innerHTML = '<div class="summary-empty">Combat endpoint unavailable.</div>'; return; }
  state.combatData = data;
  $('#combat-count').textContent = `${data.length} ability type${data.length !== 1 ? 's' : ''}`;

  // DPS stat cards
  const totalDPS = data.reduce((s, a) => s + (a.est_dps || 0), 0);
  const totalUses = data.reduce((s, a) => s + a.uses, 0);
  const totalHits = data.reduce((s, a) => s + (a.hits || 0), 0);
  const statsEl = $('#combat-stats');
  if (statsEl) {
    statsEl.innerHTML = `
      <div style="flex:1;min-width:120px;background:var(--card-bg);border:1px solid var(--border);border-radius:8px;padding:10px 14px;text-align:center">
        <div style="font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px">Abilities Used</div>
        <div style="font-size:22px;font-weight:700;color:var(--text);margin-top:2px">${totalUses}</div>
      </div>
      <div style="flex:1;min-width:120px;background:var(--card-bg);border:1px solid var(--border);border-radius:8px;padding:10px 14px;text-align:center">
        <div style="font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px">Outgoing Hits</div>
        <div style="font-size:22px;font-weight:700;color:var(--text);margin-top:2px">${totalHits}</div>
      </div>
      <div style="flex:1;min-width:120px;background:var(--card-bg);border:1px solid var(--border);border-radius:8px;padding:10px 14px;text-align:center">
        <div style="font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.5px">Est. DPS</div>
        <div style="font-size:22px;font-weight:700;color:var(--text);margin-top:2px">${totalDPS.toFixed(1)}</div>
      </div>`;
  }

  // Per-ability table
  let html = '<table style="width:100%;border-collapse:collapse"><thead><tr>' +
    '<th style="text-align:left;padding:6px 8px;border-bottom:1px solid var(--border)">Ability</th>' +
    '<th style="text-align:right;padding:6px 8px;border-bottom:1px solid var(--border)">Uses</th>' +
    '<th style="text-align:right;padding:6px 8px;border-bottom:1px solid var(--border)">Hits</th>' +
    '<th style="text-align:right;padding:6px 8px;border-bottom:1px solid var(--border)">Base DMG</th>' +
    '<th style="text-align:right;padding:6px 8px;border-bottom:1px solid var(--border)">Est DPS</th>' +
    '<th style="padding:6px 8px;border-bottom:1px solid var(--border)">Type</th>' +
    '</tr></thead><tbody>';
  for (const a of data) {
    html += `<tr>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row)">${escapeHtml(a.name)}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row);text-align:right">${a.uses}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row);text-align:right">${a.hits || '-'}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row);text-align:right">${a.base_damage ? a.base_damage.toFixed(0) : '-'}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row);text-align:right">${a.est_dps ? a.est_dps.toFixed(1) : '-'}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row)">${a.skill || a.damage_type ? escapeHtml(a.skill || '') + (a.damage_type ? ' · ' + escapeHtml(a.damage_type) : '') : ''}</td>
    </tr>`;
  }
  html += '</tbody></table>';
  container.innerHTML = html;
}

// Zones view — show zone history from session snapshot
// Drop Rates view — aggregated across sessions
async function renderDropRatesView() {
  const container = $('#drop-list');
  if (!container) return;
  container.innerHTML = '<div class="summary-empty">Loading drop rates...</div>';
  const data = await api('/api/drop-rates');
  if (!data || data.length === 0) {
    container.innerHTML = '<div class="summary-empty">No session data yet. Complete sessions to see drop rates.</div>';
    $('#drop-count').textContent = '0 items';
    return;
  }
  state.dropRates = data;
  const search = ($('#drop-search')?.value || '').toLowerCase();

  const filtered = search ? data.filter(d => d.name.toLowerCase().includes(search)) : data;
  filtered.sort((a, b) => b.total_count - a.total_count);
  $('#drop-count').textContent = `${filtered.length} item${filtered.length !== 1 ? 's' : ''} (${data.length} unique)`;

  let html = '<table style="width:100%;border-collapse:collapse"><thead><tr>' +
    '<th style="text-align:left;padding:6px 8px;border-bottom:1px solid var(--border)">Item</th>' +
    '<th style="text-align:right;padding:6px 8px;border-bottom:1px solid var(--border)">Total</th>' +
    '<th style="text-align:right;padding:6px 8px;border-bottom:1px solid var(--border)">Sessions</th>' +
    '<th style="text-align:right;padding:6px 8px;border-bottom:1px solid var(--border)">Avg/Session</th>' +
    '<th style="text-align:right;padding:6px 8px;border-bottom:1px solid var(--border)">Avg Value</th>' +
    '</tr></thead><tbody>';
  for (const d of filtered.slice(0, 500)) {
    html += `<tr>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row)">${escapeHtml(d.name)}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row);text-align:right">${d.total_count}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row);text-align:right">${d.session_count}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row);text-align:right">${d.avg_per_session.toFixed(1)}</td>
      <td style="padding:4px 8px;border-bottom:1px solid var(--row);text-align:right">${d.avg_value ? d.avg_value.toFixed(0) + 'g' : '-'}</td>
    </tr>`;
  }
  html += '</tbody></table>';
  if (filtered.length > 500) {
    html += `<div class="summary-empty">+ ${filtered.length - 500} more items (narrow your search)</div>`;
  }
  container.innerHTML = html;
}

// Drop search listener
$('#drop-search')?.addEventListener('input', () => {
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

// Open overlay in a lightweight popout window (no OBS/extensions needed)
window.openOverlay = function() {
  const w = 380, h = 600;
  const left = screen.width - w - 20;
  const top = 80;
  window.open('/overlay', 'gorgon-overlay',
    `width=${w},height=${h},left=${left},top=${top},toolbar=no,location=no,status=no,menubar=no,scrollbars=no,resizable=yes`);
};

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
    window.openOverlay();
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
