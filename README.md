# gorgon-session

A small Go app for Project Gorgon dungeon sessions. It tails the game's chat
log, parses loot events, looks up each item in the official Project Gorgon
CDN data, and suggests where each looted item should go — gifted to which NPC
for favor, or sold via vendor / consignment — based on the actual NPC gift
preferences shipped in `npcs.json`.

This is **phase 1**: the dungeon-session slice of a planned larger app. Later
phases (crafting planner, combat logger, inventory/storage management) will
reuse the same CDN cache, HTTP server, and session primitives.

## What it does today

- Tails the chat-log file the Project Gorgon client writes while chat
  logging is enabled in-game.
- Parses each appended line for loot events using a configurable regex.
- Looks the loot name up in `items.json` (cached from CDN), then routes the
  item using `npcs.json` `Preferences[]` (NPC likes/hates by item keyword)
  and `Services[]` (`Store` = vendor, `Consignment` = sell on commission):
  - If any NPC loves the item's keywords → **favor** (ranked suggestions).
  - Else if any consignment NPC accepts the item's keyword category →
    **consign**.
  - Else → **vendor sell**.
- Serves a local web dashboard on `http://127.0.0.1:7777` with live updates
  via Server-Sent Events. No build step for the UI — vanilla JS embedded
  into the binary.
- Writes a session report JSON (`~/.gorgon-session/reports/session-*.json`)
  when the session is stopped.

## What it does NOT do yet (phase 2+)

- Crafting planner (recipes.json + xptables.json are already cached in the
  CDN module, just not loaded yet).
