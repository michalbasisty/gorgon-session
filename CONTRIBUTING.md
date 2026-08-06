# Contributing to gorgon-session

Thanks for helping out. This guide covers the practical stuff: building, testing, how the code is organized, and how to make a change without breaking things.

## Project at a glance

A local Windows companion app for **Project Gorgon**. A single Go binary tails the game's chat logs and `Player.log`, parses loot/XP/kill events, computes favor/sell routing from CDN game data, and serves an embedded web dashboard plus an optional native always-on-top overlay window.

- **Backend:** Go (stdlib only, one external dep: `go-webview2` for the Windows overlay)
- **Frontend:** vanilla JS + HTML + CSS, embedded into the binary via `go:embed`
- **Storage:** no database. Config in `config.json`, session reports as JSON files, trader/price state as JSON files, UI state in `localStorage`.
- **Requires:** Go 1.22+ (Windows recommended; the overlay is Windows-only, everything else is cross-platform)

## Build & run

```powershell
# Build
go build -o bin\gorgon-session.exe ./cmd/gorgon   # or: make build

# Run (opens the dashboard at http://127.0.0.1:7777)
go run ./cmd/gorgon

# Run the native overlay window directly (needs the server running too)
go run ./cmd/gorgon -overlay
```

Useful flags (see `cmd/gorgon/main.go`):

| Flag | Purpose |
| --- | --- |
| `-addr :8080` | override bind address |
| `-chatlog <dir>` | override the ChatLogs folder |
| `-loot-regex <re>` | override the loot-line regex |
| `-version v481` | force a CDN version instead of auto-detect |
| `-test-loot "<line>"` | test the loot regex against one pasted line, then exit |
| `-overlay` | run the native overlay window and exit |

## Testing

```powershell
go test ./...          # unit tests (all packages)
go vet ./...           # static checks
```

Coverage: `go test -cover ./...`. The overlay package is mostly Windows-only (`//go:build windows`); its testable logic is covered on Windows only. The race detector (`go test -race`) needs a C toolchain (`gcc`) — on a machine without one, review concurrency-sensitive code manually (see "Shared state" below).

Frontend tests live in `test/` and run under **Node.js**, not `go test`:

```powershell
node test/routeplan.test.js          # unit tests for the route-planner renderer (VM sandbox, no server needed)
node test/routeplan-live.test.js     # end-to-end, requires a running server on :7777
node test/overlay-check.js           # CDP-driven headless-Edge check of the overlay page (manual)
```

## Code layout

| Path | What lives there |
| --- | --- |
| `cmd/gorgon/main.go` | Everything wires together here: flags, config, CDN load, tailers, pipelines, trader auto-populate, backup, HTTP server, graceful shutdown. Start here to understand the app. |
| `internal/config` | `config.json` load/save, platform-aware defaults, portable mode, overlay settings. |
| `internal/cdn` | Fetches + caches game data (`items.json`, `npcs.json`, `areas.json`, `skills.json`, `recipes.json`) from the Project Gorgon CDN to `~/.gorgon-session/cache/<version>/`. |
| `internal/logtail` | Polling file watchers (500ms): `Tailer` follows the newest `*.log` in a folder; `FileTailer` follows one file from a byte offset. Handles rotation, truncation, partial lines. |
| `internal/loot` | Regex parser for chat-log lines: loot, bonus loot, XP, gold, deaths, kills, gathering, level-ups. |
| `internal/playerlog` | Regex parser for `Player.log` lines: zones, login, skill ticks, corpse searches. |
| `internal/favor` | Keyword-based routing engine. Decides favor / sell-consignment / sell-vendor / keep per item, ranked NPC targets, honors player-price overrides. |
| `internal/session` | The one active session: aggregation, snapshots, SSE event pub/sub, report JSON on stop. |
| `internal/prices` | In-memory price history persisted to `price-history.json`. |
| `internal/trader` | Per-NPC weekly sell limits, refresh schedules, history; auto-refresh and offline catch-up. |
| `internal/overlay` | Windows-only WebView2 overlay window (always-on-top, borderless, click-through, live opacity). Non-Windows stub in `overlay_other.go`. |
| `internal/server` | HTTP API (~30 routes), SSE feed, static file serving, export/import. `server_routes.go` lists every route. |
| `web/` | The SPA. `index.html` + `shared.js` (main app: views, state, overlay glue), `summary.js`, `history.js`, `favor-traders.js`, `settings-warcache.js`, `init.js`, plus the standalone `overlay.html`/`overlay.js` HUD page. |
| `web/embed.go` | The `//go:embed` directive — **any new file in `web/` must be listed here or it won't ship in the binary.** |
| `test/` | JS tests + helpers (see Testing). |

