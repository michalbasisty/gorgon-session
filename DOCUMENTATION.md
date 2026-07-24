# Project Gorgon Session Tracker — Technical Documentation & Architecture Reference

Welcome to the technical documentation for **gorgon-session**, a high-performance, self-contained Go application designed to track, parse, and optimize loot routing for Project Gorgon dungeon sessions. 

This document provides a comprehensive reference for developers, operators, and contributors. It details the system architecture, component design, data flows, API endpoints, and extension pathways for Phase 2.

---

## 1. System Architecture Overview

`gorgon-session` is designed as a modular, event-driven pipeline. It runs entirely on the user's local machine, requiring no external database or heavy runtime dependencies. The application is compiled into a single, lightweight binary (~10 MB) with all frontend assets embedded directly into the executable.

### High-Level Component Diagram

```
+---------------------------------------------------------------------------------+
|                                  LOCAL MACHINE                                  |
|                                                                                 |
|  +-----------------------+                                                      |
|  | Project Gorgon Client |                                                      |
|  +-----------+-----------+                                                      |
|              | (Writes chat logs)                                               |
|              v                                                                  |
|  +-----------+-----------+      Lines      +--------------------+               |
|  |    internal/logtail   +---------------->|   internal/loot    |               |
|  |       (Tailer)        |                 |      (Parser)      |               |
|  +-----------------------+                 +---------+----------+               |
|                                                      |                  HTTP    |
|                                                      | Event            Get     |
|                                                      v                 +-----+  |
|  +-----------------------+                 +---------+----------+      | CDN |  |
|  |   internal/session    |  AddLoot        |     main.go        |<-----+     |  |
|  |   (Session Manager)   |<----------------+ (Pipeline Wiring)  |      +-----+  |
|  +-----------+-----------+                 +---------+----------+               |
|              |                                       |                          |
|              | Snapshot / SSE                        | Resolve                  |
|              v                                       v                          |
|  +-----------+-----------+                 +---------+----------+               |
|  |    internal/server    |                 |   internal/favor   |               |
|  |     (HTTP Server)     |                 |  (Favor Engine)    |               |
|  +-----------+-----------+                 +--------------------+               |
|              |                                                                  |
|              | HTTP / SSE (Port 7777)                                           |
|              v                                                                  |
|  +-----------+-----------+                                                      |
|  |     web/ (SPA)        |                                                      |
|  |  (Embedded Dashboard) |                                                      |
|  +-----------------------+                                                      |
+---------------------------------------------------------------------------------+
```

### Core Architectural Principles
1. **Zero-Installation & Self-Containment**: Frontend assets (HTML, JS, CSS) are embedded into the Go binary using `go:embed`. The application runs as a single process with a local HTTP server.
2. **Polling-Based File Tailing**: To ensure cross-platform compatibility and avoid filesystem-specific watcher bugs (especially on Windows network shares or virtualized environments), the log tailer uses a robust polling mechanism with a 500ms interval.
3. **Deterministic Routing**: Loot routing is computed deterministically by matching item keywords against NPC preferences and services fetched directly from the official Project Gorgon CDN.
4. **Local-First Storage**: User settings, disabled NPCs, crafting checklists, and favor progress are stored in the browser's `localStorage`. Session reports are saved locally as JSON files under `~/.gorgon-session/reports/`.

---

## 2. Component Deep Dive

The codebase is organized into clean, decoupled packages under `internal/` and a single entry point under `cmd/gorgon/`.

### 2.1. Entry Point (`cmd/gorgon/main.go`)
The `main` package is responsible for:
- **CLI Flag Parsing**: Parses command-line overrides for bind address, chat-log directory, loot regex, CDN version, and diagnostic modes.
- **Initialization**: Loads configuration, initializes the CDN client, detects the current game version, and loads/caches the required JSON files (`items.json` and `npcs.json`).
- **Pipeline Wiring**: Spawns the background goroutine that connects the `logtail.Tailer` to the `loot.Parser`, resolves item metadata and routing decisions, and feeds them into the `session.Manager`.
- **Graceful Shutdown**: Catches `SIGINT` and `SIGTERM` signals, shuts down the HTTP server with a 5-second timeout, stops the log tailer, and closes active sessions.