- Combat logger (will need to extend the loot parser's chat-log grammar).
- Inventory & storage management (will need the in-game character-export
  JSON + the storage report schema already documented in
  `PROJECT_GORGON_EXTRACTION_MAP.md`).
- Live editing of config / regex from the dashboard (today: edit
  `~/.gorgon-session/config.json` and restart).

## Build

```powershell
cd G:\project gorgon projects\gorgon-session
go build -o bin\gorgon.exe .\cmd\gorgon
```

No external services required; binary is ~10 MB and self-contained. First run
fetches `items.json` (~7 MB) and `npcs.json` (~290 KB) and caches them under
`%USERPROFILE%\.gorgon-session\cache\v<ver>\`. Subsequent starts read from
cache.

## Run

```powershell
.\bin\gorgon.exe
```

Open `http://127.0.0.1:7777/` in your browser. Type a dungeon/zone name,
click **start**, then enable chat logging in the game (see below) and loot
away. The dashboard updates as items drop.

## First-time setup

1. **Enable the in-game chat log**: open Project Gorgon → Chat window → gear
   icon → enable **Log Chat** (or use the `/log` slash command). The client
   will write a chat-log text file under
   `%LOCALAPPDATA%\Low\Elder Game\Project Gorgon\ChatLog.txt` (this is the
   default the app already uses). If your install writes it elsewhere,
   update `chat_log_path` in `~/.gorgon-session/config.json`.

2. **Launch gorgon-session** *before* entering the dungeon and click
   **start** in the dashboard with a session name.

3. Loot items appear automatically as they're written to the chat log.

## Tuning the loot regex

The default regex matches common English loot lines but the Project Gorgon
chat-log format depends on locale and client options. If names show up
wrong or not at all, run the test helper against a pasted line from your
actual chat log:

```powershell
.\bin\gorgon.exe -test-loot "[14:23:45] You loot Empty Bottle from Serbule."
```

The command prints the captured name, the resolved item record, and the
favor/sell decision. If the capture is wrong (e.g. includes "from Serbule"),
you need a different regex. If the capture is correct but `item_<id>` is `0`,
the name isn't in `items.json` (often a typo or a monster-only name).

Edit `~/.gorgon-session/config.json` and set `loot_regex` to a regex with
**capture group 1 = item name**. Restart the app. Example for tighter
matching:

```json
"loot_regex": "(?i)You loot \"?([A-Z][A-Za-z' ]+?)\"?(?:[.,]|\\s+from\\s|$)"
```

A trimmer post-process step in the parser already strips trailing
prepositional phrases like "from Serbule" / "in Kur Mountains" / "in Eltibule Keep"
from the captured name before lookup.

## Useful diagnostics flags

```powershell
# Force a CDN version instead of auto-detecting:
.\bin\gorgon.exe -version v480

# Override bind address:
.\bin\gorgon.exe -addr :8080

# Override chat-log path one-off:
.\bin\gorgon.exe -chatlog "D:\\PG\\Chat.txt"

# Override loot regex one-off:
.\bin\gorgon.exe -loot-regex "(?i)loot\\s*:?\\s*(.+)"

# Dump every NPC with gift preferences as JSON (great for sanity-checking
# favor engine coverage and seeing which NPCs love which keywords):
.\bin\gorgon.exe -dump-npcs > npcs-with-favor.json
```

## HTTP API (all under `http://127.0.0.1:7777`)

| Method | Path                   | Body / Query                              | Returns                                                |
|--------|------------------------|-------------------------------------------|--------------------------------------------------------|
| GET    | `/`                    | —                                         | Embedded dashboard HTML                                |
| GET    | `/style.css`, `/app.js`| —                                         | Dashboard assets                                       |
| GET    | `/api/session`         | —                                         | Current session snapshot                               |
| POST   | `/api/session/start`   | `{"dungeon":"<name>"}`                    | Snapshot after starting                                |
| POST   | `/api/session/stop`    | —                                         | Snapshot after stopping; writes `reports/session-*.json` |
| GET    | `/api/loot`            | —                                         | `[]LootEntry` only                                     |
| GET    | `/api/feed`            | (SSE)                                     | Live `loot`/`session_start`/`session_stop` events      |
| GET    | `/api/config`          | —                                         | Current config (read-only; edit json on disk)         |

## Project layout

```
gorgon-session/
  go.mod
  cmd/gorgon/main.go         - binary: flags, wiring, chat-log pipeline
  internal/
    config/config.go         - persistent config in ~/.gorgon-session/config.json
    cdn/client.go            - fetch + cache JSON files from cdn.projectgorgon.com
    logtail/tailer.go        - non-blocking chat-log tailer (fsnotify + poll fallback)
    loot/parser.go           - chat-line -> item-name parser (configurable regex)
    favor/engine.go          - NPC Preferences + Services based routing
    session/session.go       - dungeon-session lifecycle + report writing
    session/errors.go        - sentinel errors
    server/server.go         - HTTP routes + embedded static handler + SSE
  web/                       - embedded dashboard
    embed.go                 - go:embed
    index.html               - dashboard
    app.js                   - vanilla JS, SSE client
    style.css                - dark UI
  bin/gorgon.exe             - build output (gitignored)
```

## Notes on the favor engine

The matcher uses two pieces of CDN ground truth from `npcs.json`:

- `Preferences[]` on 241 of 340 NPCs: each entry has `Keywords[]` (matched
  against the item's `Keywords[]`), a `Pref` float (positive = Love,
  negative = Hate), and `Desire` (`Love`/`Hate`). The summed score is the
  sum of `Pref` for each satisfied preference.
- `Services[]` on 297 of 340 NPCs: `Consignment` services carry an
  `ItemTypes[]` (item keywords) for sell routing. `Store` services are the
  vendor fallback.

Known MVP limitation: preferences that use composite keywords (containing
`:`, e.g. `EquipmentSlot:Head` or `SkillPrereq:Hammer`) are **skipped**
during matching because they need additional item-schema data not loaded
yet. The next phase (crafting/combat) will load those schemas and re-enable
those matches — see `PROJECT_GORGON_EXTRACTION_MAP.md` §3.5 for the
underlying `tsysclientinfo.json` structure already documented.

## Where to extend for phase 2

- **Crafting planner**: load `recipes.json` + `xptables.json` + the rest of
  the `skills.json` schema via `cdn.Client` (the package's generic `Fetch`
  already supports any source). Add an `internal/craft` package
  paralleling `internal/favor`. Add UI tab.
- **Combat logger**: extend `loot.Parser` to also recognize ability/damage
  lines in the chat log (same tailer, new sub-regex). Add an
  `internal/combat` package.
- **Inventory & storage**: import the `characterReport` + `storageReport`
  JSON dumps the game can emit (see `PROJECT_GORGON_EXTRACTION_MAP.md` §5).
  Add an `internal/character` package and UI tab.
- **Live config editing**: turn `/api/config` into a PUT that writes
  `config.json` and reloads the parser without restarting the app.

## License & attribution

Game data is fetched from the public Project Gorgon CDN
(`cdn.projectgorgon.com`) under its terms:

> Some portions copyright 2026 Elder Game, LLC.

This app does not alter the game client. It reads CDN data and a
user-produced chat-log file; both are explicitly permitted by the official
CDN data policy (see `PROJECT_GORGON_EXTRACTION_MAP.md` for the full
policy text).