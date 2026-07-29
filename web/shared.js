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
  priceHistory: {} // item name → { average, last, count }
};
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
  const areas = await api('/api/traders');
  if (!areas) return;
  const cap = {};
  for (const area of areas) {
    for (const npc of area.npcs) {
      cap[npc.npc_name] = {
        limit: npc.weekly_limit || 0,
        sold: npc.sold_this_week || 0,
        remaining: Math.max(0, (npc.weekly_limit || 0) - (npc.sold_this_week || 0)),
        reset: npc.time_until_reset || ''
      };
    }
  }
  state.traderCapacity = cap;
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
  const r = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!r.ok) {
    toast(`${path}: ${await r.text()}`, 'error');
    return null;
  }
  return r.json();
}

// Navigation
$$('.nav-item').forEach(item => {
  item.addEventListener('click', () => {
    const view = item.dataset.view;
    switchView(view);
  });
});

function switchView(view) {
  state.currentView = view;
  $$('.nav-item').forEach(n => n.classList.toggle('active', n.dataset.view === view));
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

function zonePath(zoneName) {
  if (!state.areas || !zoneName) return zoneName;
  // Find area by lowercase name match
  const key = zoneName.toLowerCase();
  let area = null;
  for (const id of Object.keys(state.areas)) {
    if (state.areas[id].name.toLowerCase() === key) { area = state.areas[id]; break; }
  }
  if (!area) return zoneName;
  // Build parent chain (Parent=0 means root)
  let parts = [area.name];
  let parentId = area.parent;
  let guard = 0;
  while (parentId && parentId > 0 && guard < 10) {
    const p = state.areas[parentId];
    if (!p) break;
    parts.unshift(p.name);
    parentId = p.parent;
    guard++;
  }
  return parts.join(' › ');
}

function renderSessionEvents(s) {
  const el = $('#tracker-events');
  if (!el) return;
  if (s.state !== 'running') { el.innerHTML = ''; return; }

  const parts = [];
  if (s.zone) parts.push(`📍 ${escapeHtml(zonePath(s.zone))}`);
  const xp = s.xp_gains || [];
  if (xp.length) {
    // Group by skill, sum amounts
    const bySkill = {};
    for (const g of xp) { bySkill[g.skill] = (bySkill[g.skill] || 0) + g.amount; }
    const skills = Object.entries(bySkill).sort((a, b) => b[1] - a[1]).slice(0, 5);
    parts.push(`XP: ${skills.map(([sk, am]) => `${sk}+${am}`).join(', ')}${skills.length < Object.keys(bySkill).length ? '…' : ''}`);
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

  el.innerHTML = parts.length
    ? `<div class="tracker-events-bar">${parts.join(' &middot; ')}</div>`
    : '';
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
    <td><button class="loot-del-btn" onclick="deleteLootItem('${escapeHtml(e.name).replace(/'/g, "\\'")}')">×</button></td>`;
  tbody.insertBefore(tr, tbody.firstChild);
}

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

// SSE feed
const es = new EventSource('/api/feed');
es.onmessage = ev => {
  const data = JSON.parse(ev.data);
  refreshAll();

  // Rare loot notification
  if (data.kind === 'loot' && data.payload) {
    const loot = data.payload;
    if (loot.value >= state.notificationThreshold) {
      showRareLootNotification(loot);
    }
  }
};
es.onerror = () => { };

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
  const search = ($('#items-search')?.value || '').toLowerCase();
  if (!itemsCache) {
    container.innerHTML = '<div class="summary-empty">Loading items...</div>';
    itemsCache = await api('/api/items');
    if (!itemsCache) { container.innerHTML = '<div class="summary-empty">Failed to load items</div>'; return; }
  }

  const filtered = search ? itemsCache.filter(i => (i.Name || '').toLowerCase().includes(search) || (i.Keywords || []).some(k => k.toLowerCase().includes(search))) : itemsCache;
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
  ];
}
function loadDashLayout() {
  try { return JSON.parse(localStorage.getItem('dashLayout') || 'null') || defaultDashLayout(); }
  catch { return defaultDashLayout(); }
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
    body.innerHTML = fn(widget, sessions);

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
});
