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

  // Sort: prioritized NPCs first, then by total favor descending
  const sorted = [...byNPC.entries()].sort((a, b) => {
    const aPri = state.prioritizedNPCs.has(a[0].split(' (')[0]) ? 0 : 1;
    const bPri = state.prioritizedNPCs.has(b[0].split(' (')[0]) ? 0 : 1;
    if (aPri !== bPri) return aPri - bPri;
    const aFav = a[1].reduce((s, e) => {
      const t = (e.decision.favor_targets || []).filter(x => !state.disabledNPCs.has(x.npc));
      return s + (t.length > 0 ? t[0].score : 0) * e.count;
    }, 0);
    const bFav = b[1].reduce((s, e) => {
      const t = (e.decision.favor_targets || []).filter(x => !state.disabledNPCs.has(x.npc));
      return s + (t.length > 0 ? t[0].score : 0) * e.count;
    }, 0);
    return bFav - aFav;
  });

  for (const [npc, entries] of sorted) {
    const group = document.createElement('div');
    group.className = 'summary-group';

    const totalFavor = entries.reduce((sum, e) => {
      const targets = (e.decision.favor_targets || []).filter(t => !state.disabledNPCs.has(t.npc));
      const score = targets.length > 0 ? targets[0].score : 0;
      return sum + score * e.count;
    }, 0);

    // Check trader capacity for this NPC
    const npcName = npc.split(' (')[0];
    const cap = state.traderCapacity[npcName];
    const broke = cap && cap.remaining <= 0 && cap.limit > 0;
    const isPri = state.prioritizedNPCs.has(npcName);

    group.innerHTML = `<div class="summary-group-header npc">
      <button class="pri-btn${isPri ? ' active' : ''}" onclick="togglePrioritizeNPC('${escapeHtml(npcName).replace(/'/g, "\\'")}')" title="Prioritize this NPC">★</button>
      ${escapeHtml(npc)} <span style="float:right;color:var(--muted);font-weight:normal">${entries.length} items · ${totalFavor.toFixed(1)} favor${broke ? ' · <span style="color:#e74c3c">⚠ no gold left (' + cap.reset + ')</span>' : ''}</span>
    </div>`;

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

window.togglePrioritizeNPC = function(name) {
  const decoded = name.replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"');
  if (state.prioritizedNPCs.has(decoded)) {
    state.prioritizedNPCs.delete(decoded);
  } else {
    state.prioritizedNPCs.add(decoded);
  }
  saveSettings();
  if (state.session) renderSummary(state.session);
};

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