### 2.2. Configuration (`internal/config/config.go`)
Manages persistent settings stored in `~/.gorgon-session/config.json`.

- **Struct Definition**:
  ```go
  type Config struct {
      HTTPAddr           string  `json:"http_addr"`
      ChatLogDir         string  `json:"chat_log_dir"`
      LootRegex          string  `json:"loot_regex"`
      CDNRoot            string  `json:"cdn_root"`
      VersionFile        string  `json:"version_file"`
      FallbackVersion    string  `json:"fallback_version"`
      CacheDir           string  `json:"cache_dir"`
      ReportDir          string  `json:"report_dir"`
      SellValueThreshold float64 `json:"sell_value_threshold"`
  }
  ```
- **Platform-Specific Defaults**:
  - **Windows**: `%USERPROFILE%\AppData\LocalLow\Elder Game\Project Gorgon\ChatLogs`
  - **Unix/macOS/Linux**: `~/.local/share/Elder Game/Project Gorgon/ChatLogs`
- **Lifecycle**: If `config.json` is missing on startup, the application automatically writes a default configuration file to disk and creates the necessary cache and report directories.

### 2.3. CDN Client (`internal/cdn/client.go`)
Fetches, caches, and parses game data from `cdn.projectgorgon.com`.

- **Version Detection**: Queries `http://client.projectgorgon.com/fileversion.txt` to discover the latest game version (e.g., `v480`). If unreachable, it falls back to the configured `fallback_version`.
- **Caching Strategy**: Downloads `items.json` and `npcs.json` and saves them under `~/.gorgon-session/cache/<version>/`. Subsequent runs read directly from the local cache, ensuring offline capability and instant startup.
- **Data Models**:
  - `Item`: Represents item metadata (ID, name, description, value, keywords, icon).
  - `Npc`: Represents NPC metadata, including area location, gift preferences, and services.
  - `Preference`: Represents a single gift rule (keywords, desire, preference score).
  - `Service`: Represents services offered by the NPC (e.g., Store, Consignment, Storage).

### 2.4. Log Tailer (`internal/logtail/tailer.go`)
A non-blocking, polling-based directory watcher and file tailer.

- **File Discovery**: Every 500ms, the tailer scans the configured `ChatLogDir` for `*.log` files and identifies the newest file based on modification time (`mtime`).
- **File Rotation**: If a new log file becomes the newest (e.g., when a new day starts or the game client rolls the log), the tailer automatically switches to the new file and resets its byte offset.
- **Truncation Handling**: If the file size becomes smaller than the current offset (e.g., if the game client overwrites or truncates the log), the tailer resets its offset to the end of the file to prevent reading garbage data.
- **Line Splitting & Partial Reads**: Reads raw bytes from the last offset to the end of the file. It splits the bytes by `\n` (stripping trailing `\r`). If the read ends with a partial line (no trailing newline), the tailer rewinds its offset to the start of that partial line so it can be fully read during the next poll.

### 2.5. Loot Parser (`internal/loot/parser.go`)
Extracts structured loot events from raw chat-log lines.

- **Regex Engines**:
  - **Default Regex**: `\[Status\]\s+(.+?)\s+(?:x(\d+)\s+)?added to inventory\.`
    - Matches standard loot lines like `[Status] Empty Bottle x5 added to inventory.`
  - **Bonus Regex**: `Also found (.+?)(?:\s+x(\d+))?\s+\(speed bonus`
    - Matches speed bonus loot lines like `Also found Empty Bottle x2 (speed bonus...`
- **Event Generation**: If a line matches, it returns an `Event` struct containing the raw line, the trimmed item name, the count (defaults to 1), and a boolean flag indicating if it was a speed bonus.

### 2.6. Favor Engine (`internal/favor/engine.go`)
The core decision-making engine that determines where looted items should go.

