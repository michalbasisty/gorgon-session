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
  favorProgress: new Set()
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
  } catch (e) {
    state.disabledNPCs = new Set();
    state.shopNPCs = [];
    state.craftingRecipes = [];
    state.favorProgress = new Set();
  }
}

function saveSettings() {
  localStorage.setItem('disabledNPCs', JSON.stringify([...state.disabledNPCs]));
  localStorage.setItem('shopNPCs', JSON.stringify(state.shopNPCs));
  localStorage.setItem('craftingRecipes', JSON.stringify(state.craftingRecipes));
  localStorage.setItem('favorProgress', JSON.stringify([...state.favorProgress]));
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
  $('#view-crafting').classList.toggle('hidden', view !== 'crafting');
  $('#view-favor').classList.toggle('hidden', view !== 'favor');
  $('#view-shopnpc').classList.toggle('hidden', view !== 'shopnpc');

  const titles = { 
    tracker: 'Tracker', 
    summary: 'Summary', 
    history: 'History',
    'history-detail': 'Session Details',
    crafting: 'Crafting',
    favor: 'Favor Progress',
    shopnpc: 'Shop NPC'
  };
  $('#view-title').innerHTML = `${titles[view]} <small id="state">${state.session?.state || 'idle'}</small>`;

  if (view === 'summary' && state.session) renderSummary(state.session);
  if (view === 'history') renderHistory();
  if (view === 'crafting') renderCraftingView();
  if (view === 'favor') renderFavorView();
  if (view === 'shopnpc') renderShopNPCList();
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
  countEl.textContent = (s.loot || []).length;
  emptyEl.style.display = (s.loot || []).length ? 'none' : 'block';
  for (const e of (s.loot || [])) addRow(e);
}

function addRow(e) {
  const tr = document.createElement('tr');
  tr.dataset.name = e.name;
  tr.innerHTML = `
    <td class="time">${relTime(e.last_seen)}</td>
    <td>${escapeHtml(e.name)}</td>
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

// Crafting view functions
function renderCraftingView() {
  const container = $('#crafting-recipes');
  container.innerHTML = '';

  if (state.craftingRecipes.length === 0) {
    container.innerHTML = '<div class="summary-empty">No recipes yet. Add a recipe to track materials.</div>';
    return;
  }

  for (const recipe of state.craftingRecipes) {
    const card = document.createElement('div');
    card.className = 'recipe-card';
    
    const completedCount = recipe.materials.filter(m => m.completed).length;
    const totalCount = recipe.materials.length;
    
    card.innerHTML = `
      <div class="recipe-header">
        <div class="recipe-name">${escapeHtml(recipe.name)} <span style="color:var(--muted);font-size:12px;font-weight:normal">(${completedCount}/${totalCount})</span></div>
        <div class="recipe-actions">
          <button onclick="addMaterialToRecipe('${recipe.id}')">+ Material</button>
          <button class="delete" onclick="deleteRecipe('${recipe.id}')">Delete</button>
        </div>
      </div>
      <div class="material-input" id="material-input-${recipe.id}" style="display:none">
        <input type="text" placeholder="Material name" id="material-name-${recipe.id}">
        <input type="number" placeholder="Qty" min="1" value="1" id="material-qty-${recipe.id}">
        <button onclick="saveMaterial('${recipe.id}')">Add</button>
      </div>
      <ul class="material-list">
        ${recipe.materials.map(m => `
          <li class="material-item ${m.completed ? 'checked' : ''}">
            <input type="checkbox" ${m.completed ? 'checked' : ''} onchange="toggleMaterial('${recipe.id}', '${m.id}')">
            <span class="material-name">${escapeHtml(m.name)}</span>
            <span class="material-qty">x${m.qty}</span>
            <button onclick="deleteMaterial('${recipe.id}', '${m.id}')">×</button>
          </li>
        `).join('')}
      </ul>
    `;
    container.appendChild(card);
  }
}

$('#crafting-add-recipe').addEventListener('click', () => {
  const nameInput = $('#crafting-recipe-name');
  const name = nameInput.value.trim();
  if (!name) return;

  const recipe = {
    id: Date.now().toString(),
    name: name,
    materials: []
  };

  state.craftingRecipes.push(recipe);
  saveSettings();
  nameInput.value = '';
  renderCraftingView();
});

window.addMaterialToRecipe = function(recipeId) {
  const inputDiv = $(`#material-input-${recipeId}`);
  inputDiv.style.display = inputDiv.style.display === 'none' ? 'flex' : 'none';
};

window.saveMaterial = function(recipeId) {
  const name = $(`#material-name-${recipeId}`).value.trim();
  const qty = parseInt($(`#material-qty-${recipeId}`).value) || 1;
  
  if (!name) return;

  const recipe = state.craftingRecipes.find(r => r.id === recipeId);
  if (!recipe) return;

  recipe.materials.push({
    id: Date.now().toString(),
    name: name,
    qty: qty,
    completed: false
  });

  saveSettings();
  renderCraftingView();
};

window.toggleMaterial = function(recipeId, materialId) {
  const recipe = state.craftingRecipes.find(r => r.id === recipeId);
  if (!recipe) return;

  const material = recipe.materials.find(m => m.id === materialId);
  if (!material) return;

  material.completed = !material.completed;
  saveSettings();
  renderCraftingView();
};

window.deleteMaterial = function(recipeId, materialId) {
  const recipe = state.craftingRecipes.find(r => r.id === recipeId);
  if (!recipe) return;

  recipe.materials = recipe.materials.filter(m => m.id !== materialId);
  saveSettings();
  renderCraftingView();
};

window.deleteRecipe = function(recipeId) {
  if (!confirm('Delete this recipe?')) return;
  
  state.craftingRecipes = state.craftingRecipes.filter(r => r.id !== recipeId);
  saveSettings();
  renderCraftingView();
};

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
  const s = await api('/api/session/start', 'POST', { dungeon });
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
  }
};
es.onerror = () => { };

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

// First paint
refreshAll();
loadNPCList();
