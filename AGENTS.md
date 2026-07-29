# pelican-steam-updater — agent notes

Linux CLI (root on Wings): one Steam **main** install; **children** get bind mounts. Cron ticks resume via sidecar state.

## FSM (per group)

`idle` → (defer) → `await_main_empty` → restart main → `await_children` → idle

- Steam poll gated by `update_check_interval_hours`; defer by `defer_update_minutes`.
- A2S fail-open: unreachable = empty (safe to restart/sync).
- Prod only mutates (`Logger.IsMutating`); dry-run/check-only do not.
- `run` takes flock on `<config>.lock`.

## Per-child sync

Each pending child: read volume `LocalBuildID` vs target/main.
- already on target → skip (no Discord)
- behind + empty → stop → `mount.Sync` → start → Discord (if webhook set)
- behind + players → stay pending

Idle “kids behind” scans each child buildid (not only group `synced_buildid`).
Manual `sync` force-applies mounts (no buildid skip). Maintenance: no Discord.

## Packages

| Path | Role |
|------|------|
| `cmd/pelican-steam-updater` | CLI entry |
| `internal/update` | FSM + commands (`idle`,`children`,`maintenance`,`query`,`commands`,`lock`) |
| `internal/mount` | bind mount sync + prune |
| `internal/steam` | remote/local buildid; `Less` |
| `internal/a2s` | UDP A2S_INFO |
| `internal/pelican` | Client API power/resources |
| `internal/state` | YAML phase persistence |
| `internal/config` | YAML load/validate + sidecars |
| `internal/profiles` | embedded game sync rules |
| `internal/ui` | logger |
| `internal/discord` | webhook |
| `internal/selfupdate` | GH release binary replace |
| `internal/util` | ShortUUID, Truncate |

## Invariants

- Bind mounts need root; volumes under `paths.volumes`.
- Children must not run their own SteamCMD for the shared tree.
- Keep `internal/update` as small same-package files; don’t merge into one god file.
- Don’t change A2S fail-open or per-child buildid gate without an explicit ask.

## Edit map

- Phase logic → `internal/update/{idle,children}.go`
- Mount rules → `internal/profiles/*.yaml` + `mount/sync.go`
- Config schema → `config.yaml.example` + `internal/config`
