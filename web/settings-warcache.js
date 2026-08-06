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
  $('#settings-player-log').value = cfg.player_log_path || '';
  $('#settings-loot-regex').value = cfg.loot_regex || '';
  $('#settings-sell-value-threshold').value = cfg.sell_value_threshold || 50;
  $('#settings-notification-threshold').value = cfg.notification_threshold || 500;
  $('#settings-backup-enabled').checked = cfg.backup_enabled !== false;

  // Overlay settings (cfg.overlay, added by the native-window lane)
  const ov = cfg.overlay || {};
  presetOverlayControls({
    opacity: ov.opacity ?? OVERLAY_DEFAULTS.opacity,
    click_through_opacity: ov.click_through_opacity ?? OVERLAY_DEFAULTS.click_through_opacity,
    click_through_by_default: ov.click_through_by_default ?? OVERLAY_DEFAULTS.click_through_by_default,
    position: ov.position || OVERLAY_DEFAULTS.position,
    theme: ov.theme || OVERLAY_DEFAULTS.theme,
    accent_color: ov.accent_color || OVERLAY_DEFAULTS.accent_color
  });
  
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
  const player_log_path = $('#settings-player-log').value.trim();
  const loot_regex = $('#settings-loot-regex').value.trim();
  const sell_value_threshold = parseFloat($('#settings-sell-value-threshold').value) || 0;
  const notification_threshold = parseFloat($('#settings-notification-threshold').value) || 500;

  const backup_enabled = $('#settings-backup-enabled').checked;

  const res = await api('/api/config', 'POST', {
    chat_log_dir,
    player_log_path,
    loot_regex,
    sell_value_threshold,
    player_prices: state.playerPrices,
    notification_threshold,
    backup_enabled
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

// ==================== Overlay settings ====================
// Contract (shared with the native window lane): cfg.overlay =
// { opacity, click_through_opacity, click_through_by_default, position,
//   theme, accent_color }. The API merges partial updates, so posting just
// the overlay object is fine. Opacity changes apply live to a running overlay;
// position, theme, and accent apply the next time it opens.

const OVERLAY_DEFAULTS = {
  opacity: 98,
  click_through_opacity: 78,
  click_through_by_default: false,
  position: 'bottom-right',
  theme: 'dark',
  accent_color: '#3FB950'
};

function presetOverlayControls(ov) {
  $('#overlay-opacity').value = ov.opacity;
  $('#overlay-click-opacity').value = ov.click_through_opacity;
  $('#overlay-click-through').checked = !!ov.click_through_by_default;
  $('#overlay-position').value = ov.position;
  $('#overlay-theme').value = ov.theme;
  $('#overlay-accent').value = ov.accent_color;
  $('#overlay-opacity-value').textContent = ov.opacity + '%';
  $('#overlay-click-opacity-value').textContent = ov.click_through_opacity + '%';
  updateSliderFill($('#overlay-opacity'));
  updateSliderFill($('#overlay-click-opacity'));
}

// Range-input filled track: sets a CSS custom property the gradient reads
function updateSliderFill(el) {
  const pct = ((el.value - el.min) / (el.max - el.min)) * 100;
  el.style.setProperty('--range-pct', pct + '%');
}

// Live % readouts + filled track + debounced autosave
let _overlaySaveTimer = null;
function scheduleOverlaySave() {
  clearTimeout(_overlaySaveTimer);
  _overlaySaveTimer = setTimeout(() => {
    const overlay = {
      opacity: parseInt($('#overlay-opacity').value, 10),
      click_through_opacity: parseInt($('#overlay-click-opacity').value, 10),
      click_through_by_default: $('#overlay-click-through').checked,
      position: $('#overlay-position').value,
      theme: $('#overlay-theme').value,
      accent_color: $('#overlay-accent').value
    };
    saveOverlaySettings(overlay);
  }, 300);
}

$('#overlay-opacity')?.addEventListener('input', e => {
  $('#overlay-opacity-value').textContent = e.target.value + '%';
  updateSliderFill(e.target);
  scheduleOverlaySave();
});
$('#overlay-click-opacity')?.addEventListener('input', e => {
  $('#overlay-click-opacity-value').textContent = e.target.value + '%';
  updateSliderFill(e.target);
  scheduleOverlaySave();
});

async function saveOverlaySettings(overlay) {
  try {
    // api() toasts the server error text itself when the POST fails
    return !!(await api('/api/config', 'POST', { overlay }));
  } catch (err) {
    toast('Failed to save overlay settings: ' + (err && err.message || err), 'error');
    return false;
  }
}

$('#overlay-save')?.addEventListener('click', async () => {
  const overlay = {
    opacity: parseInt($('#overlay-opacity').value, 10),
    click_through_opacity: parseInt($('#overlay-click-opacity').value, 10),
    click_through_by_default: $('#overlay-click-through').checked,
    position: $('#overlay-position').value,
    theme: $('#overlay-theme').value,
    accent_color: $('#overlay-accent').value
  };
  if (await saveOverlaySettings(overlay)) toast('Overlay settings saved', 'success');
});

$('#overlay-reset')?.addEventListener('click', async () => {
  if (await saveOverlaySettings(OVERLAY_DEFAULTS)) {
    presetOverlayControls(OVERLAY_DEFAULTS);
    toast('Overlay settings reset to defaults', 'success');
  }
});

// Player prices management
let ppFilter = '';
function renderPlayerPrices() {
  const container = $('#pp-list');
  if (!container) return;
  
  const entries = Object.entries(state.playerPrices);
  const totalCount = entries.length;
  const filtered = ppFilter ? entries.filter(([name]) => name.toLowerCase().includes(ppFilter.toLowerCase())) : entries;
  
  container.innerHTML = '';
  const count = $('#pp-count');
  if (count) count.textContent = `${totalCount} item${totalCount !== 1 ? 's' : ''}`;
  
  if (filtered.length === 0) {
    container.innerHTML = `<div class="summary-empty">${totalCount === 0 ? 'No player prices set' : 'No items match your filter'}</div>`;
    return;
  }
  
  for (const [name, price] of filtered) {
    const item = document.createElement('div');
    item.className = 'pp-item';
    item.dataset.name = name;
    item.innerHTML = `
      <span class="pp-name">${escapeHtml(name)}</span>
      <span class="pp-price">${price.toFixed(0)}g</span>
      <button class="pp-trends" title="Price history" onclick="showPriceTrends('${escapeHtml(name).replace(/'/g, "\\'")}')">📈</button>
      <button class="pp-delete" aria-label="Delete ${escapeHtml(name)}">×</button>
    `;
    container.appendChild(item);
  }
}

// Event delegation for delete buttons
$('#pp-list')?.addEventListener('click', async (e) => {
  const btn = e.target.closest('.pp-delete');
  if (!btn) return;
  const item = btn.closest('.pp-item');
  if (!item) return;
  const name = item.dataset.name;
  if (!name) return;
  
  delete state.playerPrices[name];
  saveSettings();
  renderPlayerPrices();
  
  // Persist to server
  pushPlayerPricesToServer();
});

async function pushPlayerPricesToServer() {
  await api('/api/config', 'POST', {
    chat_log_dir: $('#settings-chat-log-dir')?.value.trim() || '',
    player_log_path: $('#settings-player-log')?.value.trim() || '',
    loot_regex: $('#settings-loot-regex')?.value.trim() || '',
    sell_value_threshold: parseFloat($('#settings-sell-value-threshold')?.value) || 0,
    player_prices: state.playerPrices
  });
}

$('#pp-add')?.addEventListener('click', async () => {
  const nameEl = $('#pp-item-name');
  const priceEl = $('#pp-item-price');
  const name = nameEl.value.trim();
  const price = parseFloat(priceEl.value);
  if (!name || isNaN(price) || price <= 0) { toast('Enter a valid item name and price', 'error'); return; }
  
  state.playerPrices[name] = price;
  saveSettings();
  renderPlayerPrices();
  nameEl.value = '';
  priceEl.value = '';
  nameEl.focus();
  
  await pushPlayerPricesToServer();
});

// Allow Enter key on price input to trigger Add
$('#pp-item-price')?.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') $('#pp-add')?.click();
});

