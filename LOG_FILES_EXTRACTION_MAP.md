# Log Files Extraction Map (Project Gorgon)

This file maps **what the app currently extracts from game `.log` files**, and what we can expand next without VIP-only features.

## 1) Log sources used by the app

### A) Chat logs folder
- Default path (Windows):
  - `C:\Users\<you>\AppData\LocalLow\Elder Game\Project Gorgon\ChatLogs`
- Tail mode:
  - Polls every 500ms
  - Picks newest `*.log` file
  - Reads appended lines only

### B) Player log file
- Default path (Windows):
  - `C:\Users\<you>\AppData\LocalLow\Elder Game\Project Gorgon\Player.log`
- Tail mode:
  - Polls every 500ms
  - Reads appended lines only

---

## 2) Currently parsed from ChatLogs (`internal/loot/parser.go`)

| Event kind | Example line pattern | Extracted fields | Used in app |
|---|---|---|---|
| `loot` | `[Status] <item> x<qty>? added to inventory.` | `item_name`, `count` (default `1`), `bonus=false` | Tracker/session loot, value/favor routing, history |
| `loot` (speed bonus) | `Also found <item> x<qty>? (speed bonus...` | `item_name`, `count`, `bonus=true` | Same as loot, flagged as bonus |
| `xp` | `[Status] You earned <N> XP in <Skill>.` | `amount`, `skill` | XP/session stats |
| `gold` | `[Status] You found <N> councils?.` | `amount` | Session total gold |
| `death` | `[Status] You have died.` | (event only) | Death counter/timeline |
| `kill` | `[Status] You killed <mob>!` | `killer` (mob name) | Kill counter/timeline |
| `gather` | `[Status] You collected <item> x<qty>?` or `<item> collected!` | `item_name`, `count` | Gathering stats |
| `level` | `[Status] You are now level <N> in <Skill>!` | `level`, `skill` | Level-up timeline |

---

## 3) Currently parsed from Player.log (`internal/playerlog/parser.go`)

| Event kind | Example line pattern | Extracted fields | Used in app |
|---|---|---|---|
| `login` | `Welcome to Project Gorgon!` | (event only) | Hook for future auto-session behavior |
| `zone` | `You have entered <zone>.` | `zone` | Current zone + zone history |
| `skill` | `[Status] [WW] Skill '<name>' gained <value>` | `skill`, `value` (int-cast) | Reserved for granular skill tracking |
| `use_ability` | `UseAbility(Ability(<name>,<id>))` | `ability_name`, `ability_id` | Combat ability uses |
| `on_attack_hit_me` | `entity_<id>: OnAttackHitMe(Ability(<name>,<id>))` | `ability_name`, `ability_id` | Combat outgoing hit count |

---

## 4) Derived combat data (from logs + CDN metadata)

From log events, the app builds:
- `uses` per ability
- `hits` per ability
- `ability_id`

Then enriches with CDN ability data:
- `skill`
- `damage_type`
- `base_damage` (from nested `PvE.Damage`)

And computes:
- `est_dps = (base_damage * events) / session_duration_seconds`
- where `events = hits` when hit lines exist, otherwise fallback `events = uses`

So combat values are **estimated** from logged usage/hits + static ability metadata, not direct floating combat text totals.

---

## 5) Data we can likely expand from logs (no VIP)

These are feasible with local logs only; no VIP dependency.

## Already captured in code, but not fully surfaced in UI
1. **Ability event timeline timestamps**
   - We already store per-event timestamps for uses and hits.
   - Can power DPS-over-time and burst windows.
2. **Zone history timeline**
   - Every zone transition is timestamped.
   - Can power per-zone splits for loot/XP/combat.
3. **Player.log skill tick values (`[WW] Skill ... gained`)**
   - Parsed but not yet shown in dedicated UI analytics.
4. **Login event**
   - Parsed; can trigger optional auto-start session prompt.

## High-confidence next additions
1. **Combat timeline stream**
   - Store ability events with timestamps as a per-fight timeline.
2. **Per-zone loot and XP split**
   - Combine zone transitions with loot/XP timestamps.
3. **Session phase segmentation**
   - Split one run into segments (travel/combat/loot bursts) by event density.
4. **Top mob kill stats by zone**
   - Use `kill` events + current zone.
5. **Gathering heatmap by zone**
   - Use `gather` + zone history.

## Needs sample-line capture first (format verification)
1. **Direct damage numbers** (incoming/outgoing real totals)
2. **Crit/glance/resist/miss outcomes**
3. **Buff/debuff apply/fade tracking**
4. **Healing/shield events**
5. **Party combat attribution**

For these, we need raw line examples from your actual `Player.log`/chat logs and then add regexes safely.

---

## 6) Where to extend parser logic

- ChatLogs parser: `internal/loot/parser.go`
- Player.log parser: `internal/playerlog/parser.go`
- Session storage model: `internal/session/session.go`
- Pipelines wiring: `cmd/gorgon/main.go`
- API shaping for UI: `internal/server/server_data.go`
- Combat UI rendering: `web/shared.js`

---

## 7) Notes / constraints

- Both tailers start at file end on first run, so historical lines are not backfilled automatically.
- Parsing is regex-driven and intentionally strict to avoid bad matches.
- Any new parser should be added with line samples to avoid false positives.