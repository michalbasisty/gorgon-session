const $ = s => document.querySelector(s);
const $$ = s => Array.from(document.querySelectorAll(s));

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
  } catch (e) {
    state.disabledNPCs = new Set();
    state.shopNPCs = [];
    state.craftingRecipes = [];
    state.favorProgress = new Set();
    state.playerPrices = {};
  }
}

function saveSettings() {
  localStorage.setItem('disabledNPCs', JSON.stringify([...state.disabledNPCs]));
  localStorage.setItem('shopNPCs', JSON.stringify(state.shopNPCs));
  localStorage.setItem('craftingRecipes', JSON.stringify(state.craftingRecipes));
  localStorage.setItem('favorProgress', JSON.stringify([...state.favorProgress]));
  localStorage.setItem('playerPrices', JSON.stringify(state.playerPrices));
}

loadSettings();

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
  $('#view-shopnpc').classList.toggle('hidden', view !== 'shopnpc');
  $('#view-traders').classList.toggle('hidden', view !== 'traders');
  $('#view-warcache').classList.toggle('hidden', view !== 'warcache');
  $('#view-settings').classList.toggle('hidden', view !== 'settings');

  const titles = { 
    tracker: 'Tracker', 
    summary: 'Summary', 
    history: 'History',
    'history-detail': 'Session Details',
    favor: 'Favor Progress',
    shopnpc: 'Shop NPC',
    traders: 'Traders',
    warcache: 'Warcache Solver',
    settings: 'Settings'
  };
  $('#view-title').innerHTML = `${titles[view]} <small id="state">${state.session?.state || 'idle'}</small>`;

  if (view === 'summary' && state.session) renderSummary(state.session);
  if (view === 'history') renderHistory();
  if (view === 'favor') renderFavorView();
  if (view === 'shopnpc') renderShopNPCList();
  if (view === 'traders') renderTradersView();
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
    <td class="route">${routeText(e.decision)}</td>`;
  tbody.insertBefore(tr, tbody.firstChild);
}
function routeText(d) {
  if (d.verdict === 'favor') {
    const targets = (d.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
    if (!targets.length) return 'no available NPC';
    return targets.map(t => `${t.npc} (${t.area}) +${t.score}`).join(' · ') || 'gift';
  }
  if (d.player_price) {
    return `player price: ${d.player_price.toFixed(0)}g`;
  }
  return d.sell_reason || '';
}
function escapeHtml(s) {
  return String(s == null ? '' : s).replace(/[&<>"]/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}

function renderSummary(s) {
  const loot = s.loot || [];
  console.log('Summary loot data:', loot);
  const favorItems = loot.filter(e => e.decision.verdict === 'favor');
  const sellItems = loot.filter(e => e.decision.verdict === 'sell_vendor' || e.decision.verdict === 'sell_consignment');
  const keepItems = loot.filter(e => e.decision.verdict === 'keep');

  $('#sum-dungeon').textContent = s.dungeon || 'unnamed';
  const dur = new Date(s.ended_at).getTime() - new Date(s.started_at).getTime();
  $('#sum-duration').textContent = fmtElapsed(dur);
  $('#sum-items').textContent = `${loot.length} unique items`;

  $('#favor-count').textContent = favorItems.length;
  $('#sell-count').textContent = sellItems.length;
  $('#keep-count').textContent = keepItems.length;

  if (state.summarySortMode === 'npc') {
    $('#summary-npc-view').classList.remove('hidden');
    $('#summary-map-view').classList.add('hidden');
    renderFavorList(favorItems);
    renderSellList(sellItems);
    renderKeepList(keepItems);
  } else {
    $('#summary-npc-view').classList.add('hidden');
    $('#summary-map-view').classList.remove('hidden');
    renderMapList(loot);
  }
}

// Sort toggle buttons
$('#sort-npc').addEventListener('click', () => {
  state.summarySortMode = 'npc';
  $('#sort-npc').classList.add('active');
  $('#sort-map').classList.remove('active');
  if (state.session) renderSummary(state.session);
});

$('#sort-map').addEventListener('click', () => {
  state.summarySortMode = 'map';
  $('#sort-map').classList.add('active');
  $('#sort-npc').classList.remove('active');
  if (state.session) renderSummary(state.session);
});

function renderFavorList(items) {
  const container = $('#favor-list');
  container.innerHTML = '';

  if (!items.length) {
    container.innerHTML = '<div class="summary-empty">no favor items</div>';
    return;
  }

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

  for (const [npc, entries] of byNPC) {
    const group = document.createElement('div');
    group.className = 'summary-group';

    const totalFavor = entries.reduce((sum, e) => {
      const targets = (e.decision.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
      const score = targets.length > 0 ? targets[0].score : 0;
      return sum + score * e.count;
    }, 0);
    group.innerHTML = `<div class="summary-group-header npc">${escapeHtml(npc)} <span style="float:right;color:var(--muted);font-weight:normal">${entries.length} items · ${totalFavor.toFixed(1)} favor</span></div>`;

    for (const e of entries) {
      const targets = (e.decision.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
      const score = targets.length > 0 ? targets[0].score : 0;
      const item = document.createElement('div');
      item.className = 'summary-item';
      item.innerHTML = `
        <span class="summary-item-name">${escapeHtml(e.name)}</span>
        <span class="summary-item-count">x${e.count}</span>
        <span class="summary-item-value">+${score.toFixed(1)} favor</span>`;
      group.appendChild(item);
    }
    container.appendChild(group);
  }
}

function renderSellList(items) {
  const container = $('#sell-list');
  container.innerHTML = '';

  if (!items.length) {
    container.innerHTML = '<div class="summary-empty">no sell items</div>';
    return;
  }

  const vendors = items.filter(e => e.decision.verdict === 'sell_vendor');
  const consignment = items.filter(e => e.decision.verdict === 'sell_consignment');

  if (vendors.length) {
    const group = document.createElement('div');
    group.className = 'summary-group';
    const totalValue = vendors.reduce((sum, e) => sum + (e.value || 0) * e.count, 0);
    group.innerHTML = `<div class="summary-group-header vendor">any vendor <span style="float:right;color:var(--muted);font-weight:normal">${vendors.length} items · ${totalValue.toFixed(0)}g</span></div>`;
    for (const e of vendors) {
      const item = document.createElement('div');
      item.className = 'summary-item';
      item.innerHTML = `
        <span class="summary-item-name">${escapeHtml(e.name)}</span>
        <span class="summary-item-count">x${e.count}</span>
        <span class="summary-item-value">${(e.value || 0).toFixed(0)}g</span>`;
      group.appendChild(item);
    }
    container.appendChild(group);
  }

  if (consignment.length) {
    const group = document.createElement('div');
    group.className = 'summary-group';
    const totalValue = consignment.reduce((sum, e) => sum + (e.value || 0) * e.count, 0);
    group.innerHTML = `<div class="summary-group-header consignment">consignment NPC <span style="float:right;color:var(--muted);font-weight:normal">${consignment.length} items · ${totalValue.toFixed(0)}g</span></div>`;
    for (const e of consignment) {
      const item = document.createElement('div');
      item.className = 'summary-item';
      item.innerHTML = `
        <span class="summary-item-name">${escapeHtml(e.name)}</span>
        <span class="summary-item-count">x${e.count}</span>
        <span class="summary-item-value">${(e.value || 0).toFixed(0)}g</span>`;
      group.appendChild(item);
    }
    container.appendChild(group);
  }
}

function renderKeepList(items) {
  const container = $('#keep-list');
  container.innerHTML = '';

  if (!items.length) {
    container.innerHTML = '<div class="summary-empty">no keep items</div>';
    return;
  }

  const totalValue = items.reduce((sum, e) => sum + (e.value || 0) * e.count, 0);
  const group = document.createElement('div');
  group.className = 'summary-group';
  group.innerHTML = `<div class="summary-group-header" style="color:var(--keep)">manual decision <span style="float:right;color:var(--muted);font-weight:normal">${items.length} items · ${totalValue.toFixed(0)}g</span></div>`;
  for (const e of items) {
    const item = document.createElement('div');
    item.className = 'summary-item';
    item.innerHTML = `
      <span class="summary-item-name">${escapeHtml(e.name)}</span>
      <span class="summary-item-count">x${e.count}</span>
      <span class="summary-item-value">${(e.value || 0).toFixed(0)}g</span>`;
    group.appendChild(item);
  }
  container.appendChild(group);
}

function renderMapList(items) {
  const container = $('#map-list');
  container.innerHTML = '';

  if (!items.length) {
    container.innerHTML = '<div class="summary-empty">no items</div>';
    return;
  }

  // Group items by area
  const byArea = new Map();
  const noAreaItems = [];

  for (const e of items) {
    if (e.decision.verdict === 'favor') {
      const targets = (e.decision.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
      if (targets.length > 0) {
        const area = targets[0].area;
        if (!byArea.has(area)) byArea.set(area, { favor: [], sell: [], keep: [] });
        byArea.get(area).favor.push(e);
      } else {
        noAreaItems.push(e);
      }
    } else if (e.decision.verdict === 'sell_vendor' || e.decision.verdict === 'sell_consignment') {
      // Sell items don't have area info, put in noAreaItems
      noAreaItems.push(e);
    } else {
      // Keep items
      noAreaItems.push(e);
    }
  }

  // Render areas with favor items
  for (const [area, data] of byArea) {
    const section = document.createElement('div');
    section.className = 'map-section';

    const totalFavor = data.favor.reduce((sum, e) => {
      const targets = (e.decision.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
      const score = targets.length > 0 ? targets[0].score : 0;
      return sum + score * e.count;
    }, 0);

    section.innerHTML = `<div class="map-header">${escapeHtml(area)} <span style="float:right;font-size:12px;color:var(--muted);font-weight:normal">${data.favor.length} items · ${totalFavor.toFixed(1)} favor</span></div>`;

    // Favor subsection
    if (data.favor.length > 0) {
      const subsection = document.createElement('div');
      subsection.className = 'map-subsection';
      subsection.innerHTML = '<div class="map-subsection-title favor">Give as Favor</div>';
      
      // Group by NPC within this area
      const byNPC = new Map();
      for (const e of data.favor) {
        const targets = (e.decision.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
        if (targets.length > 0) {
          const npc = targets[0].npc;
          if (!byNPC.has(npc)) byNPC.set(npc, []);
          byNPC.get(npc).push(e);
        }
      }

      for (const [npc, entries] of byNPC) {
        const npcGroup = document.createElement('div');
        npcGroup.className = 'summary-group';
        npcGroup.innerHTML = `<div class="summary-group-header npc">${escapeHtml(npc)}</div>`;
        
        for (const e of entries) {
          const targets = (e.decision.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
          const score = targets.length > 0 ? targets[0].score : 0;
          const item = document.createElement('div');
          item.className = 'summary-item';
          item.innerHTML = `
            <span class="summary-item-name">${escapeHtml(e.name)}</span>
            <span class="summary-item-count">x${e.count}</span>
            <span class="summary-item-value">+${score.toFixed(1)} favor</span>`;
          npcGroup.appendChild(item);
        }
        subsection.appendChild(npcGroup);
      }
      section.appendChild(subsection);
    }

    container.appendChild(section);
  }

  // Render items without area (sell/keep/disabled favor)
  if (noAreaItems.length > 0) {
    const section = document.createElement('div');
    section.className = 'map-section';
    section.innerHTML = '<div class="map-header">Other / Any Vendor</div>';

    const sellItems = noAreaItems.filter(e => e.decision.verdict === 'sell_vendor' || e.decision.verdict === 'sell_consignment');
    const keepItems = noAreaItems.filter(e => e.decision.verdict === 'keep');
    const disabledFavor = noAreaItems.filter(e => e.decision.verdict === 'favor');

    if (sellItems.length > 0) {
      const subsection = document.createElement('div');
      subsection.className = 'map-subsection';
      subsection.innerHTML = '<div class="map-subsection-title sell">Sell</div>';
      
      const totalValue = sellItems.reduce((sum, e) => sum + (e.value || 0) * e.count, 0);
      const group = document.createElement('div');
      group.className = 'summary-group';
      group.innerHTML = `<div class="summary-group-header vendor">any vendor <span style="float:right;color:var(--muted);font-weight:normal">${sellItems.length} items · ${totalValue.toFixed(0)}g</span></div>`;
      
      for (const e of sellItems) {
        const item = document.createElement('div');
        item.className = 'summary-item';
        item.innerHTML = `
          <span class="summary-item-name">${escapeHtml(e.name)}</span>
          <span class="summary-item-count">x${e.count}</span>
          <span class="summary-item-value">${(e.value || 0).toFixed(0)}g</span>`;
        group.appendChild(item);
      }
      subsection.appendChild(group);
      section.appendChild(subsection);
    }

    if (keepItems.length > 0) {
      const subsection = document.createElement('div');
      subsection.className = 'map-subsection';
      subsection.innerHTML = '<div class="map-subsection-title keep">Keep</div>';
      
      const totalValue = keepItems.reduce((sum, e) => sum + (e.value || 0) * e.count, 0);
      const group = document.createElement('div');
      group.className = 'summary-group';
      group.innerHTML = `<div class="summary-group-header" style="color:var(--keep)">manual decision <span style="float:right;color:var(--muted);font-weight:normal">${keepItems.length} items · ${totalValue.toFixed(0)}g</span></div>`;
      
      for (const e of keepItems) {
        const item = document.createElement('div');
        item.className = 'summary-item';
        item.innerHTML = `
          <span class="summary-item-name">${escapeHtml(e.name)}</span>
          <span class="summary-item-count">x${e.count}</span>
          <span class="summary-item-value">${(e.value || 0).toFixed(0)}g</span>`;
        group.appendChild(item);
      }
      subsection.appendChild(group);
      section.appendChild(subsection);
    }

    if (disabledFavor.length > 0) {
      const subsection = document.createElement('div');
      subsection.className = 'map-subsection';
      subsection.innerHTML = '<div class="map-subsection-title" style="background:rgba(149,165,166,0.15);color:var(--muted)">Disabled NPCs</div>';
      
      const group = document.createElement('div');
      group.className = 'summary-group';
      group.innerHTML = `<div class="summary-group-header" style="color:var(--muted)">favor targets disabled <span style="float:right;color:var(--muted);font-weight:normal">${disabledFavor.length} items</span></div>`;
      
      for (const e of disabledFavor) {
        const item = document.createElement('div');
        item.className = 'summary-item';
        item.innerHTML = `
          <span class="summary-item-name">${escapeHtml(e.name)}</span>
          <span class="summary-item-count">x${e.count}</span>`;
        group.appendChild(item);
      }
      subsection.appendChild(group);
      section.appendChild(subsection);
    }

    container.appendChild(section);
  }
}

// History view functions
async function renderHistory() {
  const container = $('#history-list');
  container.innerHTML = '<div class="summary-empty">Loading sessions...</div>';

  const sessions = await api('/api/sessions');
  if (!sessions) {
    container.innerHTML = '<div class="summary-empty">Failed to load sessions</div>';
    return;
  }

  if (sessions.length === 0) {
    container.innerHTML = '<div class="summary-empty">No sessions yet. Start a session to see history.</div>';
    $('#history-total-sessions').textContent = '0 sessions';
    $('#history-total-value').textContent = '0g total';
    return;
  }

  const totalSessions = sessions.length;
  const totalValue = sessions.reduce((sum, s) => sum + s.total_value, 0);
  $('#history-total-sessions').textContent = `${totalSessions} session${totalSessions !== 1 ? 's' : ''}`;
  $('#history-total-value').textContent = `${totalValue.toFixed(0)}g total`;

  container.innerHTML = '';
  for (const session of sessions) {
    const card = document.createElement('div');
    card.className = 'history-card';
    card.onclick = () => loadSessionDetail(session.id);

    const date = new Date(session.started_at);
    const dateStr = date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], {hour12: false, hour: '2-digit', minute: '2-digit'});

    card.innerHTML = `
      <div class="history-card-header">
        <div class="history-card-dungeon">${escapeHtml(session.dungeon)}</div>
        <div class="history-card-date">${dateStr}</div>
      </div>
      ${session.notes ? `<div class="history-card-notes">${escapeHtml(session.notes)}</div>` : ''}
      <div class="history-card-stats">
        <div class="history-stat">
          <div class="history-stat-label">Duration</div>
          <div class="history-stat-value">${fmtElapsed(session.duration_secs * 1000)}</div>
        </div>
        <div class="history-stat">
          <div class="history-stat-label">Items</div>
          <div class="history-stat-value">${session.total_items} (${session.unique_items} unique)</div>
        </div>
        <div class="history-stat">
          <div class="history-stat-label">Value</div>
          <div class="history-stat-value value">${session.total_value.toFixed(0)}g</div>
        </div>
        <div class="history-stat">
          <div class="history-stat-label">Favor</div>
          <div class="history-stat-value">${session.favor_items}</div>
        </div>
        <div class="history-stat">
          <div class="history-stat-label">Sell</div>
          <div class="history-stat-value">${session.sell_items}</div>
        </div>
      </div>
    `;
    container.appendChild(card);
  }
}

async function loadSessionDetail(sessionId) {
  const snapshot = await api(`/api/session/${sessionId}`);
  if (!snapshot) {
    alert('Failed to load session details');
    return;
  }

  state.historyDetail = snapshot;
  switchView('history-detail');
  renderHistoryDetail(snapshot);
}

function renderHistoryDetail(snapshot) {
  const date = new Date(snapshot.started_at);
  const dateStr = date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], {hour12: false, hour: '2-digit', minute: '2-digit'});

  $('#history-detail-title').textContent = snapshot.dungeon || 'Unnamed Session';
  $('#history-detail-dungeon').textContent = snapshot.dungeon || 'unnamed';
  $('#history-detail-date').textContent = dateStr;
  $('#history-detail-duration').textContent = fmtElapsed(new Date(snapshot.ended_at).getTime() - date.getTime());

  let totalItems = 0;
  let totalValue = 0;
  let favorItems = 0;
  let sellItems = 0;

  for (const loot of snapshot.loot) {
    totalItems += loot.count;
    totalValue += loot.value * loot.count;
    if (loot.decision.verdict === 'favor') favorItems++;
    if (loot.decision.verdict === 'sell_vendor' || loot.decision.verdict === 'sell_consignment') sellItems++;
  }

  $('#history-detail-items').textContent = `${totalItems} (${snapshot.loot.length} unique)`;
  $('#history-detail-value').textContent = `${totalValue.toFixed(0)}g`;
  $('#history-detail-favor').textContent = favorItems;
  $('#history-detail-sell').textContent = sellItems;

  const itemsList = $('#history-detail-items-list');
  itemsList.innerHTML = '';

  const sortedLoot = [...snapshot.loot].sort((a, b) => (b.value * b.count) - (a.value * a.count));

  for (const loot of sortedLoot) {
    const item = document.createElement('div');
    item.className = 'summary-item';
    const verdictClass = loot.decision.verdict.replace(/_/g, '-');
    item.innerHTML = `
      <span class="summary-item-name">${escapeHtml(loot.name)}</span>
      <span class="summary-item-count">x${loot.count}</span>
      <span class="summary-item-value">${(loot.value * loot.count).toFixed(0)}g</span>
      <span class="verdict ${loot.decision.verdict}">${loot.decision.verdict.replace(/_/g, ' ')}</span>
    `;
    itemsList.appendChild(item);
  }
}

$('#back-to-history').addEventListener('click', () => {
  switchView('history');
});

$('#export-csv')?.addEventListener('click', () => {
  if (state.historyDetail) {
    // Extract session ID from the started_at timestamp (format: session-YYYYMMDD-HHMMSS)
    const d = new Date(state.historyDetail.started_at);
    const pad = n => String(n).padStart(2, '0');
    const sessionId = `session-${d.getFullYear()}${pad(d.getMonth()+1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`;
    window.location.href = `/api/session/${sessionId}/export`;
  }
});

// Favor progress view functions
function renderFavorView() {
  const container = $('#favor-list-view');
  container.innerHTML = '';
  const search = $('#favor-search').value.toLowerCase();

  const filtered = state.npcs.filter(npc =>
    npc.name.toLowerCase().includes(search) ||
    npc.area.toLowerCase().includes(search)
  );

  if (filtered.length === 0) {
    container.innerHTML = '<div class="summary-empty">No NPCs found</div>';
    return;
  }

  for (const npc of filtered) {
    const isCompleted = state.favorProgress.has(npc.name);
    const item = document.createElement('div');
    item.className = 'favor-item' + (isCompleted ? ' completed' : '');
    item.innerHTML = `
      <div class="favor-info">
        <div class="favor-name">${escapeHtml(npc.name)}</div>
        <div class="favor-area">${escapeHtml(npc.area)}</div>
      </div>
      <div class="favor-status">
        <span class="favor-badge ${isCompleted ? 'completed' : 'pending'}">${isCompleted ? 'Maxed' : 'Pending'}</span>
        <label class="favor-toggle">
          <input type="checkbox" ${isCompleted ? 'checked' : ''} onchange="toggleFavorProgress('${escapeHtml(npc.name).replace(/'/g, "\\'")}')">
          <span class="slider"></span>
        </label>
      </div>
    `;
    container.appendChild(item);
  }
}

$('#favor-search').addEventListener('input', () => {
  if (state.currentView === 'favor') renderFavorView();
});

window.toggleFavorProgress = function(npcName) {
  if (state.favorProgress.has(npcName)) {
    state.favorProgress.delete(npcName);
    state.disabledNPCs.delete(npcName);
  } else {
    state.favorProgress.add(npcName);
    state.disabledNPCs.add(npcName);
  }
  saveSettings();
  renderFavorView();
};

// Shop NPC functions
function renderShopNPCList() {
  const container = $('#shopnpc-list');
  container.innerHTML = '';
  const search = $('#shopnpc-search').value.toLowerCase();
  
  const filtered = state.npcs.filter(npc => 
    npc.name.toLowerCase().includes(search) || 
    npc.area.toLowerCase().includes(search)
  );

  const hiddenNames = new Set(state.shopNPCs);

  $('#shopnpc-count').textContent = filtered.filter(n => !hiddenNames.has(n.name)).length;

  for (const npc of filtered) {
    const isHidden = hiddenNames.has(npc.name);
    const item = document.createElement('div');
    item.className = 'npc-item' + (isHidden ? ' disabled' : '');
    item.innerHTML = `
      <div class="npc-info">
        <div class="npc-name">${escapeHtml(npc.name)}</div>
        <div class="npc-area">${escapeHtml(npc.area)}</div>
      </div>
      <label class="npc-toggle">
        <input type="checkbox" ${!isHidden ? 'checked' : ''} onchange="toggleShopNPC('${escapeHtml(npc.name).replace(/'/g, "\\'")}')">
        <span class="slider"></span>
      </label>`;
    container.appendChild(item);
  }
}

window.toggleShopNPC = function(name) {
  if (state.shopNPCs.includes(name)) {
    state.shopNPCs = state.shopNPCs.filter(n => n !== name);
  } else {
    state.shopNPCs.push(name);
  }
  saveSettings();
  renderShopNPCList();
};

// NPC Settings functions
async function loadNPCList() {
  const npcs = await api('/api/npcs');
  if (npcs) {
    state.npcs = npcs;
    if (state.currentView === 'favor') renderFavorView();
  }
}

$('#shopnpc-search').addEventListener('input', () => {
  if (state.currentView === 'shopnpc') renderShopNPCList();
});

// API helpers
async function api(path, method = 'GET', body = null) {
  const r = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!r.ok) {
    alert(`${path}: ${await r.text()}`);
    return null;
  }
  return r.json();
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

// Request notification permission on page load
if ('Notification' in window && Notification.permission === 'default') {
  // We'll request permission when user first starts a session
}

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

// Settings view functions
async function renderSettingsView() {
  const statusEl = $('#settings-status');
  statusEl.textContent = '';
  statusEl.className = 'settings-status';

  const cfg = await api('/api/config');
  if (!cfg) {
    statusEl.textContent = 'Failed to load configuration';
    statusEl.className = 'settings-status error';
    return;
  }

  $('#settings-chat-log-dir').value = cfg.chat_log_dir || '';
  $('#settings-loot-regex').value = cfg.loot_regex || '';
  $('#settings-sell-value-threshold').value = cfg.sell_value_threshold || 50;
  $('#settings-notification-threshold').value = cfg.notification_threshold || 500;
  
  // Load player prices from server config
  state.playerPrices = cfg.player_prices || {};
  state.notificationThreshold = cfg.notification_threshold || 500;
  renderPlayerPrices();
}

// Settings form submission
$('#settings-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const statusEl = $('#settings-status');
  statusEl.textContent = 'Saving...';
  statusEl.className = 'settings-status';

  const chat_log_dir = $('#settings-chat-log-dir').value.trim();
  const loot_regex = $('#settings-loot-regex').value.trim();
  const sell_value_threshold = parseFloat($('#settings-sell-value-threshold').value) || 0;
  const notification_threshold = parseFloat($('#settings-notification-threshold').value) || 500;

  const res = await api('/api/config', 'POST', {
    chat_log_dir,
    loot_regex,
    sell_value_threshold,
    player_prices: state.playerPrices,
    notification_threshold
  });

  if (res && res.ok) {
    state.notificationThreshold = notification_threshold;
    statusEl.textContent = 'Settings saved successfully!';
    statusEl.className = 'settings-status success';
  } else {
    statusEl.textContent = 'Failed to save settings';
    statusEl.className = 'settings-status error';
  }
});

// First paint
refreshAll();
loadNPCList();

// Loot table search/filter
$('#loot-search')?.addEventListener('input', () => {
  if (state.session) renderLootTable(state.session);
});

$('#loot-filter')?.addEventListener('change', () => {
  if (state.session) renderLootTable(state.session);
});

// Player prices management
function renderPlayerPrices() {
  const container = $('#pp-list');
  if (!container) return;
  container.innerHTML = '';
  
  const entries = Object.entries(state.playerPrices);
  if (entries.length === 0) {
    container.innerHTML = '<div class="summary-empty">No player prices set</div>';
    return;
  }
  
  for (const [name, price] of entries) {
    const item = document.createElement('div');
    item.className = 'pp-item';
    item.innerHTML = `
      <span class="pp-name">${escapeHtml(name)}</span>
      <span class="pp-price">${price.toFixed(0)}g</span>
      <button class="pp-delete" onclick="removePlayerPrice('${escapeHtml(name).replace(/'/g, "\\'")}')">×</button>
    `;
    container.appendChild(item);
  }
}

window.removePlayerPrice = async function(name) {
  delete state.playerPrices[name];
  saveSettings();
  renderPlayerPrices();
  await api('/api/config', 'POST', {
    chat_log_dir: $('#settings-chat-log-dir')?.value.trim() || '',
    loot_regex: $('#settings-loot-regex')?.value.trim() || '',
    sell_value_threshold: parseFloat($('#settings-sell-value-threshold')?.value) || 0,
    player_prices: state.playerPrices
  });
};

$('#pp-add')?.addEventListener('click', async () => {
  const name = $('#pp-item-name').value.trim();
  const price = parseFloat($('#pp-item-price').value);
  if (!name || isNaN(price) || price <= 0) return;
  
  state.playerPrices[name] = price;
  saveSettings();
  renderPlayerPrices();
  $('#pp-item-name').value = '';
  $('#pp-item-price').value = '';
  
  await api('/api/config', 'POST', {
    chat_log_dir: $('#settings-chat-log-dir')?.value.trim() || '',
    loot_regex: $('#settings-loot-regex')?.value.trim() || '',
    sell_value_threshold: parseFloat($('#settings-sell-value-threshold')?.value) || 0,
    player_prices: state.playerPrices
  });
});

// Traders view functions
async function renderTradersView() {
  const container = $('#traders-list');
  if (!container) return;
  
  const traders = await api('/api/traders');
  if (!traders) {
    container.innerHTML = '<div class="summary-empty">Failed to load traders</div>';
    return;
  }
  
  state.traders = traders;
  $('#traders-count').textContent = traders.length;
  
  if (traders.length === 0) {
    container.innerHTML = '<div class="summary-empty">No traders added yet. Click "+ Add Trader" to get started.</div>';
    return;
  }
  
  container.innerHTML = '';
  for (const trader of traders) {
    const card = document.createElement('div');
    card.className = 'trader-card';
    
    const remaining = trader.weekly_limit - trader.sold_this_week;
    const percentUsed = (trader.sold_this_week / trader.weekly_limit) * 100;
    const progressClass = percentUsed >= 90 ? 'danger' : percentUsed >= 70 ? 'warning' : 'normal';
    
    // Format reset countdown
    const resetCountdown = trader.time_until_reset || '5d 22h';
    
    card.innerHTML = `
      <div class="trader-header">
        <div class="trader-info">
          <div class="trader-name">${escapeHtml(trader.npc_name)}</div>
          <div class="trader-area">${escapeHtml(trader.area)}</div>
        </div>
        <div class="trader-actions">
          <button class="trader-edit" onclick="editTrader('${escapeHtml(trader.npc_name).replace(/'/g, "\\'")}')">Edit</button>
          <button class="trader-delete" onclick="removeTrader('${escapeHtml(trader.npc_name).replace(/'/g, "\\'")}')">×</button>
        </div>
      </div>
      <div class="trader-progress">
        <div class="progress-bar">
          <div class="progress-fill ${progressClass}" style="width: ${Math.min(percentUsed, 100)}%"></div>
        </div>
        <div class="progress-text">
          <span>${trader.sold_this_week.toFixed(0)}g / ${trader.weekly_limit.toFixed(0)}g</span>
          <span class="remaining">${remaining.toFixed(0)}g remaining</span>
        </div>
        <div class="reset-time">
          <span class="reset-label">Resets in:</span>
          <span class="reset-value">${escapeHtml(resetCountdown)}</span>
        </div>
      </div>
      <div class="trader-actions">
        <button class="log-sale-btn" onclick="logSale('${escapeHtml(trader.npc_name).replace(/'/g, "\\'")}')">Log Sale</button>
      </div>
    `;
    container.appendChild(card);
  }
}

window.removeTrader = async function(npcName) {
  if (!confirm(`Remove ${npcName} from traders?`)) return;
  
  await api('/api/traders', 'DELETE', { npc_name: npcName });
  renderTradersView();
};

window.editTrader = async function(npcName) {
  const trader = state.traders.find(t => t.npc_name === npcName);
  if (!trader) return;
  
  // Show edit modal
  const modal = document.createElement('div');
  modal.className = 'modal';
  modal.innerHTML = `
    <div class="modal-content modal-small">
      <div class="modal-header">
        <h3>Edit Trader</h3>
        <button class="modal-close">×</button>
      </div>
      <div class="modal-body">
        <div class="form-group">
          <label>${escapeHtml(trader.npc_name)}</label>
          <label class="sublabel">${escapeHtml(trader.area)}</label>
        </div>
        <div class="form-group">
          <label>Weekly Limit (gold)</label>
          <input type="number" class="limit-input" value="${trader.weekly_limit}" min="0" step="100">
        </div>
        <div class="form-group">
          <label>Reset Duration</label>
          <div style="display:flex;gap:8px">
            <input type="number" class="reset-days" value="${trader.reset_days || 5}" min="0" max="30" style="width:80px">
            <span style="line-height:32px">days</span>
            <input type="number" class="reset-hours" value="${trader.reset_hours || 22}" min="0" max="23" style="width:80px">
            <span style="line-height:32px">hours</span>
          </div>
          <div class="sublabel">Time until limit resets after a sale</div>
        </div>
        <button class="save-trader-btn">Save Changes</button>
      </div>
    </div>
  `;
  document.body.appendChild(modal);
  
  const limitInput = modal.querySelector('.limit-input');
  const daysInput = modal.querySelector('.reset-days');
  const hoursInput = modal.querySelector('.reset-hours');
  limitInput.focus();
  limitInput.select();
  
  modal.querySelector('.modal-close').onclick = () => modal.remove();
  modal.onclick = (e) => { if (e.target === modal) modal.remove(); };
  
  modal.querySelector('.save-trader-btn').onclick = async () => {
    const limit = parseFloat(limitInput.value);
    if (isNaN(limit) || limit <= 0) {
      alert('Invalid limit');
      return;
    }
    
    const days = parseInt(daysInput.value) || 0;
    const hours = parseInt(hoursInput.value) || 0;
    if (days === 0 && hours === 0) {
      alert('Reset duration must be at least 1 hour');
      return;
    }
    
    await api('/api/traders', 'POST', {
      action: 'update',
      npc_name: npcName,
      area: trader.area,
      weekly_limit: limit,
      reset_days: days,
      reset_hours: hours
    });
    modal.remove();
    renderTradersView();
  };
  
  limitInput.onkeydown = (e) => {
    if (e.key === 'Enter') modal.querySelector('.save-trader-btn').click();
  };
};

window.logSale = async function(npcName) {
  const amount = prompt(`How much did you sell to ${npcName}?`);
  if (!amount) return;
  
  const value = parseFloat(amount);
  if (isNaN(value) || value <= 0) {
    alert('Invalid amount');
    return;
  }
  
  await api('/api/traders', 'POST', {
    action: 'log_sale',
    npc_name: npcName,
    amount: value
  });
  renderTradersView();
};

$('#traders-add-btn')?.addEventListener('click', async () => {
  // Show modal with searchable NPC list grouped by area
  const modal = document.createElement('div');
  modal.className = 'modal';
  modal.innerHTML = `
    <div class="modal-content">
      <div class="modal-header">
        <h3>Add Trader</h3>
        <button class="modal-close">×</button>
      </div>
      <div class="modal-body">
        <input type="text" class="npc-search" placeholder="Search NPC..." autocomplete="off">
        <div class="npc-list-grouped"></div>
      </div>
    </div>
  `;
  document.body.appendChild(modal);
  
  const npcListContainer = modal.querySelector('.npc-list-grouped');
  const searchInput = modal.querySelector('.npc-search');
  
  // Group NPCs by area
  const npcsByArea = {};
  for (const npc of state.npcs) {
    const area = npc.area || 'Unknown';
    if (!npcsByArea[area]) npcsByArea[area] = [];
    npcsByArea[area].push(npc);
  }
  
  function renderNPCList(filter = '') {
    npcListContainer.innerHTML = '';
    const filterLower = filter.toLowerCase();
    
    for (const [area, npcs] of Object.entries(npcsByArea).sort()) {
      const filteredNPCs = npcs.filter(n => 
        n.name.toLowerCase().includes(filterLower) || 
        area.toLowerCase().includes(filterLower)
      );
      
      if (filteredNPCs.length === 0) continue;
      
      const areaSection = document.createElement('div');
      areaSection.className = 'npc-area-section';
      areaSection.innerHTML = `<div class="npc-area-header">${escapeHtml(area)}</div>`;
      
      for (const npc of filteredNPCs) {
        const npcItem = document.createElement('div');
        npcItem.className = 'npc-item-selectable';
        npcItem.innerHTML = `<span>${escapeHtml(npc.name)}</span>`;
        npcItem.onclick = () => selectNPC(npc.name, area, modal);
        areaSection.appendChild(npcItem);
      }
      
      npcListContainer.appendChild(areaSection);
    }
  }
  
  renderNPCList();
  
  searchInput.oninput = () => renderNPCList(searchInput.value);
  
  modal.querySelector('.modal-close').onclick = () => modal.remove();
  modal.onclick = (e) => { if (e.target === modal) modal.remove(); };
});

function selectNPC(npcName, area, modal) {
  modal.remove();
  
  // Show limit input modal
  const limitModal = document.createElement('div');
  limitModal.className = 'modal';
  limitModal.innerHTML = `
    <div class="modal-content modal-small">
      <div class="modal-header">
        <h3>Set Weekly Limit</h3>
        <button class="modal-close">×</button>
      </div>
      <div class="modal-body">
        <div class="form-group">
          <label>${escapeHtml(npcName)}</label>
          <label class="sublabel">${escapeHtml(area)}</label>
          <input type="number" class="limit-input" placeholder="Weekly limit (gold)" min="0" step="100">
        </div>
        <div class="form-group">
          <label>Reset Duration</label>
          <div style="display:flex;gap:8px">
            <input type="number" class="reset-days" value="5" min="0" max="30" style="width:80px">
            <span style="line-height:32px">days</span>
            <input type="number" class="reset-hours" value="22" min="0" max="23" style="width:80px">
            <span style="line-height:32px">hours</span>
          </div>
          <div class="sublabel">Time until limit resets after a sale</div>
        </div>
        <button class="add-trader-confirm">Add Trader</button>
      </div>
    </div>
  `;
  document.body.appendChild(limitModal);
  
  const limitInput = limitModal.querySelector('.limit-input');
  const daysInput = limitModal.querySelector('.reset-days');
  const hoursInput = limitModal.querySelector('.reset-hours');
  
  limitInput.focus();
  
  limitModal.querySelector('.modal-close').onclick = () => limitModal.remove();
  limitModal.onclick = (e) => { if (e.target === limitModal) limitModal.remove(); };
  
  limitModal.querySelector('.add-trader-confirm').onclick = async () => {
    const limit = parseFloat(limitInput.value);
    if (isNaN(limit) || limit <= 0) {
      alert('Invalid limit');
      return;
    }
    
    const days = parseInt(daysInput.value) || 0;
    const hours = parseInt(hoursInput.value) || 0;
    if (days === 0 && hours === 0) {
      alert('Reset duration must be at least 1 hour');
      return;
    }
    
    await api('/api/traders', 'POST', {
      action: 'add',
      npc_name: npcName,
      area: area,
      weekly_limit: limit,
      reset_days: days,
      reset_hours: hours
    });
    limitModal.remove();
    renderTradersView();
  };
  
  limitInput.onkeydown = (e) => {
    if (e.key === 'Enter') limitModal.querySelector('.add-trader-confirm').click();
  };
}

// ==================== Warcache Solver ====================

const SYM_LABELS = ['1','2','3','4','5','6','7','8','9','10','11','12'];
const NUM_SYMS = 12;
const SLOT_COUNT = 4;

let warcachePossibilities = [];
let warcacheGuess = [null, null, null, null];
let warcacheSelectedSlot = 0;
let warcacheHistory = [];

function warcacheGenerateAll() {
  const all = [];
  for (let a = 0; a < NUM_SYMS; a++)
    for (let b = 0; b < NUM_SYMS; b++)
      for (let c = 0; c < NUM_SYMS; c++)
        for (let d = 0; d < NUM_SYMS; d++)
          all.push([a, b, c, d]);
  return all;
}

function warcacheComputeFeedback(guess, possibility) {
  let correct = 0;
  const gCount = new Array(NUM_SYMS).fill(0);
  const pCount = new Array(NUM_SYMS).fill(0);
  for (let i = 0; i < SLOT_COUNT; i++) {
    if (guess[i] === possibility[i]) {
      correct++;
    } else {
      gCount[guess[i]]++;
      pCount[possibility[i]]++;
    }
  }
  let wrong = 0;
  for (let s = 0; s < NUM_SYMS; s++) {
    wrong += Math.min(gCount[s], pCount[s]);
  }
  return [correct, wrong];
}

function warcacheFilter(guess, feedback) {
  const [fc, fw] = feedback;
  warcachePossibilities = warcachePossibilities.filter(p => {
    const [c, w] = warcacheComputeFeedback(guess, p);
    return c === fc && w === fw;
  });
}

function warcacheSuggest() {
  if (warcachePossibilities.length === 0) return null;
  if (warcachePossibilities.length === 1) return warcachePossibilities[0];
  
  // Minimax: find guess that minimizes max remaining possibilities
  // For efficiency, only evaluate guesses from remaining possibilities
  let bestGuess = null;
  let minMaxRemaining = Infinity;
  
  // Sample if too many possibilities (use first 500 for speed)
  const candidates = warcachePossibilities.length > 500 
    ? warcachePossibilities.slice(0, 500) 
    : warcachePossibilities;
  
  for (const guess of candidates) {
    // Count feedback distribution
    const feedbackCounts = {};
    for (const possibility of warcachePossibilities) {
      const [c, w] = warcacheComputeFeedback(guess, possibility);
      const key = `${c},${w}`;
      feedbackCounts[key] = (feedbackCounts[key] || 0) + 1;
    }
    
    // Find max remaining for this guess
    const maxRemaining = Math.max(...Object.values(feedbackCounts));
    
    if (maxRemaining < minMaxRemaining) {
      minMaxRemaining = maxRemaining;
      bestGuess = guess;
    }
  }
  
  return bestGuess || warcachePossibilities[0];
}

function warcacheRenderGuessSlots() {
  for (let i = 0; i < SLOT_COUNT; i++) {
    const slot = $(`.guess-slot[data-pos="${i}"]`);
    if (slot) {
      if (warcacheGuess[i] !== null) {
        slot.textContent = SYM_LABELS[warcacheGuess[i]];
        slot.classList.add('filled');
      } else {
        slot.textContent = '';
        slot.classList.remove('filled');
      }
    }
  }
  // Highlight selected slot
  $$('.guess-slot').forEach((el, i) => {
    el.style.borderColor = i === warcacheSelectedSlot ? 'var(--accent)' : '';
  });
}

function warcacheUpdateSuggestion() {
  const sug = warcacheSuggest();
  const el = $('#warcache-suggestion');
  if (sug) {
    el.textContent = sug.map(s => SYM_LABELS[s]).join(' ');
  } else {
    el.textContent = warcachePossibilities.length === 0 ? 'No solution!' : '—';
  }
  const rem = $('#warcache-remaining');
  if (rem) rem.textContent = `${warcachePossibilities.length.toLocaleString()} possibilities remaining`;
}

function warcacheRenderHistory() {
  const container = $('#warcache-history');
  if (!container) return;
  container.innerHTML = '';
  for (const entry of warcacheHistory) {
    const row = document.createElement('div');
    row.className = 'warcache-history-row';
    const guessHtml = entry.guess.map(s => 
      `<div class="history-sym">${SYM_LABELS[s]}</div>`
    ).join('');
    row.innerHTML = `
      <div class="history-guess">${guessHtml}</div>
      <span class="history-feedback">${entry.feedback[0]},${entry.feedback[1]}</span>
    `;
    container.appendChild(row);
  }
}

function renderWarcacheView() {
  if (warcachePossibilities.length === 0) {
    warcachePossibilities = warcacheGenerateAll();
  }
  warcacheRenderGuessSlots();
  warcacheUpdateSuggestion();
  warcacheRenderHistory();
}

// Symbol button clicks
$$('.sym-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    const sym = parseInt(btn.dataset.sym);
    warcacheGuess[warcacheSelectedSlot] = sym;
    // Move to next empty slot
    for (let i = 0; i < SLOT_COUNT; i++) {
      const next = (warcacheSelectedSlot + 1 + i) % SLOT_COUNT;
      if (warcacheGuess[next] === null) {
        warcacheSelectedSlot = next;
        break;
      }
    }
    warcacheRenderGuessSlots();
  });
});

// Slot clicks to select which slot to fill
$$('.guess-slot').forEach(slot => {
  slot.addEventListener('click', () => {
    warcacheSelectedSlot = parseInt(slot.dataset.pos);
    warcacheRenderGuessSlots();
  });
});

// Submit guess
$('#warcache-submit')?.addEventListener('click', () => {
  if (warcacheGuess.includes(null)) {
    alert('Fill all 4 slots first');
    return;
  }
  const fbText = $('#warcache-feedback').value.trim();
  const match = fbText.match(/^(\d)\s*,\s*(\d)$/);
  if (!match) {
    alert('Enter feedback as "correct,wrong" (e.g. 0,2 or 1,1)');
    return;
  }
  const feedback = [parseInt(match[1]), parseInt(match[2])];
  
  // Validate: correct + wrong <= 4
  if (feedback[0] + feedback[1] > SLOT_COUNT) {
    alert('Correct + wrong cannot exceed 4');
    return;
  }
  // Special case: 4,0 means solved
  if (feedback[0] === 4) {
    alert('Solved! The answer is ' + warcacheGuess.map(s => SYM_LABELS[s]).join(' '));
  }
  
  warcacheFilter([...warcacheGuess], feedback);
  warcacheHistory.push({ guess: [...warcacheGuess], feedback });
  
  // Reset guess
  warcacheGuess = [null, null, null, null];
  warcacheSelectedSlot = 0;
  $('#warcache-feedback').value = '';
  
  warcacheRenderGuessSlots();
  warcacheUpdateSuggestion();
  warcacheRenderHistory();
});

// Undo last guess
$('#warcache-undo')?.addEventListener('click', () => {
  if (warcacheHistory.length === 0) return;
  // Regenerate and re-apply all but last
  warcachePossibilities = warcacheGenerateAll();
  const last = warcacheHistory.pop();
  for (const entry of warcacheHistory) {
    warcacheFilter(entry.guess, entry.feedback);
  }
  warcacheRenderGuessSlots();
  warcacheUpdateSuggestion();
  warcacheRenderHistory();
});

// Use suggestion
$('#warcache-use-suggestion')?.addEventListener('click', () => {
  const sug = warcacheSuggest();
  if (sug) {
    warcacheGuess = [...sug];
    warcacheSelectedSlot = 0;
    warcacheRenderGuessSlots();
  }
});

// Reset
$('#warcache-reset')?.addEventListener('click', () => {
  warcachePossibilities = warcacheGenerateAll();
  warcacheGuess = [null, null, null, null];
  warcacheSelectedSlot = 0;
  warcacheHistory = [];
  $('#warcache-feedback').value = '';
  warcacheRenderGuessSlots();
  warcacheUpdateSuggestion();
  warcacheRenderHistory();
});
