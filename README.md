# gorgon-session

A local Project Gorgon companion app for tracking sessions, loot, traders, history, and quick analytics.

No external database, no cloud dependency — runs on your machine.

---

## What this app does

- Tracks active session data from game logs
- Shows loot decisions (favor / sell / keep)
- Tracks traders and reset timers
- Keeps session history and stats
- Includes item catalog, recipes, combat stats, and drop-rate views
- Serves a web UI at `http://127.0.0.1:7777`

---

## Requirements

- **Windows** (primary tested target)
- **Project Gorgon** installed
- Chat log enabled in-game
- (Optional, for building from source) **Go 1.22+**

---

## Quick install (recommended)

1. Download or copy `gorgon-session.exe`.
2. Put it in a folder where you want to keep the app.
3. Run `gorgon-session.exe`.
4. Browser opens automatically to:
   - `http://127.0.0.1:7777`

That’s it.

---

## First-time setup

## 1) Enable game logs
In Project Gorgon:

- Open chat settings
- Enable chat logging (`/log` command also works)

Default paths used by app:

- Chat logs folder:  
  `C:\Users\<you>\AppData\LocalLow\Elder Game\Project Gorgon\ChatLogs`
- Player log file:  
  `C:\Users\<you>\AppData\LocalLow\Elder Game\Project Gorgon\Player.log`

If your paths are different, set them in **Settings** tab.

## 2) Start a session

1. Open app UI
2. Enter dungeon/zone name
3. Click **start**
4. Play normally — data updates live
5. Click **stop** when done

Session reports are saved under your user profile:

- `%USERPROFILE%\.gorgon-session\reports\`

---

## Build from source (optional)

From project root:

```powershell
go build -o bin\gorgon-session.exe ./cmd/gorgon
```

This compiles to:

- `bin/gorgon-session.exe` (Windows)

Optional (if you have `make` installed):

```bash
make build
```

(`make build` also outputs into `bin/`.)

---

## Run options

```powershell
# default
.\gorgon-session.exe

# custom address
.\gorgon-session.exe -addr 127.0.0.1:7788

# custom chat log folder
.\gorgon-session.exe -chatlog "D:\PG\ChatLogs"

# custom loot regex
.\gorgon-session.exe -loot-regex "(?i)loot\\s*:?\\s*(.+)"
```

---

## Troubleshooting

## UI views are empty / requests pending

- Close all app tabs and overlay windows
- Close app process
- Reopen app and only one browser tab first
- Hard refresh browser (`Ctrl+F5`)

## Port already in use

Run with another port:

```powershell
.\gorgon-session.exe -addr 127.0.0.1:7788
```

## No loot appears

- Confirm in-game logging is enabled
- Confirm paths in **Settings** are correct
- Check that session state is **running**

---

## Data storage locations

App data folder:

- `%USERPROFILE%\.gorgon-session\`

Inside it:

- `config.json` (settings)
- `cache\` (CDN cached data)
- `reports\` (session report JSON files)
- `traders.json` (trader tracking)
- `backups\` (automatic backups if enabled)

---

## Notes

- This app reads logs and CDN data; it does not modify the game client.
- All processing happens locally on your machine.
