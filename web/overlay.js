// Minimal standalone overlay HUD. The overlay page does not load shared.js
// (its startup code assumes elements that don't exist here), so these helpers
// are re-declared locally.
async function api(path) {
  try {
    const r = await fetch(path);
    if (!r.ok) return null;
    return await r.json();
  } catch (e) {
    return null;
  }
}

function escapeHtml(s) {
  return String(s == null ? '' : s).replace(/[&<>"]/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}

function fmtElapsed(ms) {
  const s = Math.floor((ms || 0) / 1000);
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
  if (h) return `${h}h${String(m).padStart(2, '0')}m`;
  if (m) return `${m}m${String(sec).padStart(2, '0')}s`;
  return `${sec}s`;
}
const fmtNum = v => (Number(v) || 0).toLocaleString();

const els = {
  state: document.getElementById('ov-state'),
  elapsed: document.getElementById('ov-elapsed'),
  dps: document.getElementById('ov-dps'),
  loot: document.getElementById('ov-loot'),
  traders: document.getElementById('ov-traders-count'),
  capacity: document.getElementById('ov-capacity'),
  schedule: document.getElementById('ov-schedule'),
};

let lastSession = null;

async function poll() {
  const [s, combat, traders, schedule] = await Promise.all([
    api('/api/session'),
    api('/api/combat'),
    api('/api/traders'),
    api('/api/traders/schedule'),
  ]);
  lastSession = s || lastSession;
  renderSession(s || lastSession);
  renderCombat(combat, s || lastSession);
  renderTraders(traders, schedule);
}

function renderSession(s) {
  if (!s || !els.state) return;
  const state = s.state || 'idle';
  els.state.textContent = state;
  els.state.className = 'ov-badge ' + state;
  if (state === 'running') {
    els.elapsed.textContent = fmtElapsed(Date.now() - new Date(s.started_at).getTime());
  } else if (state === 'stopped') {
    els.elapsed.textContent = 'ended';
  } else {
    els.elapsed.textContent = '—';
  }
  const loot = Array.isArray(s.loot) ? s.loot : [];
  const total = loot.reduce((sum, l) => sum + (Number(l.value) || 0) * (Number(l.count) || 0), 0);
  els.loot.textContent = fmtNum(Math.round(total)) + 'g';
}

function renderCombat(combat, s) {
  if (!els.dps) return;
  const running = s && s.state === 'running';
  if (!running || !Array.isArray(combat) || combat.length === 0) {
    els.dps.textContent = '—';
    return;
  }
  const dps = combat.reduce((sum, a) => sum + (Number(a.est_dps) || 0), 0);
  els.dps.textContent = fmtNum(Math.round(dps));
}

function renderTraders(traders, schedule) {
  if (!els.traders) return;
  if (!Array.isArray(traders)) return;
  let count = 0, capacity = 0;
  for (const area of traders) {
    for (const npc of (Array.isArray(area && area.npcs) ? area.npcs : [])) {
      count++;
      capacity += Math.max(0, (Number(npc.weekly_limit) || 0) - (Number(npc.sold_this_week) || 0));
    }
  }
  els.traders.textContent = fmtNum(count);
  els.capacity.textContent = fmtNum(Math.round(capacity));
  const rows = Array.isArray(schedule) ? schedule : [];
  if (rows.length && els.schedule) {
    const next = rows[0]; // schedule is sorted closest-refresh-first
    els.schedule.textContent = `next refresh: ${escapeHtml(next.npc_name || '?')} in ${escapeHtml(next.time_until || '?')}`;
  } else if (els.schedule) {
    els.schedule.textContent = 'no refresh schedule';
  }
}

// Live elapsed tick while a session is running.
setInterval(() => {
  if (lastSession && lastSession.state === 'running') {
    els.elapsed.textContent = fmtElapsed(Date.now() - new Date(lastSession.started_at).getTime());
  }
}, 1000);

// Controls
document.getElementById('ov-clickthrough').addEventListener('click', () => {
  const on = document.body.classList.toggle('clickthrough');
  const btn = document.getElementById('ov-clickthrough');
  btn.classList.toggle('active', on);
  btn.title = on
    ? 'Click-through ON — click to restore'
    : 'Toggle click-through (window won\'t block game clicks)';
});

document.getElementById('ov-close').addEventListener('click', () => window.close());

poll();
setInterval(poll, 5000);
