# Gorgon App Audit: Refactor, Improve, Expand (No VIP)

_Date: 2026-07-29_

This list is based on a codebase scan of backend (`cmd/`, `internal/`) and frontend (`web/`) plus test run results.

---

## ✅ Current health snapshot

- `go test ./...` passes.
- Core features are working and modularized by package.
- Biggest risk now is **technical debt in API/server and frontend state management**, not basic functionality.

---

## 1) Refactor list (structure/maintainability)

## P0 (do first)

- [ ] **Split `internal/server/server.go` into focused files by domain**
  - Why: one large file handles session, config, traders, exports, combat, drop rates, static serving.
  - Suggested split:
    - `server_session.go`
    - `server_config.go`
    - `server_history.go`
    - `server_traders.go`
    - `server_static.go`
    - `server_analytics.go`

- [ ] **Create shared request/response DTO structs for API handlers**
  - Why: many inline anonymous structs make validation inconsistent and harder to test.
  - Outcome: cleaner validation + easier OpenAPI/doc generation later.

- [ ] **Refactor frontend global state into per-view modules** (`web/shared.js`, `summary.js`, `history.js`, `favor-traders.js`, `settings-warcache.js`)
  - Why: many cross-file globals and inline handlers increase regressions.
  - Outcome: predictable state flow and simpler debugging.

## P1

- [ ] **Add service layer between HTTP handlers and core packages**
  - Why: handlers currently mix transport + business logic.
  - Outcome: easier unit tests and cleaner API changes.

- [ ] **Normalize naming (`Value` vs `Valor`) across backend and frontend**
  - Why: mixed naming increases mapping mistakes.
  - Outcome: clearer contracts and fewer accidental bugs.

- [ ] **Replace `interface{}` fields with concrete types**
  - Example: `AreaTraders.NPCs interface{}` should be a typed slice.

---

## 2) Improvement list (bugs, reliability, UX)

## P0 (high-impact)

- [ ] **Fix `/api/config` partial update behavior**
  - Current risk: missing fields can overwrite config with zero values.
  - Concrete issue: `pushPlayerPricesToServer()` posts partial payload but server assigns all fields directly.
  - Fix: patch semantics (only apply provided fields) + validation.

- [ ] **Fix SSE event fan-out model**
  - Current risk: multiple SSE clients consume from one channel and can miss events (work-stealing behavior).
  - Fix: subscription manager with per-client buffered channels.

- [ ] **Fix goroutine leak in SSE ping loop**
  - Current risk: ping goroutine can block on channel send after disconnect.
  - Fix: remove extra ping channel and write ping directly in same select loop.

- [ ] **Sanitize `/api/session/{id}` path input**
  - Current risk: path traversal attempts via crafted IDs.
  - Fix: strict ID regex allowlist (`^session-\d{8}-\d{6}$` or UUID format if changed later).

## P1

- [ ] **Handle parser regex errors on live config update**
  - Current behavior ignores parser set-regex error.
  - Fix: reject invalid regex with `400` and keep old parser state.

- [ ] **Reduce race risk in tailers (`Tailer`, `FileTailer`)**
  - Reason: mutable offsets/paths updated across goroutines.
  - Fix: consistent locking around shared state or single-owner goroutine messages.

- [ ] **Improve history performance for large report folders**
  - Current behavior reads many JSON files into memory.
  - Fix: index/cache metadata file + incremental updates.

- [ ] **Document and enforce API schema versions for import/export**
  - Prevent future breakage when config/session schema evolves.

## P2

- [ ] **Add graceful backup policy controls**
  - Keep count/retention period configurable instead of hardcoded values.

- [ ] **Improve sorting consistency in API responses**
  - Example: items list from map iteration is unsorted.

---

## 3) Test & quality improvements

- [ ] **Add tests for packages with no tests**
  - `internal/config`, `internal/logtail`, `internal/prices`, `internal/cdn`, `cmd/gorgon`.

- [ ] **Add concurrency/race checks in CI**
  - Run `go test -race ./...` on CI.

- [ ] **Add handler tests for config update semantics, session ID validation, SSE behavior**

- [ ] **Add frontend smoke tests**
  - At minimum: view switch, start/stop session flow, settings save/load, history export.

---

## 4) Expansion ideas WITHOUT VIP (all local/log/CDN-based)

These features can be built with current data sources (chat logs, Player.log, CDN data, local files) and do **not** require VIP-only gameplay features.

## High value

- [ ] **Sell route optimizer by current zone**
  - Combine session loot + `zone` + trader capacities to suggest nearest profitable sell path.

- [ ] **"What to keep" smart presets**
  - User rule sets by keyword/skill/crafting relevance (e.g., always keep alchemy mats).

- [ ] **Drop-rate confidence view**
  - Add confidence intervals and minimum-session warnings (not just averages).

- [ ] **Session comparison mode**
  - Compare two sessions/zones: value/hour, deaths, loot quality, favorite NPC gains.

- [ ] **Combat efficiency panel**
  - Ability usage + hit rate + estimated DPS trend over session duration.

## Medium value

- [ ] **Crafting ingredient demand from loot stream**
  - Flag looted items used in tracked recipes.

- [ ] **Auto-tag and note templates**
  - Auto-note items by verdict/category (e.g., "consign", "favor for <npc>").

- [ ] **Zone-specific NPC quick list in overlay**
  - Show nearby favor/sell NPC recommendations live.

- [ ] **Session goals/checklist**
  - User-defined targets: total gold, item count, specific drops.

## Nice-to-have

- [ ] **Import/export for only one feature scope**
  - Separate exports for prices, trader limits, favor-progress.

- [ ] **Offline mode indicator and cache status UI**
  - Show CDN cache version age and refresh controls.

---

## 5) Suggested execution order (practical)

1. Fix config patch semantics + validation.
2. Fix SSE fan-out + ping goroutine behavior.
3. Add session ID path validation.
4. Split `server.go` and add handler tests.
5. Modularize frontend state by view.
6. Ship first non-VIP expansion: **zone-aware sell route optimizer**.

---

## Validation run

- Command run: `go test ./...`
- Result: pass
