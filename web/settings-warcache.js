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
