// Startup — runs after all view modules are loaded
switchView('dashboard');
refreshAll().catch(e => toast('Startup: ' + e.message, 'error'));
loadNPCList().catch(e => toast('NPCs: ' + e.message, 'error'));
loadAreas().catch(e => toast('Areas: ' + e.message, 'error'));
loadSkills().catch(e => toast('Skills: ' + e.message, 'error'));
loadRecipes().catch(e => toast('Recipes: ' + e.message, 'error'));
loadItems().catch(e => toast('Items: ' + e.message, 'error'));

async function loadAreas() {
  const areas = await api('/api/areas');
  if (areas) state.areas = areas;
}

async function loadSkills() {
  const skills = await api('/api/skills');
  if (skills) state.skills = skills;
}

async function loadRecipes() {
  const recipes = await api('/api/recipes');
  if (recipes) state.recipes = recipes;
}

async function loadItems() {
  const items = await api('/api/items');
  if (items) {
    state.itemNames = {};
    for (const i of items) state.itemNames[i.ItemID] = i.Name;
  }
}
