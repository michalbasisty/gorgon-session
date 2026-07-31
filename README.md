# gorgon-session

A local Windows companion app for **Project Gorgon**: it tails your chat logs and `Player.log`, tracks sessions (loot, kills, XP, gold, zones), aggregates drop rates, computes favor/sell routing from CDN data, and serves an embedded web dashboard at `http://127.0.0.1:7777`.

No external database, no cloud dependency — everything runs on your machine.

---

## Features

- **Session tracking** — live loot, kills, XP, gold, and zone history; session reports saved as JSON.
- **Favor routing** — item verdicts (favor / sell / keep) computed from NPC gift preferences, with suggested targets.
- **Trader management** — weekly sell capacity per trader, refresh schedule, and history (CSV export/import).
- **Crafting** — recipe search plus a profit calculator.
- **Price trends** — historical price view for items.
- **Drop rates** — per-enemy and per-zone breakdowns, with a zone filter.
- **Session compare** — side-by-side session comparison.
- **Notes, backup, settings** — session notes, automatic report backups, live-editable settings.
- **Native overlay** — always-on-top WebView2 window (no browser needed) with split menus, opacity/theme/accent/position config, click-through toggle (`Ctrl+F9` / `o`), drag handle, `Esc` to close.

---

## Quick start

1. Download a release binary, or build from source (Go 1.22+):

   ```powershell
   go build -o bin\gorgon-session.exe ./cmd/gorgon
   ```

2. Run `gorgon-session.exe` (or `go run ./cmd/gorgon`).
3. In **Settings**, point the app at your game logs — both are auto-detected:
   - **Chat logs folder**: `C:\Users\<you>\AppData\LocalLow\Elder Game\Project Gorgon\ChatLogs`
   - **Player.log**: `C:\Users\<you>\AppData\LocalLow\Elder Game\Project Gorgon\Player.log`
     (zone detection reads `Downloading Map ... for area <zone> runtime key` lines)
4. Open `http://127.0.0.1:7777` — the dashboard.
5. Start a session, play, stop the session when done.

The overlay can be opened from the dashboard (Settings → Open Overlay) or directly with `go run ./cmd/gorgon -overlay`.

---

## Honest caveats

- Session data lives in memory and is only written to report JSON when a session is **stopped** — a crash loses the current session.
- The overlay is **Windows-only** (WebView2). On other platforms the app still runs and serves the dashboard; see `internal/overlay/overlay_other.go`.

---

## Data storage

- `%USERPROFILE%\.gorgon-session\` — app data folder
  - `config.json` (settings)
  - `cache\` (CDN data)
  - `reports\` (session report JSON files)
  - `backups\` (automatic backups)

---

## Troubleshooting

- **Port already in use**: run with `.\gorgon-session.exe -addr 127.0.0.1:7788`.
- **No loot appears**: confirm chat logging is enabled in-game and the paths in **Settings** are correct, and that a session is running.
- **Empty views / pending requests**: close extra dashboard tabs, restart the app, and hard-refresh the browser (`Ctrl+F5`).

---

## Notes

- The app only reads logs and CDN data; it never modifies the game client.
- All processing happens locally on your machine.
