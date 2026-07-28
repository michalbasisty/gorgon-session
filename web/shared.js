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
  currentView: 'tracker', 
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
  notificationThreshold: 500
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
  $('#view-tracker').classList.toggle('hidden', view !== 'tracker');
  $('#view-summary').classList.toggle('hidden', view !== 'summary');
  $('#view-history').classList.toggle('hidden', view !== 'history');
  $('#view-history-detail').classList.toggle('hidden', view !== 'history-detail');
  $('#view-favor').classList.toggle('hidden', view !== 'favor');
  $('#view-traders').classList.toggle('hidden', view !== 'traders');
  $('#view-warcache').classList.toggle('hidden', view !== 'warcache');
  $('#view-settings').classList.toggle('hidden', view !== 'settings');

  const titles = { 
    tracker: 'Tracker', 
    summary: 'Summary', 
    history: 'History',
    'history-detail': 'Session Details',
    favor: 'Favor Progress',
    traders: 'Shops & Traders',
    warcache: 'Warcache Solver',
    settings: 'Settings'
  };
  $('#view-title').innerHTML = `${titles[view]} <small id="state">${state.session?.state || 'idle'}</small>`;

  if (view === 'summary' && state.session) renderSummary(state.session);
  if (view === 'history') renderHistory();
  if (view === 'favor') renderFavorView();
  if (view === 'traders') { loadTraderCapacity().then(() => renderTradersView()); }
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

  if (state.currentView === 'summary') {
    renderSummary(s);
  }
}

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
  if (d.verdict === 'favor') {
    let targets = (d.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
    if (!targets.length) return 'no available NPC';
    // Sort: prioritized NPCs first
    targets = [...targets].sort((a, b) => {
      const aPri = state.prioritizedNPCs.has(a.npc) ? 0 : 1;
      const bPri = state.prioritizedNPCs.has(b.npc) ? 0 : 1;
      return aPri - bPri || b.score - a.score;
    });
    return targets.map(t => {
      const cap = state.traderCapacity[t.npc];
      const broke = cap && cap.remaining <= 0 && cap.limit > 0;
      const pri = state.prioritizedNPCs.has(t.npc) ? '★ ' : '';
      return `${pri}${t.npc} (${t.area}) +${t.score}${broke ? ' ⚠ 0g' : ''}`;
    }).join(' · ') || 'gift';
  }
  if (d.player_price) {
    return `player price: ${d.player_price.toFixed(0)}g`;
  }
  return d.sell_reason || '';
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
  if (data.kind === 'loot' || data.kind === 'session_start' || data.kind === 'session_stop') {
    refreshAll();
    
    // Check for rare loot notification
    if (data.kind === 'loot' && data.payload) {
      const loot = data.payload;
      if (loot.value >= state.notificationThreshold) {
        showRareLootNotification(loot);
      }
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

async function refreshAll() {
  const s = await api('/api/session');
  renderSession(s);
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