- **Indexing**: On startup, the engine indexes NPCs and their preferences. It builds a map of `byKeyword` (`map[string][]int`) mapping item keywords to NPC row indexes for fast, O(1) lookups.
- **Composite Keywords**: Keywords containing `:` (e.g., `EquipmentSlot:Head`) are skipped in Phase 1 because matching them requires loading additional item-schema data.
- **Routing Algorithm**:
  1. **Keyword Matching**: For each keyword of the looted item, the engine looks up candidate NPCs.
  2. **Preference Evaluation**: For each candidate NPC, the engine checks if *all* keywords of a preference rule are present in the item's keywords.
  3. **Scoring**: It sums the preference scores (`Pref` values) for matched rules. Positive scores indicate "Love" (favor targets). Negative scores indicate "Hate" (ignored).
  4. **Verdict Resolution**:
     - **Favor**: If any NPC has a score > 0, the item is routed to **Favor**. Suggestions are ranked in descending order of score.
     - **Consignment**: If no NPC loves the item, the engine checks if any NPC offers `Consignment` services and accepts the item's keywords in their `ItemTypes`. If so, the item is routed to **Consignment** (higher expected return).
     - **Vendor**: If neither applies, the item is routed to **Vendor Sell**.

### 2.7. Session Manager (`internal/session/session.go`)
Manages the active dungeon session lifecycle and aggregates loot.

- **State Machine**:
  - `idle`: No active session.
  - `running`: Active session capturing loot.
  - `stopped`: Session finalized; report written.
- **Loot Aggregation**: When a loot event is added, the manager checks if the item has already been looted during this session. If so, it increments the count and updates the `LastSeen` timestamp. If not, it creates a new `LootEntry` with its resolved routing decision.
- **Event Broadcasting**: Publishes session events (`loot`, `session_start`, `session_stop`) to an internal channel (`events`) consumed by the HTTP Server's SSE handlers.
- **Report Writing**: When a session is stopped, it writes a complete, structured JSON snapshot of the session to `~/.gorgon-session/reports/session-YYYYMMDD-HHMMSS.json`.

### 2.8. HTTP Server (`internal/server/server.go`)
Exposes the REST API and streams live updates.

- **Static Asset Serving**: Serves the embedded frontend assets (`index.html`, `app.js`, `style.css`) from the embedded filesystem.
- **REST API**: Exposes endpoints to start/stop sessions, fetch current session snapshots, list past sessions, and retrieve detailed reports of past sessions.
- **Server-Sent Events (SSE)**: The `/api/feed` endpoint establishes a persistent, one-way connection to the browser. It streams live events as they happen and sends a `:ping` every 15 seconds to keep the connection alive and prevent timeouts.

### 2.9. Web Dashboard (`web/app.js`, `web/index.html`, `web/style.css`)
A modern, dark-themed Single Page Application (SPA) built with vanilla JavaScript and CSS.

- **State Management**: Maintains a local `state` object tracking the active session, current view, list of NPCs, disabled NPCs, crafting recipes, and favor progress.
- **Local Storage Integration**:
  - `disabledNPCs`: Set of NPCs the user has disabled (e.g., because they haven't unlocked them or don't want to gift them). Items routed to these NPCs will automatically fall back to the next best target or sell routing.
  - `craftingRecipes`: User-defined crafting checklists with materials and completion checkboxes.
  - `favorProgress`: Set of NPCs the user is actively tracking favor progress for.
- **Views**:
  - **Tracker**: Displays a live-updating table of looted items, counts, verdicts, and suggested routes.
  - **Summary**: Aggregates session loot. Can be toggled between **By NPC** (grouped by NPC with total favor calculations) and **By Map** (grouped by game area/zone).
  - **History**: Lists past sessions with duration, total items, unique items, and total gold value. Clicking a session opens a detailed breakdown.
  - **Crafting**: Allows users to add custom recipes and track material checklists.
  - **Favor**: Lists all NPCs with search and toggle switches to disable/enable them.
  - **Shop NPC**: Lists all shop NPCs and their locations.

---

## 3. Data Flow Trace

The following trace shows how a single loot event flows through the system:

