async function renderHistory() {
  const container = $('#history-list');
  if (!container) return;
  try {
    container.innerHTML = '<div class="summary-empty">Loading sessions...</div>';

    const sessionsResp = await api('/api/sessions');
    if (!sessionsResp) {
      container.innerHTML = '<div class="summary-empty">Failed to load sessions</div>';
      return;
    }
    const sessions = Array.isArray(sessionsResp) ? sessionsResp : [];

  if (sessions.length === 0) {
    container.innerHTML = '<div class="summary-empty">No sessions yet. Start a session to see history.</div>';
    $('#history-total-sessions').textContent = '0 sessions';
    $('#history-total-value').textContent = '0g total';
    return;
  }

  const totalSessions = sessions.length;
  const totalValue = sessions.reduce((sum, s) => sum + (Number(s.total_value) || 0), 0);
  $('#history-total-sessions').textContent = `${totalSessions} session${totalSessions !== 1 ? 's' : ''}`;
  $('#history-total-value').textContent = `${totalValue.toFixed(0)}g total`;

  // Search/filter
  const q = ($('#history-search')?.value || '').toLowerCase();
  const filtered = q ? sessions.filter(s =>
    (s.dungeon || '').toLowerCase().includes(q) ||
    (s.notes || '').toLowerCase().includes(q)
  ) : sessions;

  const bulkMode = $('#bulk-toggle')?.checked;
  const selected = new Set(state._selectedSessions || []);

  container.innerHTML = '';
  if (filtered.length === 0) {
    container.innerHTML = '<div class="summary-empty">No sessions match your search</div>';
    return;
  }

  for (const session of filtered) {
    const card = document.createElement('div');
    card.className = 'history-card';
    if (!bulkMode) card.onclick = () => loadSessionDetail(session.id);

    const date = new Date(session.started_at);
    const dateStr = date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], {hour12: false, hour: '2-digit', minute: '2-digit'});

    const checked = selected.has(session.id) ? ' checked' : '';
    card.innerHTML = `
      ${bulkMode ? `<label class="bulk-check"><input type="checkbox" class="bulk-cb" data-id="${session.id}"${checked}></label>` : ''}
      <div class="history-card-body">
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
            <div class="history-stat-value value">${(Number(session.total_value) || 0).toFixed(0)}g</div>
          </div>
          <div class="history-stat">
            <div class="history-stat-label">Favor</div>
            <div class="history-stat-value">${session.favor_items}</div>
          </div>
          <div class="history-stat">
            <div class="history-stat-label">Sell</div>
            <div class="history-stat-value">${session.sell_items}</div>
          </div>
          ${session.total_gold ? `<div class="history-stat"><div class="history-stat-label">Gold</div><div class="history-stat-value">${session.total_gold}g</div></div>` : ''}
          ${session.deaths ? `<div class="history-stat"><div class="history-stat-label">Deaths</div><div class="history-stat-value">${session.deaths}</div></div>` : ''}
          </div>
        </div>
      `;
    container.appendChild(card);
  }

    // Track bulk selections
    if (bulkMode) {
      container.querySelectorAll('.bulk-cb').forEach(cb => {
        cb.addEventListener('change', () => {
          if (!state._selectedSessions) state._selectedSessions = new Set();
          const id = cb.dataset.id;
          if (cb.checked) state._selectedSessions.add(id);
          else state._selectedSessions.delete(id);
          updateBulkActions();
        });
      });
    }
  } catch (e) {
    console.error('renderHistory failed', e);
    container.innerHTML = `<div class="summary-empty">History render error: ${escapeHtml(e?.message || String(e))}</div>`;
  }
}

function updateBulkActions() {
  const count = state._selectedSessions?.size || 0;
  const actions = $('#bulk-actions');
  if (!actions) return;
  if (count > 0) {
    actions.classList.remove('hidden');
    actions.querySelector('#bulk-delete').textContent = `Delete (${count})`;
  } else {
    actions.classList.add('hidden');
  }
}

async function loadSessionDetail(sessionId) {
  const snapshot = await api(`/api/session/${sessionId}`);
  if (!snapshot) {
    toast('Failed to load session details', 'error');
    return;
  }

  state.historyDetail = snapshot;
  state.historyDetailId = sessionId;
  switchView('history-detail');
  renderHistoryDetail(snapshot);
}

