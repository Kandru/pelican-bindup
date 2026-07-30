# pelican-steam-updater — agent notes

Linux CLI (root on Wings): one **main** install; **children** get bind mounts. Cron ticks resume via sidecar state.

Profiles without `steam_app_id` skip Steam polling and the update FSM; idle ticks only run maintenance (empty restarts). Child mounts for those groups are applied via manual `sync`.

## FSM (per Steam-enabled group)

`idle` → (defer) → `await_main_empty` → restart main → `await_children` → idle

- Steam poll gated by `update_check_interval_hours`; defer by `defer_update_minutes`.
- Query fail-open (A2S / BFBC2 / quake3): unreachable = empty (safe to restart/sync). Protocol from `profile.query_protocol`.
- Prod only mutates (`Logger.IsMutating`); dry-run/check-only do not.
- `run` takes flock on `<config>.lock`.

## Update order (Steam profiles only)

1. Steam remote vs **main** volume buildid only (children share mounts — disk buildid is useless).
2. Defer → if main behind target: wait empty → restart main (SteamCMD); if main already on target (e.g. maintenance): Discord update notice then children.
3. `await_children`: wait until main local ≥ target, then each child via state `child_synced[uuid]`.
4. Per child: already marked synced → skip; else empty → stop → `mount.Sync` → start → mark synced → Discord.

Idle “kids behind” = any child whose `child_synced` ≠ main local (same defer). New children after first bootstrap stay unsynced until that path runs.
Manual `sync` force-applies mounts (no state skip). Maintenance: no Discord.

## Packages

| Path | Role |
|------|------|
| `cmd/pelican-steam-updater` | CLI entry |
| `internal/update` | FSM + commands (`idle`,`children`,`maintenance`,`query`,`commands`,`lock`) |
| `internal/mount` | bind mount sync + prune |
| `internal/steam` | remote/local buildid; `Less` |
| `internal/a2s` | UDP A2S_INFO |
| `internal/bfbc2` | TCP RCON `serverInfo` player counts |
| `internal/quake3` | UDP Quake3 `getstatus` player counts |
| `internal/pelican` | Client API power/resources |
| `internal/state` | YAML phase persistence |
| `internal/config` | YAML load/validate + sidecars |
| `internal/profiles` | embedded game sync rules + query protocol |
| `internal/ui` | logger |
| `internal/discord` | webhook |
| `internal/selfupdate` | GH release binary replace |
| `internal/util` | ShortUUID, Truncate |

## Invariants

- Bind mounts need root; volumes under `paths.volumes`.
- Children must not run their own SteamCMD for the shared tree.
- Keep `internal/update` as small same-package files; don’t merge into one god file.
- Don’t change query fail-open (unreachable = empty) or state-based per-child sync without an explicit ask.
- Never use child volume LocalBuildID to decide sync (bind mounts mirror main).

## Edit map

- Phase logic → `internal/update/{idle,children}.go`
- Mount rules → `internal/profiles/*.yaml` + `mount/sync.go`
- Config schema → `config.yaml.example` + `internal/config`
