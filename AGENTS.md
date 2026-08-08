# pelican-steam-updater — AI notes

Linux/root Wings CLI: one **main** install; **children** bind-mount it. Cron resumes via sidecar state.

No `steam_app_id` → skip Steam poll/FSM; idle = maintenance only; mounts via manual `sync`.

## FSM (Steam groups)

`idle` → defer → `await_main_empty` → restart main → `await_children` → idle

- Poll: `update_check_interval_hours`; defer: `defer_update_minutes`
- Query fail-open (a2s/bfbc2/quake3): unreachable = empty. Protocol: `profile.query_protocol`
- Mutate only in `prod` (`Logger.IsMutating`); `run` flocks `<config>.lock`

## Update order (Steam)

1. Remote vs **main** volume buildid only — never child LocalBuildID (mounts mirror main)
2. Defer → main behind? wait empty → restart (SteamCMD); else Discord then children
3. `await_children`: main local ≥ target; per child via `child_synced[uuid]`
4. Synced → skip; else empty → stop → `mount.Sync` → start → mark synced → Discord

Kids behind = `child_synced` ≠ main local (same defer). New children stay unsynced until that path. Manual `sync` force-applies (no state skip). Maintenance: no Discord.

## Packages

- `cmd/pelican-steam-updater` — CLI
- `internal/update` — FSM + commands (`idle`,`children`,`maintenance`,`query`,`commands`,`group`,`lock`)
- `internal/mount` — bind sync + prune
- `internal/steam` — remote/local buildid; `Less`
- `internal/a2s` | `bfbc2` | `quake3` — player queries
- `internal/query` — protocol registry
- `internal/pelican` — panel power/resources
- `internal/state` — YAML phase persistence
- `internal/config` — load/validate + sidecars
- `internal/profiles` — embedded sync rules + query protocol
- `internal/ui` | `discord` | `selfupdate` | `util`

## Invariants

- Root + volumes under `paths.volumes`; children must not SteamCMD shared tree
- Small same-package files in `internal/update` (no god file)
- Don’t change fail-open or state-based child sync without explicit ask
- Never use child LocalBuildID to decide sync

## Edit map

- Phases → `internal/update/{idle,children}.go`
- Mounts → `internal/profiles/*.yaml` + `mount/sync.go`
- Config → `config.yaml.example` + `internal/config`
- New query protocol → `internal/<pkg>` + register in `internal/query`