## How data flows

```
Project Gorgon
  ├─ ChatLogs\Chat-<date>.log  ──> logtail.Tailer ──> loot.Parser ──┐
  └─ Player.log                ──> logtail.FileTailer ─> playerlog.Parser ─┤
                                                                         v
                                                     main.go pipelines → session.Manager
                                                                         │ snapshot + SSE
                                                                         v
                                              internal/server (HTTP :7777) ←→ web/ SPA
```

A loot line like `[Status] Iron Ore x3 added to inventory.` flows: tailer picks up the appended bytes → `loot.Parser` extracts item+count → `main.go` looks the item up by name in the CDN index → `favor.Engine` computes a routing decision → `session.Manager.AddLoot` aggregates it → `/api/feed` streams an SSE event → `shared.js` updates the Tracker table.

## Making a change end-to-end (typical flow)

1. **Parser event** (e.g. a new chat-log line type): add a regex + event kind in `internal/loot` or `internal/playerlog`, with table-driven tests.
2. **Pipeline**: route the new event in `cmd/gorgon/main.go` (`pipeline` / `playerPipeline`).
3. **State**: add aggregation to `internal/session` (respect `m.mu` — see below) and expose it in `Snapshot`.
4. **API**: add/extend a handler in `internal/server` and register the route in `server_routes.go`.
5. **UI**: render it in `web/` (add the file to `web/embed.go`).
6. **Verify**: `go test ./...`, `go vet ./...`, run the app, and (if UI) `node test/routeplan.test.js` style test or a manual smoke.

## Code conventions & gotchas

- **Stdlib first.** No new dependencies without a reason; the only non-stdlib import is `go-webview2`.
- **Shared state:** `session.Manager`, `trader`, `favor` and both tailers are touched from multiple goroutines. Guard with `sync.Mutex`/`RWMutex`. Known past bug class: reading a field *after* unlocking — always snapshot under the lock, publish outside it.
- **Windows-only code:** use `//go:build windows` (e.g. `internal/overlay/overlay_windows.go`, `internal/server/spawn_windows.go`). Provide a stub with the inverse tag so cross-platform builds stay green.
- **Tailers:** polling-based by design (cross-platform, no fsnotify). The `*.log` extension filter is intentional — the game writes `Chat-<date>.log` files, not `.txt`.
- **Embed:** new frontend files MUST be added to `web/embed.go`'s `//go:embed` line.
- **Tests:** table-driven, stdlib `testing` only. `playerlog` is at 100% coverage — keep it that way. Prefer `t.TempDir()` for files.
- **Logging:** keep startup/spam logs sane — the tailer deliberately rate-limits its "no .log files found" warning (every 30s) because it polls twice a second.

## Known limitations (don't be surprised)

- The **active session lives in memory only** and is written to a report JSON on stop — a crash loses the current session.
- **CDN cache never expires** per version; if CDN data is corrected, delete `~/.gorgon-session/cache/<version>/` to force a refetch.
- The overlay is **Windows-only** (WebView2).
- Frontend JS has **thin test coverage**; `shared.js` is a large single file with all views in the global scope. Refactor with care.

## Pull request checklist

- [ ] `go vet ./...` clean, `go test ./...` green (new behavior has tests)
- [ ] New `web/` files added to `web/embed.go`
- [ ] Windows-only additions have a non-Windows stub (or build tag)
- [ ] API changes documented in `DOCUMENTATION.md` §4 (API reference)
- [ ] Concurrency-sensitive changes audited for the "read-after-unlock" bug class
