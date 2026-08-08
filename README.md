# pelican-steam-updater

Cron-driven updater for [Pelican](https://pelican.dev) Wings hosts that share one Steam game install across multiple servers via bind mounts.

One **main** server downloads Steam updates. **Child** servers receive synced files. The tool polls Steam for new builds, restarts servers only when they are empty (A2S), and keeps sibling installs in sync — without every server running its own SteamCMD update.

**Platform:** Linux only (runs on the Wings host as root).

## Table of contents

- [Features](#features)
- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Commands](#commands)
- [Cron](#cron)
- [Logging](#logging)
- [Discord notifications](#discord-notifications)
- [Game profiles](#game-profiles)
- [Building from source](#building-from-source)
- [Releasing](#releasing)

## Features

- **Shared Steam installs** — one main volume, children get bind-mounted files with per-game exclusion rules
- **Steam update orchestration** — detect remote buildid, defer for phased rollouts, restart main when empty, sync children
- **Empty-server safety** — A2S player queries before any restart or sync
- **Maintenance reboots** — optional periodic restarts for long uptimes (skipped during updates)
- **Connectivity test** — verify Steam API, Pelican panel, and A2S for every server in one command
- **Structured logging** — timestamped stdout; optional sidecar log file with retention
- **Discord webhooks** — notify on update-related restarts (not maintenance)
- **Dry-run mode** — see what would happen without changing anything
- **Embedded profiles** — game sync rules ship inside the binary (no extra files on the host)

## How it works

Each **group** in the config is one main server plus its children.

```
idle
  └─ Steam check (every N hours)
       └─ update found (or children behind main) → defer (default 30 min)
            └─ main behind target?
                 ├─ yes → empty? → restart main (SteamCMD) → Discord
                 └─ no  → Discord (main already current) → children
                      └─ wait until main buildid is current
                           └─ for each child (state not yet synced, when empty): stop → sync mounts → start
                                └─ idle
```

Steam is checked on the **main** install only. Children are tracked per-server in state (`child_synced`); already-synced siblings are skipped. If main was updated outside the FSM (e.g. maintenance reboot), the same defer still runs, then children are synced. New children added to a group are synced via that path (not silently marked synced). While an update is in progress, maintenance reboots are skipped. Cron should run every few minutes (e.g. every 5); per-group settings control how often Steam is actually polled.

State is persisted in a sidecar file so each cron tick can resume where the last run left off.

## Requirements

- Linux Wings host with bind-mount support
- Root access (bind mounts and volume paths)
- Pelican Client API key (`ptlc_…`) with access to all configured servers
- Game query host/port per server (protocol comes from the profile: A2S UDP or BFBC2 TCP RCON)
- Network access to your Pelican panel, optionally `api.steamcmd.net` (Steam profiles), and Discord

For Steam-backed profiles, children must **not** run their own SteamCMD update for the shared game tree.

## Installation

1. Download the latest release binary for your architecture from [GitHub Releases](https://github.com/kandru/pelican-docker-mount-updater/releases), or [build locally](#building-from-source).
2. Install to a directory on the Wings host, e.g. `/opt/pelican-steam-updater/`:

```bash
install -m 755 pelican-steam-updater_linux_amd64 /opt/pelican-steam-updater/pelican-steam-updater
```

3. Copy the example config into the **same directory as the binary**:

```bash
cp config.yaml.example /opt/pelican-steam-updater/config.yaml
```

4. Edit `config.yaml` — panel URL, API key, server UUIDs, query addresses (A2S or RCON depending on profile).
5. Verify everything:

```bash
/opt/pelican-steam-updater/pelican-steam-updater test
```

6. Add [cron entries](#cron).

## Quick start

```bash
# connectivity check (Steam if configured, Pelican, game query, Discord test message if configured)
pelican-steam-updater test

# preview what a cron tick would do
pelican-steam-updater run -mode dry-run

# production cron tick
pelican-steam-updater run

# re-apply bind mounts manually (e.g. after host reboot); groups run in parallel
pelican-steam-updater sync

# limit test or sync to one config group
pelican-steam-updater test -group cs2-ballerbude
pelican-steam-updater sync -group cs2-ballerbude
```

Config and sidecar files default to the binary's directory. Override with `-config /path/to/config.yaml`.

## Configuration

See [`config.yaml.example`](config.yaml.example) for a fully documented reference.

### File layout

Place `config.yaml` next to the binary. Sidecar files are created automatically:

| File | Purpose |
|------|---------|
| `config.yaml` | Your configuration |
| `config.state.yaml` | Persisted update/sync state per group |
| `config.lock` | Prevents concurrent `run` invocations |
| `config.log` | Optional run log (when `logging.enabled`) |

### Groups

A group defines one main server and its children:

```yaml
groups:
  - name: cs2-ballerbude
    profile: cs2
    update_check_interval_hours: 1
    defer_update_minutes: 30
    maintenance:
      enabled: true
      reboot_interval_hours: 2
    main:
      uuid: ...
      query_host: server.example.com
      query_port: 27015
    children:
      - uuid: ...
        query_host: server.example.com
        query_port: 27016
```

| Setting | Default | Description |
|---------|---------|-------------|
| `update_check_interval_hours` | `1` | How often to poll Steam while idle |
| `defer_update_minutes` | `30` | Wait after update detected before restarting main (or syncing children if main is already current) |
| `maintenance.reboot_interval_hours` | `24` | Reboot empty servers after this uptime |

### Global options

| Section | Description |
|---------|-------------|
| `pelican` | Panel URL and Client API key |
| `paths.volumes` | Pelican volumes directory (default `/var/lib/pelican/volumes`) |
| `steam.info_api` | Steam buildid API (default `https://api.steamcmd.net/v1/info`) |
| `logging` | Optional sidecar log; `retain_hours: 0` = last run only |
| `discord` | Optional webhook URL for update notifications |
| `self_update` | Binary self-update from GitHub releases |

### Modes

Set in config or override on the CLI with `-mode`:

| Mode | Behavior |
|------|----------|
| `prod` | Perform API calls, mounts, and state changes (default) |
| `dry-run` | Log actions without mutating anything |
| `check-only` | `run` only checks Steam buildids |

## Commands

```
pelican-steam-updater <command> [flags]
```

| Command | Description |
|---------|-------------|
| `run` | Cron tick — update orchestration and maintenance |
| `test` | Verify Steam API, Pelican panel, A2S, and Discord webhook |
| `status` | Show group phases and server states |
| `check-update` | Check Steam buildids only |
| `sync` | Apply bind mounts (all groups in parallel; `-group` to limit) |
| `self-update` | Update binary from latest GitHub release |
| `version` | Print version |

### Flags

| Flag | Description |
|------|-------------|
| `-config` | Config file (default: `config.yaml` next to the binary) |
| `-mode` | Override mode: `prod`, `dry-run`, `check-only` |
| `-group` | Limit to one group (`sync`, `test`) |
| `-no-color` | Disable ANSI colors in terminal output |

## Cron

Run on the Wings host as root. Example from [`docs/crontab.example`](docs/crontab.example):

```cron
# Every 5 minutes — per-group update_check_interval_hours controls Steam polling
*/5 * * * * /opt/pelican-steam-updater/pelican-steam-updater run >>/var/log/pelican-steam-updater.log 2>&1

# Re-apply bind mounts after host reboot
@reboot /opt/pelican-steam-updater/pelican-steam-updater sync >>/var/log/pelican-steam-updater.log 2>&1
```

## Logging

Every step is logged to stdout with timestamps. When `logging.enabled` is true, the same output is also written to `<config-name>.log` next to the config file.

- `retain_hours: 0` — keep only the current run (file rewritten each invocation)
- `retain_hours: N` — keep the last N hours; older lines are dropped at the start of each run

## Discord notifications

Optional global webhook:

```yaml
discord:
  webhook_url: https://discord.com/api/webhooks/...
```

| Event | Notified |
|-------|----------|
| `test` command | Test message to verify the webhook |
| Main restarted for Steam update | Yes (includes buildid) |
| Child synced and restarted | Yes (per child) |
| Maintenance reboot | No |
| Update detected but not yet applied | No |

Notifications are only sent in `prod` mode.

## Game profiles

Sync profiles define which files are bind-mounted and which are left independent per server (configs, addons, etc.). Profiles are embedded in the binary — currently available:

| Profile | Game | Steam App ID | Query protocol | Notes |
|---------|------|--------------|----------------|-------|
| `cs2` | Counter-Strike 2 | 730 | `a2s` (UDP) | Full Steam update FSM + child sync |
| `bfbc2` | Battlefield: Bad Company 2 | — | `bfbc2` (TCP RCON) | No Steam poll; maintenance empty-restarts; child sync via `sync` |
| `warfork` | Warfork | 1136510 | `quake3` (UDP) | Full Steam update FSM; `basewf` shares only `data*.pk3` / `modules*.pk3` |

Profile YAML fields:

| Field | Required | Description |
|-------|----------|-------------|
| `steam_app_id` | no | When set, enables Steam buildid polling and the update FSM. Omit for non-Steam games. |
| `manifest_relative` | no | Path under the main volume to `appmanifest_*.acf` (defaults when `steam_app_id` is set). |
| `query_protocol` | no | `a2s` (default), `bfbc2`, or `quake3`. Selects how emptiness checks query player counts. |
| `exclude_dirs` / `exclude_files` / `exclude_patterns` | no | Paths left independent per child (not bind-mounted from main). |
| `mount_only` | no | Under listed dirs, only basename patterns are mounted; everything else in that dir stays per-child. |

For BFBC2, set `query_port` to each server's **RCON port** (TCP). `serverInfo` does not need an RCON password. For Warfork, set `query_port` to each server's **game UDP port** (default `44400`). After updating the main install manually, run `sync --group <name>` to re-apply mounts to children.

To add a profile, create `internal/profiles/<name>.yaml` and rebuild. See [`internal/profiles/cs2.yaml`](internal/profiles/cs2.yaml), [`internal/profiles/bfbc2.yaml`](internal/profiles/bfbc2.yaml), and [`internal/profiles/warfork.yaml`](internal/profiles/warfork.yaml) for examples. To add a new query protocol, implement `Query(host, port)` in a new package under `internal/` and register it in [`internal/query/query.go`](internal/query/query.go).

## Building from source

Requires Docker only (no Go on the host). Caches are stored in `.cache/` under your user.

```bash
make build        # linux/amd64 → dist/
make build-all    # linux amd64 + arm64
make release      # archives + checksums
make clean-cache  # remove .cache/
```

## Releasing

Bump [`VERSION`](VERSION), commit, and push to `main`. GitHub Actions tags `vX.Y.Z`, builds release binaries, publishes assets, and keeps the **5 most recent** releases.