```
[Game Client]
      |
      | 1. Writes line to ChatLogs/ChatLog.txt:
      |    "[Status] Iron Ore x3 added to inventory."
      v
[internal/logtail (Tailer)]
      |
      | 2. Polls directory, detects new bytes, reads line.
      | 3. Emits line on Lines channel.
      v
[main.go (Pipeline)]
      |
      | 4. Receives line from channel.
      v
[internal/loot (Parser)]
      |
      | 5. ParseLine("[Status] Iron Ore x3 added to inventory.")
      |    - Matches DefaultRegex.
      |    - Captures ItemName: "Iron Ore", Count: 3.
      |    - Returns Event{ItemName: "Iron Ore", Count: 3, Bonus: false}.
      v
[main.go (Pipeline)]
      |
      | 6. Looks up "Iron Ore" in itemIndex (case-insensitive).
      |    - Finds Item ID 1024, Value 15g, Keywords ["Ore", "Metal", "Iron"].
      v
[internal/favor (Engine)]
      |
      | 7. Resolve("Iron Ore", ["Ore", "Metal", "Iron"], 15.0)
      |    - Matches keywords against NPC preferences.
      |    - Finds NPC "Mari" loves "Ore" (+3.0 favor) and "Iron" (+5.0 favor).
      |    - Finds NPC "Tyler Green" loves "Metal" (+2.0 favor).
      |    - Computes scores: Mari = 8.0, Tyler Green = 2.0.
      |    - Returns Decision{Verdict: "favor", FavorTargets: [Mari (8.0), Tyler Green (2.0)]}.
      v
[internal/session (Session Manager)]
      |
      | 8. AddLoot(LootEntry{Name: "Iron Ore", Count: 3, Decision: ...})
      |    - Aggregates count into session state.
      |    - Publishes Event{Kind: "loot", Payload: LootEntry} to events channel.
      v
[internal/server (HTTP Server)]
      |
      | 9. SSE handler (/api/feed) receives event.
      | 10. Formats and flushes event as SSE data block to browser.
      v
[web/app.js (Web Dashboard)]
      |
      | 11. SSE EventSource receives "loot" event.
      | 12. Updates local state.
      | 13. Dynamically prepends a new row to the Tracker table.
      | 14. Plays sound / updates badges if configured.
```

---

## 4. API Reference

All API endpoints bind to `http://127.0.0.1:7777` by default.

### 4.1. REST Endpoints

| Method | Path | Description | Request Body | Response Schema |
| :--- | :--- | :--- | :--- | :--- |
| **GET** | `/` | Serves the embedded dashboard HTML. | — | `text/html` |
| **GET** | `/style.css` | Serves the dashboard stylesheet. | — | `text/css` |
| **GET** | `/app.js` | Serves the dashboard JavaScript. | — | `application/javascript` |
| **GET** | `/api/session` | Retrieves the current session snapshot. | — | `Snapshot` (see below) |
| **POST** | `/api/session/start` | Starts a new session. | `{"dungeon": "Serbule Crypt"}` | `Snapshot` |
| **POST** | `/api/session/stop` | Stops the active session and writes a report. | — | `Snapshot` |
| **GET** | `/api/loot` | Retrieves only the list of looted items. | — | `[]LootEntry` |
| **GET** | `/api/config` | Retrieves the current configuration (read-only). | — | `Config` |
| **GET** | `/api/npcs` | Retrieves a list of all indexed NPCs. | — | `[]NPCInfo` |
| **GET** | `/api/sessions` | Retrieves a summary list of all past sessions. | — | `[]SessionSummary` |
| **GET** | `/api/session/{id}` | Retrieves details of a specific past session. | — | `Snapshot` |

### 4.2. SSE Stream (`/api/feed`)
Establishes a persistent Server-Sent Events stream. Events are formatted as JSON payloads.

#### Event Kinds:
1. **`session_start`**: Emitted when a session is started.
   ```json
   {
     "kind": "session_start",
     "time": "2026-07-24T08:15:00Z",
     "payload": { "dungeon": "Serbule Crypt" }
   }
   ```
2. **`session_stop`**: Emitted when a session is stopped.
   ```json
   {
     "kind": "session_stop",
     "time": "2026-07-24T08:45:00Z",
     "payload": { "items": 42 }
   }
   ```
3. **`loot`**: Emitted when an item is looted or its count is incremented.
   ```json
   {
     "kind": "loot",
     "time": "2026-07-24T08:16:30Z",
     "payload": {
       "name": "Iron Ore",
       "item_id": 1024,
       "value": 15.0,
       "count": 3,
       "bonus": false,
       "first_seen": "2026-07-24T08:16:30Z",
       "last_seen": "2026-07-24T08:16:30Z",
       "decision": {
         "item": "Iron Ore",
         "verdict": "favor",
         "favor_targets": [
           { "npc": "Mari", "area": "Serbule", "score": 8.0, "matches": ["Ore", "Iron"] },
           { "npc": "Tyler Green", "area": "Serbule", "score": 2.0, "matches": ["Metal"] }
         ]
       }
     }
   }
   ```

