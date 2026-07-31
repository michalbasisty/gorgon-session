function renderSummary(s) {
  const loot = s.loot || [];
  console.log('Summary loot data:', loot);
  const favorItems = loot.filter(e => e.decision.verdict === 'favor');
  const sellItems = loot.filter(e => e.decision.verdict === 'sell_vendor' || e.decision.verdict === 'sell_consignment');
  const keepItems = loot.filter(e => e.decision.verdict === 'keep');

  const zone = zonePath(s.zone || s.dungeon || 'unnamed');
  $('#sum-dungeon').textContent = zone;
  const dur = new Date(s.ended_at || Date.now()).getTime() - new Date(s.started_at).getTime();
  $('#sum-duration').textContent = fmtElapsed(dur);
  $('#sum-items').textContent = `${loot.length} unique items`;

  $('#favor-count').textContent = favorItems.length;
  $('#sell-count').textContent = sellItems.length;
  $('#keep-count').textContent = keepItems.length;

  // Event stats
  const deaths = (s.deaths || []).length;
  const gold = s.total_gold || 0;
  const kills = (s.kills || []).length;
  const xpCount = (s.xp_gains || []).length;
  const lvlCount = (s.level_ups || []).length;
  const gatherCount = (s.gathering || []).length;

  let extra = $('#sum-event-stats');
  if (!extra) {
    const ref = document.querySelector('.summary-grid');
    if (ref) {
      extra = document.createElement('div');
      extra.id = 'sum-event-stats';
      extra.className = 'summary-grid';
      extra.style.marginTop = '12px';
      ref.after(extra);
    }
  }
  if (extra) {
    extra.innerHTML = `
      <div class="summary-section" style="grid-column:1/-1">
        <h3>Session Events</h3>
        <div style="display:flex;gap:16px;flex-wrap:wrap;padding:8px 0">
          <span>💰 ${gold}g found</span>
          <span>💀 ${deaths} death${deaths !== 1 ? 's' : ''}</span>
          <span>⚔ ${kills} kill${kills !== 1 ? 's' : ''}</span>
          <span>🪓 ${gatherCount} gather${gatherCount !== 1 ? 's' : ''}</span>
          <span>⬆ ${lvlCount} level-up${lvlCount !== 1 ? 's' : ''}</span>
          <span>📊 ${xpCount} XP ticks</span>
        </div>
      </div>`;
  }

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
  sharedRenderFavorList($('#favor-list'), items);
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
  sharedRenderSellList($('#sell-list'), items);
}

// Combat breakdown modal. sessionId omitted → current/latest session.
window.showCombatBreakdown = async function(sessionId) {
  const q = sessionId ? '?session=' + encodeURIComponent(sessionId) : '';
  const data = await api('/api/combat/breakdown' + q);
  if (!data) return;
  const abilities = Array.isArray(data.abilities) ? data.abilities : [];
  const types = data.damage_types || {};

  const maxDmg = Math.max(...abilities.map(a => a.total_damage || 0), 1);
  const modal = document.createElement('div');
  modal.className = 'modal';
  modal.innerHTML = `
    <div class="modal-content" style="max-width:640px">
      <div class="modal-header">
        <h3>⚔ Combat Breakdown${sessionId ? '' : ' — current session'}</h3>
        <button class="modal-close">×</button>
      </div>
      <div class="modal-body">
        <h4>Abilities</h4>
        ${abilities.length === 0
          ? '<div class="summary-empty">No combat data for this session</div>'
          : `<table class="schedule-table"><thead><tr>
              <th>Ability</th><th style="text-align:right">Casts</th><th style="text-align:right">Damage</th>
              <th style="text-align:right">%</th><th style="min-width:140px">Share</th>
            </tr></thead><tbody>
            ${abilities.map(a => {
              const share = ((a.total_damage || 0) / maxDmg) * 100;
              return `<tr>
                <td>${escapeHtml(a.name || 'Unknown')}</td>
                <td style="text-align:right">${a.casts || 0}</td>
                <td style="text-align:right">${Math.round(a.total_damage || 0).toLocaleString()}</td>
                <td style="text-align:right">${(a.pct || 0).toFixed(1)}%</td>
                <td><div class="bar-track"><div class="bar-fill" style="width:${share.toFixed(1)}%"></div></div></td>
              </tr>`;
            }).join('')}
            </tbody></table>`}
        <h4 style="margin-top:16px">Damage Types</h4>
        <div id="cd-types"></div>
      </div>
    </div>
  `;
  document.body.appendChild(modal);
  modal.querySelector('.modal-close').onclick = () => modal.remove();
  modal.onclick = (e) => { if (e.target === modal) modal.remove(); };

  const typeEl = modal.querySelector('#cd-types');
  if (!typeEl) return;
  const entries = Object.entries(types).filter(([, v]) => v > 0);
  if (entries.length === 0) {
    typeEl.innerHTML = '<div class="summary-empty">No damage type data</div>';
    return;
  }
  const max = Math.max(...entries.map(([, v]) => v), 1);
  typeEl.innerHTML = entries.map(([k, v]) => {
    const pct = (v / max) * 100;
    return `<div class="bar-row">
      <span class="bar-label">${escapeHtml(k)}</span>
      <div class="bar-track"><div class="bar-fill" style="width:${pct.toFixed(1)}%"></div></div>
      <span class="bar-val">${Math.round(v).toLocaleString()}</span>
    </div>`;
  }).join('');
};

window.showCombatBreakdownForDetail = function() {
  showCombatBreakdown(state.historyDetailId);
};

function renderKeepList(items) {
  sharedRenderKeepList($('#keep-list'), items);
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