function renderHistoryDetail(snapshot) {
  const date = new Date(snapshot.started_at);
  const dateStr = date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], {hour12: false, hour: '2-digit', minute: '2-digit'});

  const zoneName = zonePath(snapshot.zone || snapshot.dungeon || 'Unnamed Session');
  $('#history-detail-title').textContent = zoneName;
  $('#history-detail-dungeon').textContent = zoneName;
  $('#history-detail-date').textContent = dateStr;
  $('#history-detail-duration').textContent = fmtElapsed(new Date(snapshot.ended_at).getTime() - date.getTime());

  // Notes with edit
  const notesEl = $('#history-detail-notes');
  if (notesEl) {
    notesEl.innerHTML = snapshot.notes
      ? `<span class="detail-notes-text">${escapeHtml(snapshot.notes)}</span>
         <button class="edit-notes-btn" onclick="editHistoryNotes()">✏️</button>`
      : '<span class="detail-notes-text detail-notes-empty">No notes</span>' +
        '<button class="edit-notes-btn" onclick="editHistoryNotes()">✏️</button>';
  }

  const loot = snapshot.loot || [];
  let totalItems = 0;
  let totalValue = 0;
  let favorItems = 0;
  let sellItems = 0;

  for (const item of loot) {
    totalItems += item.count;
    totalValue += item.value * item.count;
    if (item.decision.verdict === 'favor') favorItems++;
    if (item.decision.verdict === 'sell_vendor' || item.decision.verdict === 'sell_consignment') sellItems++;
  }

  $('#history-detail-items').textContent = `${totalItems} (${loot.length} unique)`;
  $('#history-detail-value').textContent = `${totalValue.toFixed(0)}g`;
  $('#history-detail-favor').textContent = favorItems;
  $('#history-detail-sell').textContent = sellItems;

  // Update event stat cards
  const deaths = (snapshot.deaths || []).length;
  const gold = snapshot.total_gold || 0;
  const kills = (snapshot.kills || []).length;
  const xpCount = (snapshot.xp_gains || []).length;
  const lvlCount = (snapshot.level_ups || []).length;
  const gatherCount = (snapshot.gathering || []).length;

  const setText = (id, v) => { const el = $(id); if (el) el.textContent = v; };
  setText('#hd-stat-deaths', deaths);
  setText('#hd-stat-kills', kills);
  setText('#hd-stat-gold', gold + 'g');
  setText('#hd-stat-xp', xpCount);
  setText('#hd-stat-levels', lvlCount);
  setText('#hd-stat-gather', gatherCount);

  // Render tabs
  renderHistorySummary(loot);
  renderHistoryTimeline(snapshot);
  renderHistoryEvents(snapshot);

  // Render items tab
  const sortedLoot = [...loot].sort((a, b) => (b.value * b.count) - (a.value * a.count));
  const itemsList = $('#history-detail-items-list');
  itemsList.innerHTML = '';
  for (const item of sortedLoot) {
    const el = document.createElement('div');
    el.className = 'summary-item';
    el.innerHTML = `
      <span class="summary-item-name">${escapeHtml(item.name)}</span>
      <span class="summary-item-count">x${item.count}</span>
      <span class="summary-item-value">${(item.value * item.count).toFixed(0)}g</span>
      <span class="verdict ${item.decision.verdict}">${item.decision.verdict.replace(/_/g, ' ')}</span>
    `;
    itemsList.appendChild(el);
  }

  // Reset to summary tab
  activateHdTab('summary');
}

function renderHistorySummary(loot) {
  const favorItems = loot.filter(e => e.decision.verdict === 'favor');
  const sellItems = loot.filter(e => e.decision.verdict === 'sell_vendor' || e.decision.verdict === 'sell_consignment');
  const keepItems = loot.filter(e => e.decision.verdict === 'keep');

  renderHdFavorList(favorItems);
  renderHdSellList(sellItems);
  renderHdKeepList(keepItems);
}

function renderHdFavorList(items) {
  sharedRenderFavorList($('#hd-favor-list'), items);
  // history detail doesn't need the prioritize button; strip it
  $('#hd-favor-list')?.querySelectorAll('.pri-btn').forEach(b => b.remove());
}

function renderHdSellList(items) {
  sharedRenderSellList($('#hd-sell-list'), items, true);
}

function renderHdKeepList(items) {
  sharedRenderKeepList($('#hd-keep-list'), items);
}

function activateHdTab(tab) {
  $$('.hd-tab').forEach(t => t.classList.toggle('active', t.dataset.tab === tab));
  $$('.hd-panel').forEach(p => p.classList.toggle('hidden', p.id !== `hd-panel-${tab}`));
}

// Tab switching
$('.hd-tabs')?.addEventListener('click', (e) => {
  const tab = e.target.closest('.hd-tab');
  if (tab) activateHdTab(tab.dataset.tab);
});

