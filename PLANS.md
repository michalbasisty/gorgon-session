# gorgon-session Expansion Plan (No VIP Required)

This plan lists realistic features we can add using only:

- game log files
- local app storage
- public Project Gorgon CDN data

No VIP-only mechanics required.

---

## Phase 1 — Stability & Quality (High priority)

- [ ] Add in-app diagnostics panel (API latency, pending count, last errors)
- [ ] Add one-click "Reset UI state" (clear filters/hidden sets safely)
- [ ] Add request metrics in backend logs (endpoint + duration)
- [ ] Add race-check CI (`go test -race ./...`)
- [ ] Add tests for config update semantics and session ID validation

---

## Phase 2 — Better Session Intelligence

- [ ] Session goals (target gold, target drops, target favor)
- [ ] Zone-specific performance summary (value/hour, deaths, kill pace)
- [ ] Compare sessions side-by-side (zone A vs zone B)
- [ ] Better timeline view (loot, zone changes, combat spikes)
- [ ] Session tags + fast filtering in history

---

## Phase 3 — Trader & Sell Optimization

- [ ] Route planner: best sell route from current zone
- [ ] Trader capacity warnings before reset
- [ ] "Use remaining capacity first" recommendations
- [ ] Multi-step sell checklist per session
- [ ] Estimated total sell outcome (vendor vs consignment split)

---

## Phase 4 — Loot Intelligence

- [ ] Keep/sell/favor rule presets by keyword/category
- [ ] Smart auto-notes (e.g., "consign", "craft material", "favor target")
- [ ] Loot rarity/value alert profiles per zone
- [ ] Better drop-rate math (confidence + min sample warnings)
- [ ] Historical trend view for key items

---

## Phase 5 — Crafting Support (No VIP)

- [ ] Recipe tracker tied to current inventory-like loot intake
- [ ] Material shortfall calculator for selected recipes
- [ ] "Loot useful for tracked recipes" highlight in tracker
- [ ] Skill-focused recipe view (what to farm for chosen skill)
- [ ] Craft queue planner (materials needed across multiple recipes)

---

## Phase 6 — Combat & Farming Efficiency

- [ ] Ability efficiency metrics (uses, hits, estimated contribution)
- [ ] Combat pacing timeline during session
- [ ] Death context summary (zone, recent events)
- [ ] Build practice dashboards (per-session combat trends)
- [ ] Overlay mini-panels with compact live stats

---

## Phase 7 — UX & Workflow

- [ ] Startup profile (default view, refresh cadence, compact mode)
- [ ] Better onboarding wizard for first run
- [ ] Export/import by feature scope (history only, traders only, prices only)
- [ ] Keyboard shortcuts help panel
- [ ] Optional lightweight theme variants

---

## Nice-to-have ideas

- [ ] Multi-character profile separation
- [ ] Session templates (pre-filled notes/goals by zone)
- [ ] Local "what changed since last run" summary
- [ ] Advanced search across all session reports

---

## Suggested implementation order

1. Phase 1 (stability)
2. Phase 3 (trader optimization)
3. Phase 2 (session insights)
4. Phase 4 (loot intelligence)
5. Phase 5 + 6 (crafting/combat depth)
6. Phase 7 (UX polish)

---

## Definition of done for each feature

- [ ] Works on clean start and after long runtime
- [ ] Handles missing/partial data safely
- [ ] Has at least one focused test (backend) or smoke flow (frontend)
- [ ] Includes small README/tooltip update for users