---

## 5. Configuration & Regex Tuning

### 5.1. Tuning the Loot Regex
The default regex matches standard English client lines. However, if you play in a different locale, use custom chat tabs, or have client options that alter the chat log format, you can override the regex.

To test a custom regex without restarting the server, use the `-test-loot` flag:
```powershell
.\bin\gorgon.exe -test-loot "[14:23:45] You loot Empty Bottle from Serbule."
```

If the default regex fails, edit `~/.gorgon-session/config.json` and set `loot_regex` to a pattern where **capture group 1 is the item name** and **capture group 2 (optional) is the count**.

Example for custom chat log formats:
```json
"loot_regex": "(?i)You loot \\\"?([A-Z][A-Za-z' ]+?)\\\"?(?:[.,]|\\s+from\\s|$)"
```

### 5.2. Configuration File Schema (`config.json`)
```json
{
  "http_addr": "127.0.0.1:7777",
  "chat_log_dir": "C:\\Users\\Michał\\AppData\\LocalLow\\Elder Game\\Project Gorgon\\ChatLogs",
  "loot_regex": "",
  "cdn_root": "http://cdn.projectgorgon.com",
  "version_file": "http://client.projectgorgon.com/fileversion.txt",
  "fallback_version": "v480",
  "cache_dir": "C:\\Users\\Michał\\.gorgon-session\\cache",
  "report_dir": "C:\\Users\\Michał\\.gorgon-session\\reports",
  "sell_value_threshold": 500.0
}
```

---

## 6. Development & Extension

### 6.1. Building from Source
To compile the application locally, ensure you have Go 1.21+ installed, then run:
```powershell
# Build the executable
go build -o bin/gorgon.exe ./cmd/gorgon
```

### 6.2. Testing & Diagnostics
The application includes several diagnostic flags to verify the integrity of the favor engine and CDN data:

```powershell
# Dump all NPCs with gift preferences and services as JSON
.\bin\gorgon.exe -dump-npcs > npcs-with-favor.json

# Force a specific CDN version (useful for testing updates or historical data)
.\bin\gorgon.exe -version v480

# Bind to a different port
.\bin\gorgon.exe -addr :8080
```

### 6.3. Phase 2 Roadmap
The architecture is designed to be easily extensible. Here is how to implement the planned Phase 2 features:

1. **Crafting Planner Integration**:
   - **Data Ingestion**: Extend `internal/cdn/client.go` to fetch and cache `recipes.json` and `xptables.json` from the CDN.
   - **Backend Models**: Parse recipes into structured Go models mapping recipes to required ingredients and skill levels.
   - **Frontend UI**: Connect the existing `Crafting` view in `web/app.js` to a new `/api/recipes` endpoint, allowing users to search recipes, view material checklists, and track completion.

2. **Combat Logger Expansion**:
   - **Grammar Extension**: Extend `internal/loot/parser.go` to parse combat events (e.g., damage dealt, damage taken, heals, critical hits, and experience gains).
   - **Session Aggregation**: Update `internal/session/session.go` to track combat metrics (DPS, HPS, total damage, and XP earned) alongside loot entries.
   - **Real-Time Feed**: Stream combat events over the `/api/feed` SSE stream and render live charts/graphs on the dashboard.

3. **Inventory & Storage Management**:
   - **Character Export Ingestion**: Implement a parser for the in-game character-export JSON (which contains current inventory, vault storage, and pocket dimensions).
   - **Storage Mapping**: Map item locations across all storage vaults using the schema documented in the extraction maps.
   - **Dashboard Search**: Add a global search bar to the dashboard to instantly locate items across all character vaults and storage chests.

4. **Live Configuration Editing**:
   - **REST Endpoint**: Add a `POST /api/config` endpoint to `internal/server/server.go` that accepts a JSON payload of configuration overrides.
   - **On-Disk Persistence**: Implement a thread-safe save mechanism in `internal/config/config.go` to write overrides back to `config.json` and hot-reload the log tailer or loot parser.