window.editHistoryNotes = function() {
  const notesEl = $('#history-detail-notes');
  if (!notesEl) return;
  const current = state.historyDetail?.notes || '';
  notesEl.innerHTML = `
    <textarea id="notes-edit-textarea" rows="2">${escapeHtml(current)}</textarea>
    <button class="save-notes-btn" onclick="saveHistoryNotes()">Save</button>
    <button class="edit-notes-btn" onclick="renderHistoryDetail(state.historyDetail)">Cancel</button>
  `;
  $('#notes-edit-textarea').focus();
};

window.saveHistoryNotes = async function() {
  const notes = $('#notes-edit-textarea')?.value || '';
  const sessionId = state.historyDetailId;
  if (!sessionId) return;
  const res = await api(`/api/session/${sessionId}`, 'PATCH', { notes });
  if (res) {
    state.historyDetail = res;
    toast('Notes saved', 'success');
    renderHistoryDetail(res);
  }
};

window.deleteSession = async function(sessionId) {
  if (!confirm('Delete this session permanently?')) return;
  const res = await api(`/api/session/${sessionId}`, 'DELETE');
  if (res) {
    toast('Session deleted', 'success');
    switchView('history');
    renderHistory();
  }
};

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

$('#delete-session')?.addEventListener('click', () => {
  if (state.historyDetailId) deleteSession(state.historyDetailId);
});

// History search input
$('#history-search')?.addEventListener('input', () => {
  if (state.currentView === 'history') renderHistory();
});

// Bulk select toggle
$('#bulk-toggle')?.addEventListener('change', () => {
  if (!state._selectedSessions) state._selectedSessions = new Set();
  else state._selectedSessions.clear();
  updateBulkActions();
  if (state.currentView === 'history') renderHistory();
});

// Bulk export
$('#bulk-export')?.addEventListener('click', async () => {
  const ids = [...(state._selectedSessions || [])];
  if (ids.length === 0) return;
  try {
    const resp = await fetch('/api/sessions/bulk-export', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids }),
    });
    if (!resp.ok) { toast('Export failed', 'error'); return; }
    const blob = await resp.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `gorgon-sessions-${ids.length}.json`;
    a.click();
    URL.revokeObjectURL(url);
    toast(`Exported ${ids.length} sessions`, 'success');
  } catch (e) {
    toast('Export failed', 'error');
  }
});

// Bulk delete
$('#bulk-delete')?.addEventListener('click', async () => {
  const ids = [...(state._selectedSessions || [])];
  if (ids.length === 0) return;
  if (!confirm(`Delete ${ids.length} session${ids.length > 1 ? 's' : ''} permanently?`)) return;
  for (const id of ids) {
    await api(`/api/session/${id}`, 'DELETE');
  }
  state._selectedSessions = new Set();
  updateBulkActions();
  toast(`Deleted ${ids.length} session${ids.length > 1 ? 's' : ''}`, 'success');
  renderHistory();
});

