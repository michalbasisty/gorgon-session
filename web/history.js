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

  // Search/filter
  const q = ($('#history-search')?.value || '').toLowerCase();
  const filtered = q ? sessions.filter(s =>
    (s.dungeon || '').toLowerCase().includes(q) ||
    (s.notes || '').toLowerCase().includes(q)
  ) : sessions;

  container.innerHTML = '';
  if (filtered.length === 0) {
    container.innerHTML = '<div class="summary-empty">No sessions match your search</div>';
    return;
  }

  for (const session of filtered) {
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

  $('#history-detail-title').textContent = snapshot.dungeon || 'Unnamed Session';
  $('#history-detail-dungeon').textContent = snapshot.dungeon || 'unnamed';
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
