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
  if (!container) return;
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

$('#trader-search')?.addEventListener('input', () => {
  if (state.currentView === 'traders') renderTradersView();
});

window.toggleHideArea = function(area) {
  if (state.hiddenAreas.has(area)) {
    state.hiddenAreas.delete(area);
  } else {
    state.hiddenAreas.add(area);
  }
  saveSettings();
  renderTradersView();
};

window.toggleHideTrader = function(name) {
  const decoded = new DOMParser().parseFromString(name, 'text/html').body.textContent;
  if (state.hiddenTraders.has(decoded)) {
    state.hiddenTraders.delete(decoded);
  } else {
    state.hiddenTraders.add(decoded);
  }
  saveSettings();
  renderTradersView();
};

window.toggleShowHiddenOnly = function() {
  state.showHiddenOnly = !state.showHiddenOnly;
  const btn = $('#show-hidden-btn');
  if (btn) btn.classList.toggle('active', state.showHiddenOnly);
  renderTradersView();
};

// Traders view functions
async function renderTradersView() {
  const container = $('#traders-list');
  if (!container) return;
  
  container.innerHTML = '<div class="summary-empty">Loading traders...</div>';
  
  const areas = await api('/api/traders');
  if (!areas) {
    container.innerHTML = '<div class="summary-empty">Failed to load traders</div>';
    return;
  }
  
  const search = ($('#trader-search')?.value || '').toLowerCase();
  const showHiddenOnly = state.showHiddenOnly;
  
  // Sort areas alphabetically
  areas.sort((a, b) => a.area.localeCompare(b.area));
  
  container.innerHTML = '';
  const shopSet = new Set(state.shopNPCs);
  const hiddenNpcs = [];
  const esc = s => escapeHtml(s).replace(/'/g, "\\'");
  
  // Sync button state
  const btn = $('#show-hidden-btn');
  if (btn) btn.classList.toggle('active', showHiddenOnly);
  
  for (const areaData of areas) {
    // Sort NPCs by name
    areaData.npcs.sort((a, b) => a.npc_name.localeCompare(b.npc_name));
    const areaName = esc(areaData.area);
    const areaHidden = state.hiddenAreas.has(areaData.area);
    
    // Collect hidden traders
    const hiddenInArea = areaData.npcs.filter(n => state.hiddenTraders.has(n.npc_name));
    for (const npc of hiddenInArea) {
      hiddenNpcs.push({ ...npc, area: areaData.area });
    }
    
    // In showHiddenOnly mode, skip the main list entirely
    if (showHiddenOnly) continue;
    
    // Filter visible traders
    const visibleNpcs = areaData.npcs.filter(n => !state.hiddenTraders.has(n.npc_name));
    
    // Filter by search
    const filtered = search
      ? visibleNpcs.filter(n => n.npc_name.toLowerCase().includes(search) || areaData.area.toLowerCase().includes(search))
      : visibleNpcs;
    
    // Skip hidden areas (unless searching)
    if (!search && areaHidden) continue;
    
    if (filtered.length === 0) continue;
    
    const areaSection = document.createElement('div');
    areaSection.className = 'trader-area-section';
    
    const header = document.createElement('div');
    header.className = 'trader-area-header';
    header.innerHTML = `
      <span>${escapeHtml(areaData.area)} <span class="badge">${filtered.length}</span></span>
      <div class="area-header-actions">
        <button class="hide-btn" title="Hide region" onclick="toggleHideArea('${areaName}')">👁</button>
        <span class="collapse-icon">▼</span>
      </div>
    `;
    header.onclick = (e) => {
      if (e.target.closest('.hide-btn')) return;
      const content = areaSection.querySelector('.trader-area-content');
      const icon = header.querySelector('.collapse-icon');
      if (content.style.display === 'none') {
        content.style.display = 'block';
        icon.textContent = '▼';
      } else {
        content.style.display = 'none';
        icon.textContent = '▶';
      }
    };
    areaSection.appendChild(header);
    
    const content = document.createElement('div');
    content.className = 'trader-area-content';
    
    for (const npc of filtered) {
      const remaining = Math.max(0, (npc.weekly_limit || 0) - (npc.sold_this_week || 0));
      const isShop = shopSet.has(npc.npc_name);
      
      const row = document.createElement('div');
      row.className = 'trader-row' + (npc.unused_warning ? ' unused-warning' : '');
      row.dataset.npcName = npc.npc_name;
      row.dataset.area = areaData.area;
      
      const name = esc(npc.npc_name);
      const area = esc(areaData.area);
      
      row.innerHTML = `
        <div class="trader-row-top">
          <label class="npc-toggle" title="Mark as shop NPC">
            <input type="checkbox" ${isShop ? 'checked' : ''} onchange="toggleShopNPC('${name}')">
            <span class="slider"></span>
          </label>
          <div class="trader-name">${escapeHtml(npc.npc_name)}</div>
          <div class="trader-capacity">
            <span class="capacity-label">Remaining:</span>
            <span class="capacity-value${remaining <= 0 ? ' zeroed' : ''}">${remaining.toLocaleString()}g</span>
          </div>
          <div class="trader-reset">
            <span class="reset-timer">${npc.time_until_reset}</span>
            ${npc.unused_warning ? '<span class="unused-badge">⚠ Unused</span>' : ''}
          </div>
          <button class="hide-btn" title="Hide trader" onclick="toggleHideTrader('${name}')">👁</button>
        </div>
        <div class="trader-row-bottom">
          <label>Limit: <input type="number" class="limit-input" value="${npc.weekly_limit || 0}" min="0" step="1000"></label>
          <label>Left: <input type="number" class="left-input" value="${remaining}" min="0" step="100"></label>
          <label>Reset Days: <input type="number" class="days-input" value="${npc.reset_days || 5}" min="0" max="30"></label>
          <label>Reset Hours: <input type="number" class="hours-input" value="${npc.reset_hours || 22}" min="0" max="23"></label>
          <button class="save-btn" onclick="saveTraderRow(this)">💾 Save</button>
        </div>
      `;
      // Show save button when user edits any field
      row.querySelectorAll('.trader-row-bottom input').forEach(inp => {
        inp.addEventListener('input', () => {
          const btn = row.querySelector('.save-btn');
          btn.style.display = '';
          btn.textContent = '💾 Save';
          btn.disabled = false;
        });
      });
      content.appendChild(row);
    }
    
    areaSection.appendChild(content);
    container.appendChild(areaSection);
  }
  
  // Hidden section
  if (showHiddenOnly || state.hiddenAreas.size > 0 || hiddenNpcs.length > 0) {
    const hiddenSection = document.createElement('div');
    hiddenSection.className = 'trader-area-section hidden-section';
    
    // Collect all hidden NPCs: from hiddenTraders + from hidden areas
    const allHidden = [...hiddenNpcs];
    const allNames = new Set(allHidden.map(n => n.npc_name));
    for (const area of state.hiddenAreas) {
      const areaData = areas.find(a => a.area === area);
      if (areaData) {
        for (const npc of areaData.npcs) {
          if (!allNames.has(npc.npc_name)) {
            allHidden.push({ ...npc, area });
            allNames.add(npc.npc_name);
          }
        }
      }
    }
    
    const totalHidden = state.hiddenAreas.size + allHidden.length;
    const header = document.createElement('div');
    header.className = 'trader-area-header hidden-header';
    header.innerHTML = `
      <span>Hidden <span class="badge">${totalHidden}</span></span>
      <span class="collapse-icon">${showHiddenOnly ? '▼' : '▶'}</span>
    `;
    if (!showHiddenOnly) {
      header.onclick = () => {
        const content = hiddenSection.querySelector('.trader-area-content');
        const icon = header.querySelector('.collapse-icon');
        if (content.style.display === 'none') {
          content.style.display = 'block';
          icon.textContent = '▼';
        } else {
          content.style.display = 'none';
          icon.textContent = '▶';
        }
      };
    }
    hiddenSection.appendChild(header);
    
    const content = document.createElement('div');
    content.className = 'trader-area-content';
    content.style.display = showHiddenOnly ? 'block' : 'none';
    
    // Hidden areas
    for (const area of state.hiddenAreas) {
      const row = document.createElement('div');
      row.className = 'trader-row hidden-item-row';
      row.innerHTML = `
        <div class="trader-row-top">
          <div class="trader-name">${escapeHtml(area)}</div>
          <span class="hidden-type-badge">region</span>
          <button class="unhide-btn" onclick="toggleHideArea('${esc(area)}')">👁‍🗨</button>
        </div>
      `;
      content.appendChild(row);
    }
    
    // Hidden traders
    for (const npc of allHidden) {
      const row = document.createElement('div');
      row.className = 'trader-row hidden-item-row';
      const name = esc(npc.npc_name);
      row.innerHTML = `
        <div class="trader-row-top">
          <div class="trader-name">${escapeHtml(npc.npc_name)}</div>
          <div class="trader-area-label">${escapeHtml(npc.area)}</div>
          <button class="unhide-btn" onclick="toggleHideTrader('${name}')">👁‍🗨</button>
        </div>
      `;
      content.appendChild(row);
    }
    
    hiddenSection.appendChild(content);
    container.appendChild(hiddenSection);
  }
  
  if (container.children.length === 0) {
    container.innerHTML = '<div class="summary-empty">No traders found</div>';
  }
}

// Save all fields for a trader row
window.saveTraderRow = async function(btn) {
  const row = btn.closest('.trader-row');
  const npcName = row.dataset.npcName;
  const area = row.dataset.area;
  const limit = Math.max(0, parseFloat(row.querySelector('.limit-input').value) || 0);
  const left = Math.max(0, parseFloat(row.querySelector('.left-input').value) || 0);
  const days = Math.min(30, Math.max(0, parseInt(row.querySelector('.days-input').value) || 0));
  const hours = Math.min(23, Math.max(0, parseInt(row.querySelector('.hours-input').value) || 0));
  row.querySelector('.limit-input').value = limit;
  row.querySelector('.left-input').value = left;
  row.querySelector('.days-input').value = days;
  row.querySelector('.hours-input').value = hours;
  const sold = Math.max(0, limit - left);
  
  btn.disabled = true;
  btn.textContent = 'Saving...';
  const res = await api('/api/traders', 'POST', {
    npc_name: npcName,
    area: area,
    weekly_limit: limit,
    sold: sold,
    reset_days: days,
    reset_hours: hours
  });
  if (!res) {
    btn.textContent = '💾 Save';
    btn.disabled = false;
    return;
  }
  await loadTraderCapacity();
  refreshCapacityDisplay(npcName);
  btn.textContent = '✓ Saved';
  btn.disabled = false;
  setTimeout(() => { btn.textContent = '💾 Save'; }, 1500);
};

// Update the remaining display for a specific NPC without rebuilding the view
function refreshCapacityDisplay(npcName) {
  const cap = state.traderCapacity[npcName];
  if (!cap) return;
  const remaining = cap.remaining;
  
  // Find the row by NPC name and update capacity value
  document.querySelectorAll('.trader-name').forEach(el => {
    if (el.textContent === npcName) {
      const capVal = el.closest('.trader-row-top')?.querySelector('.capacity-value');
      if (capVal) {
        capVal.textContent = remaining.toLocaleString() + 'g';
        capVal.classList.toggle('zeroed', remaining <= 0);
      }
      const capLabel = el.closest('.trader-row-top')?.querySelector('.capacity-label');
      if (capLabel) {
        capLabel.textContent = 'Remaining:';
      }
    }
  });
}

window.logSale = async function(npcName) {
  const amount = prompt(`How much did you sell to ${npcName}?`);
  if (!amount) return;
  
  const value = parseFloat(amount);
  if (isNaN(value) || value <= 0) {
    alert('Invalid amount');
    return;
  }
  
  await api('/api/traders', 'POST', {
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