function renderHistoryEvents(snapshot) {
  const container = $('#hd-events-content');
  if (!container) return;
  const parts = [];

  // XP gains grouped by skill
  const xp = snapshot.xp_gains || [];
  if (xp.length) {
    const bySkill = {};
    for (const g of xp) { bySkill[g.skill] = (bySkill[g.skill] || 0) + g.amount; }
    let html = '<div class="summary-group"><div class="summary-group-header npc">XP Gains</div>';
    for (const [skill, amt] of Object.entries(bySkill).sort((a, b) => b[1] - a[1])) {
      const cat = skillCategory(skill);
      const catHtml = cat ? ` <span class="muted">${escapeHtml(cat)}</span>` : '';
      html += `<div class="summary-item"><span class="summary-item-name">${escapeHtml(skill)}${catHtml}</span><span class="summary-item-value">+${amt} XP</span></div>`;
    }
    html += '</div>';
    parts.push(html);
  }

  // Level ups
  const lvls = snapshot.level_ups || [];
  if (lvls.length) {
    let html = '<div class="summary-group"><div class="summary-group-header" style="color:var(--favor)">Level Ups</div>';
    for (const l of lvls) {
      html += `<div class="summary-item"><span class="summary-item-name">${escapeHtml(l.skill)}</span><span class="summary-item-value">Level ${l.level}</span></div>`;
    }
    html += '</div>';
    parts.push(html);
  }

  // Kills
  const kills = snapshot.kills || [];
  if (kills.length) {
    const byMob = {};
    for (const k of kills) { byMob[k.mob] = (byMob[k.mob] || 0) + 1; }
    let html = '<div class="summary-group"><div class="summary-group-header" style="color:var(--sell)">Kills</div>';
    for (const [mob, cnt] of Object.entries(byMob).sort((a, b) => b[1] - a[1])) {
      html += `<div class="summary-item"><span class="summary-item-name">${escapeHtml(mob)}</span><span class="summary-item-value">x${cnt}</span></div>`;
    }
    html += '</div>';
    parts.push(html);
  }

  // Gathering
  const gather = snapshot.gathering || [];
  if (gather.length) {
    const byItem = {};
    for (const g of gather) { byItem[g.item] = (byItem[g.item] || 0) + g.count; }
    let html = '<div class="summary-group"><div class="summary-group-header" style="color:#2ecc71">Gathering</div>';
    for (const [item, cnt] of Object.entries(byItem).sort((a, b) => b[1] - a[1])) {
      html += `<div class="summary-item"><span class="summary-item-name">${escapeHtml(item)}</span><span class="summary-item-value">x${cnt}</span></div>`;
    }
    html += '</div>';
    parts.push(html);
  }

  // Deaths
  const deaths = snapshot.deaths || [];
  if (deaths.length) {
    let html = '<div class="summary-group"><div class="summary-group-header" style="color:#e74c3c">Deaths</div>';
    for (const d of deaths) {
      const t = new Date(d.time).toLocaleTimeString();
      html += `<div class="summary-item"><span class="summary-item-name">${t}</span></div>`;
    }
    html += '</div>';
    parts.push(html);
  }

  // Zone history
  const zones = snapshot.zone_history || [];
  if (zones.length) {
    let html = '<div class="summary-group"><div class="summary-group-header" style="color:#3498db">Zone Changes</div>';
    for (const z of zones) {
      const t = new Date(z.time).toLocaleTimeString();
      html += `<div class="summary-item"><span class="summary-item-name">${t}</span><span class="summary-item-value">${escapeHtml(z.zone)}</span></div>`;
    }
    html += '</div>';
    parts.push(html);
  }

  container.innerHTML = parts.length ? parts.join('') : '<div class="summary-empty">No events recorded for this session</div>';
}

function renderHistoryTimeline(snapshot) {
  const container = $('#hd-timeline-content');
  if (!container) return;
  const events = [];

  // Loot
  for (const l of (snapshot.loot || [])) {
    const t = l.last_seen || snapshot.started_at;
    events.push({ time: new Date(t), type: 'loot', label: `🏆 ${escapeHtml(l.name)} x${l.count}`, detail: `${(l.valor || 0).toFixed(0)}g` });
  }

  // XP
  for (const x of (snapshot.xp_gains || [])) {
    const cat = skillCategory(x.skill);
    const catLabel = cat ? ` (${cat})` : '';
    events.push({ time: new Date(x.time), type: 'xp', label: `📈 ${escapeHtml(x.skill)}${catLabel}`, detail: `+${x.amount} XP` });
  }

  // Deaths
  for (const d of (snapshot.deaths || [])) {
    events.push({ time: new Date(d.time), type: 'death', label: '💀 Died', detail: '' });
  }

  // Kills
  for (const k of (snapshot.kills || [])) {
    events.push({ time: new Date(k.time), type: 'kill', label: `⚔ Killed ${escapeHtml(k.mob)}`, detail: '' });
  }

  // Gathering
  for (const g of (snapshot.gathering || [])) {
    events.push({ time: new Date(g.time), type: 'gather', label: `🪓 Gathered ${escapeHtml(g.item)}`, detail: `x${g.count}` });
  }

  // Level ups
  for (const l of (snapshot.level_ups || [])) {
    events.push({ time: new Date(l.time), type: 'level', label: `⬆ ${escapeHtml(l.skill)}`, detail: `Level ${l.level}` });
  }

  // Zone changes
  for (const z of (snapshot.zone_history || [])) {
    events.push({ time: new Date(z.time), type: 'zone', label: `📍 ${escapeHtml(zonePath(z.zone))}`, detail: '' });
  }

  if (events.length === 0) {
    container.innerHTML = '<div class="summary-empty">No events recorded for this session</div>';
    return;
  }

  events.sort((a, b) => a.time - b.time);

  const typeColors = { loot: '#f1c40f', xp: '#2ecc71', death: '#e74c3c', kill: '#e67e22', gather: '#1abc9c', level: '#9b59b6', zone: '#3498db' };

  let html = '<div class="timeline">';
  for (const ev of events) {
    const color = typeColors[ev.type] || '#7a7f8a';
    const t = ev.time.toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit' });
    html += `<div class="timeline-item" style="border-left-color:${color}">
      <span class="timeline-time">${t}</span>
      <span class="timeline-label">${ev.label}</span>
      ${ev.detail ? `<span class="timeline-detail">${ev.detail}</span>` : ''}
    </div>`;
  }
  html += '</div>';
  container.innerHTML = html;
}