// Search filter
$('#pp-search')?.addEventListener('input', (e) => {
  ppFilter = e.target.value;
  renderPlayerPrices();
});

// Export/Import
$('#export-data')?.addEventListener('click', () => {
  window.location.href = '/api/export';
  toast('Downloading settings export...', 'info');
});

$('#import-data')?.addEventListener('click', () => {
  $('#import-file')?.click();
});

$('#import-file')?.addEventListener('change', async (e) => {
  const file = e.target.files[0];
  if (!file) return;
  const status = $('#import-status');
  status.textContent = 'Importing...';
  status.className = 'settings-status';
  try {
    const text = await file.text();
    const data = JSON.parse(text);
    const res = await api('/api/import', 'POST', data);
    if (res) {
      status.textContent = res.message || 'Import successful!';
      status.className = 'settings-status success';
      // Reload settings view
      setTimeout(() => renderSettingsView(), 1000);
    }
  } catch (err) {
    status.textContent = 'Import failed: ' + err.message;
    status.className = 'settings-status error';
  }
  e.target.value = '';
});

// ==================== Warcache Solver ====================

const SYM_LABELS = ['1','2','3','4','5','6','7','8','9','10','11','12'];
const NUM_SYMS = 12;
const SLOT_COUNT = 4;

let warcachePossibilities = [];
let warcacheGuess = [null, null, null, null];
let warcacheSelectedSlot = 0;
let warcacheHistory = [];
let warcacheCachedGuess = null;

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
  warcacheCachedGuess = null;
  const [fc, fw] = feedback;
  warcachePossibilities = warcachePossibilities.filter(p => {
    const [c, w] = warcacheComputeFeedback(guess, p);
    return c === fc && w === fw;
  });
}

