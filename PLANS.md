# gorgon-session Expansion Plan (No VIP Required)

Features added using only game log files, local app storage, and public Project Gorgon CDN data. No VIP-only mechanics required.

---

## ✅ Done (shipped)

- **Route planner** — best sell/favor route from the current session's items (`/api/route-planner`, route plan panel in the Tracker view).
- **Trader capacity warnings** — the Shops & Traders view shows each trader's remaining weekly capacity.
- **Compare sessions side-by-side** (zone A vs zone B) — `/api/sessions/compare`.
- **Historical trend view for key items** — `/api/prices/trends`.
- **Kill tracking** — mob kills from `corpse_search` lines (no VIP required).
- **Overlay mini-panels with compact live stats** — native WebView2 always-on-top overlay.
- **Export/import by feature scope** — settings export (`/api/export`, `/api/import`), trader history CSV, notes export, sessions bulk export.
- **Tests for config update semantics and session ID validation** — `internal/config/config_test.go`, `internal/server/server_test.go`.

---

## ⏳ Not yet done

- Session goals (target gold, target drops, target favor)
- Zone-specific performance summary (value/hour, deaths, kill pace)
- Better timeline view (loot, zone changes)
- Session tags + fast filtering in history
- "Use remaining capacity first" recommendations
- Multi-step sell checklist per session
- Estimated total sell outcome (vendor vs consignment split)
- Keep/sell/favor rule presets by keyword/category
- Smart auto-notes (e.g., "consign", "craft material", "favor target")
- Loot rarity/value alert profiles per zone
- Better drop-rate math (confidence + min sample warnings)
- Recipe tracker tied to current inventory-like loot intake
- Material shortfall calculator for selected recipes
- "Loot useful for tracked recipes" highlight in tracker
- Skill-focused recipe view (what to farm for chosen skill)
- Craft queue planner (materials needed across multiple recipes)
- Death context summary (zone, recent events)
- Build practice dashboards (per-session trends)
- Startup profile (default view, refresh cadence, compact mode)
- Better onboarding wizard for first run
- Keyboard shortcuts help panel
- Optional lightweight theme variants
- In-app diagnostics panel (API latency, pending count, last errors)
- One-click "Reset UI state" (clear filters/hidden sets safely)
- Request metrics in backend logs (endpoint + duration)
- Race-check CI (`go test -race ./...`)
- Multi-character profile separation
- Session templates (pre-filled notes/goals by zone)
- Local "what changed since last run" summary
- Advanced search across all session reports

---

## Definition of done for each feature

- Works on clean start and after long runtime
- Handles missing/partial data safely
- Has at least one focused test (backend) or smoke flow (frontend)
- Includes small README/tooltip update for users
