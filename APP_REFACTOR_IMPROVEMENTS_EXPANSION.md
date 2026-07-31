# Gorgon App: Refactor, Improve, Expand — Status

_Last updated: 2026-07-31_

Originally an audit list; most high-priority items are now done. `go test ./...` passes.

---

## ✅ Done

**Refactor**
- **Split `internal/server/server.go` into focused files** — now `server.go`, `server_http.go`, `server_sessions.go`, `server_admin.go`, `server_data.go`, `server_routes.go` (+ platform `spawn_*.go`).
- **Modularized frontend state by view** — `web/shared.js`, `summary.js`, `history.js`, `favor-traders.js`, `settings-warcache.js`, `init.js` (was one global `state` sprawl).
- **Typed DTOs where it mattered** — `AreaTraders.NPCs` is now `[]TraderInfo` (was `interface{}`); `configPatch` typed for `/api/config`.

**Reliability / bugs**
- **`/api/config` partial update** — POST/PUT now merges only provided fields (`applyConfigPatch`, `configPatch`) instead of overwriting with zero values; invalid `loot_regex` is rejected with `400` and the old parser state is kept. Covered by `TestApplyConfigPatch_MergesNotReplaces`.
- **SSE fan-out** — `handleFeed` now subscribes per client (`Sess.Subscribe()` / `unsubscribe()`) instead of a shared channel, so clients no longer steal each other's events.
- **SSE ping goroutine leak** — ping is written in the same select loop (15s ticker + `r.Context().Done()`); no separate goroutine to leak.
- **Session ID path validation** — `/api/session/{id}` validates `^session-\d{8}-\d{6}$` before touching disk (`sessionIDPattern`; `TestHandleSessionByID_InvalidID`).

**Expansion features**
- Sell route optimizer (`/api/route-planner`, route plan panel in Tracker).
- Session comparison mode (`/api/sessions/compare`).
- Combat efficiency panel (`/api/combat`, `/api/combat/breakdown`).
- Feature-scope export/import (settings `/api/export` + `/api/import`, trader history CSV, notes export, sessions bulk export).
- Cache status UI — the Warcache view shows CDN cache version age with refresh controls.

**Tests**
- `internal/config/config_test.go` added (overlay defaults after empty load).
- Handler tests for config patch semantics and session ID validation added (`internal/server/server_test.go`).

---

## ⏳ Still open

- Service layer between HTTP handlers and core packages.
- Normalize `Value` vs `Valor` naming across backend/frontend.
- Reduce race risk in tailers (`Tailer`, `FileTailer`) — consistent locking around shared offsets.
- Improve history performance for large report folders (index/cache metadata instead of reading all JSON).
- Document/enforce API schema versions for import/export.
- Graceful backup retention policy controls (prune count is hardcoded at 48).
- Sorting consistency in API responses (items list from map iteration is unsorted).
- Tests for `internal/logtail`, `internal/prices`, `internal/cdn`, `cmd/gorgon` (still untested).
- Race-check CI (`go test -race ./...`) and frontend smoke tests.

---

## Suggested order for the open items

1. Reduce tailer race risk + add race-check CI.
2. Add tests for `logtail`/`prices`/`cdn`.
3. Service layer + naming normalization.
4. History performance index; schema-versioned import/export.
