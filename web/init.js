// Startup — runs after all view modules are loaded
switchView('dashboard');
refreshAll();
loadNPCList();
loadAreas();

async function loadAreas() {
  const areas = await api('/api/areas');
  if (areas) state.areas = areas;
}