function warcacheSuggest() {
  if (warcachePossibilities.length === 0) return null;
  if (warcachePossibilities.length === 1) return warcachePossibilities[0];
  
  // Skip minimax when space is large — first guess is info-theoretically
  // equivalent regardless, and minimax over 20K possibilities blocks the UI.
  if (warcachePossibilities.length > 2000) {
    if (!warcacheCachedGuess) warcacheCachedGuess = warcachePossibilities[Math.floor(Math.random() * warcachePossibilities.length)];
    return warcacheCachedGuess;
  }
  
  // Minimax: find guess that minimizes max remaining possibilities
  let bestGuess = null;
  let minMaxRemaining = Infinity;
  
  for (const guess of warcachePossibilities) {
    const feedbackCounts = {};
    for (const possibility of warcachePossibilities) {
      const [c, w] = warcacheComputeFeedback(guess, possibility);
      const key = `${c},${w}`;
      feedbackCounts[key] = (feedbackCounts[key] || 0) + 1;
    }
    
    const maxRemaining = Math.max(...Object.values(feedbackCounts));
    
    if (maxRemaining < minMaxRemaining) {
      minMaxRemaining = maxRemaining;
      bestGuess = guess;
    }
  }
  
  return bestGuess;
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
    toast('Solved! The answer is ' + warcacheGuess.map(s => SYM_LABELS[s]).join(' '), 'success');
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

// Game overlay buttons
$('#open-overlay')?.addEventListener('click', () => { openOverlay(); });

$('#copy-overlay-url')?.addEventListener('click', () => {
  const url = window.location.origin + '/?overlay=1';
  navigator.clipboard.writeText(url).then(() => {
    toast('Overlay URL copied', 'success');
  }).catch(() => {
    // Fallback
    const ta = document.createElement('textarea');
    ta.value = url;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand('copy');
    ta.remove();
    toast('Overlay URL copied', 'success');
  });
});

// Reset
$('#warcache-reset')?.addEventListener('click', () => {
  warcachePossibilities = warcacheGenerateAll();
  warcacheGuess = [null, null, null, null];
  warcacheSelectedSlot = 0;
  warcacheHistory = [];
  warcacheCachedGuess = null;
  $('#warcache-feedback').value = '';
  warcacheRenderGuessSlots();
  warcacheUpdateSuggestion();
  warcacheRenderHistory();
});

// ==================== Price Trends ====================

// Modal with a pure CSS bar chart of the last 50 price observations.
window.showPriceTrends = async function(name) {
  const data = await api('/api/prices/trends?name=' + encodeURIComponent(name));
  if (!data) return;
  const entries = Array.isArray(data.entries) ? data.entries : [];

  const modal = document.createElement('div');
  modal.className = 'modal';
  modal.innerHTML = `
    <div class="modal-content">
      <div class="modal-header">
        <h3>📈 ${escapeHtml(data.item || name)} — price trends</h3>
        <button class="modal-close">×</button>
      </div>
      <div class="modal-body">
        ${entries.length === 0
          ? '<div class="summary-empty">No price observations yet</div>'
          : `<div class="trends-chart" id="trends-chart"></div>
             <div class="trends-meta">${entries.length} observation${entries.length !== 1 ? 's' : ''} (last 50) · hover a bar for details</div>`}
      </div>
    </div>
  `;
  document.body.appendChild(modal);
  modal.querySelector('.modal-close').onclick = () => modal.remove();
  modal.onclick = (e) => { if (e.target === modal) modal.remove(); };
  if (entries.length === 0) return;

  const max = Math.max(...entries.map(e => e.price || 0), 1);
  const chart = modal.querySelector('#trends-chart');
  chart.innerHTML = entries.map(e => {
    const h = Math.max(2, ((e.price || 0) / max) * 100);
    const label = new Date(e.t).toLocaleString();
    return `<div class="trends-bar-wrap" title="${escapeHtml(label)} — ${(e.price || 0).toFixed(0)}g (qty ${e.qty || 0})">
      <div class="trends-bar" style="height:${h.toFixed(1)}%"></div>
    </div>`;
  }).join('');
};

// ==================== Profit Calculator ====================

// Skill select is populated lazily because state.skills loads asynchronously.
function ensureProfitSkills() {
  const select = $('#profit-skill');
  if (!select || select.options.length > 1) return;
  for (const s of Object.keys(state.skills || {}).sort()) {
    const opt = document.createElement('option');
    opt.value = s;
    opt.textContent = s;
    select.appendChild(opt);
  }
}

window.calcProfit = async function() {
  ensureProfitSkills();
  const skill = $('#profit-skill')?.value || '';
  const maxLevel = parseInt($('#profit-max-level')?.value) || 0;
  const container = $('#profit-results');
  if (!container) return;
  container.innerHTML = '<div class="summary-empty" style="padding:12px">Calculating...</div>';
  const data = await api(`/api/crafting/profit?skill=${encodeURIComponent(skill)}&max_level=${maxLevel}`);
  if (!data) { container.innerHTML = ''; return; }
  const recipes = Array.isArray(data.recipes) ? data.recipes : [];
  if (recipes.length === 0) {
    container.innerHTML = '<div class="summary-empty" style="padding:12px">No recipes found</div>';
    return;
  }
  container.innerHTML = `<table class="schedule-table"><thead><tr>
    <th>Recipe</th><th>Lv</th><th>Ingredients</th><th style="text-align:right">Cost</th>
    <th style="text-align:right">Sell</th><th style="text-align:right">Profit</th><th style="text-align:right">Margin</th>
  </tr></thead><tbody>
    ${recipes.map(r => {
      const ings = (r.ingredients || [])
        .map(i => `${escapeHtml(i.name)} x${i.qty}${i.cost ? ` (${i.cost.toFixed(0)}g)` : ''}`)
        .join(', ') || '—';
      const unknown = r.cost_unknown ? ' <span class="value-unknown">value unknown</span>' : '';
      return `<tr>
        <td>${escapeHtml(r.name || 'Unnamed')}${unknown}</td>
        <td>${r.level ?? '?'}</td>
        <td style="color:var(--muted);font-size:12px">${ings}</td>
        <td style="text-align:right">${(r.ingredients_cost || 0).toFixed(0)}g</td>
        <td style="text-align:right">${(r.sell_value || 0).toFixed(0)}g</td>
        <td style="text-align:right;font-weight:600;color:${r.profit >= 0 ? 'var(--favor)' : '#e74c3c'}">${r.profit >= 0 ? '+' : ''}${r.profit.toFixed(0)}g</td>
        <td style="text-align:right">${r.cost_unknown ? '—' : (r.margin_pct ? r.margin_pct.toFixed(1) + '%' : '—')}</td>
      </tr>`;
    }).join('')}
  </tbody></table>`;
  toast(`Top ${recipes.length} recipes by profit`, 'info');
};

$('#profit-calc')?.addEventListener('click', calcProfit);
$('#profit-max-level')?.addEventListener('keydown', e => { if (e.key === 'Enter') calcProfit(); });
