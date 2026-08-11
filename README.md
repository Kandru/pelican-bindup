# pelican-steam-updater

Linux CLI for [Pelican](https://pelican.dev) Wings hosts. One **main** game server holds the real install; **child** servers share those files via bind mounts. A cron tick checks for Steam updates, restarts servers only when empty, and keeps children in sync — without every server running its own SteamCMD copy.

## Why this saves disk

Large games (e.g. CS2) can take tens of gigabytes per server. With N independent installs you pay that cost N times. This tool keeps **one** full install on the main volume and bind-mounts the shared tree onto each child. Per-server files (configs, addons, maps you want independent) stay excluded by the game profile — so you keep distinct servers without duplicating the bulk game data.

## Requirements

- Linux Wings host with bind-mount support (run as **root**)
- Pelican Client API key (`ptlc_…`) with access to all configured servers
- Query host/port per server (protocol depends on the game profile)
- For Steam profiles: children must **not** run their own SteamCMD update on the shared tree

## Installation

1. Download the latest release for your architecture from [GitHub Releases](https://github.com/kandru/pelican-docker-mount-updater/releases).
2. Install the binary and config into one directory (e.g. `/opt/pelican-steam-updater/`):

```bash
install -m 755 pelican-steam-updater_linux_amd64 /opt/pelican-steam-updater/pelican-steam-updater
cp config.yaml.example /opt/pelican-steam-updater/config.yaml
```

3. Edit `config.yaml` — panel URL, API key, server UUIDs, query addresses.
4. Verify connectivity:

```bash
/opt/pelican-steam-updater/pelican-steam-updater test
```

5. Add the [cron entries](#cron) below.

Config and sidecar files (`config.state.yaml`, `config.lock`, optional log) live next to the binary by default. Override with `-config /path/to/config.yaml`.

## Configuration

See [`config.yaml.example`](config.yaml.example) for the full reference. A group is one main plus its children:

```yaml
groups:
  - name: cs2-example
    profile: cs2
    main:
      uuid: ...
      query_host: server.example.com
      query_port: 27015
    children:
      - uuid: ...
        query_host: server.example.com
        query_port: 27016
      - uuid: ...
        query_host: server.example.com
        query_port: 27017
        # Optional: use another profile's sync exclusions / query_protocol.
        # Files still come from main; Steam/FSM still follow the group profile.
        # profile: warfork
```

Embedded profiles: `cs2`, `bfbc2`, `warfork` (sync rules ship inside the binary). Children may set `profile` to use a different profile's copy rules while still bind-mounting from main.

## Commands

| Command | Description |
|---------|-------------|
| `run` | Cron tick — update orchestration and maintenance |
| `sync` | Re-apply bind mounts (needed after reboot) |
| `test` | Verify panel, Steam (if used), query, Discord |
| `status` | Show group phases and server states |

Useful flags: `-config`, `-mode prod|dry-run|check-only`, `-group <name>`.

```bash
pelican-steam-updater run -mode dry-run   # preview a tick
pelican-steam-updater sync -group cs2-example
```

## Cron

Run as root on the Wings host. Config must sit next to the binary (or pass `-config`).

```cron
# Every 5 minutes — update orchestration / maintenance
*/5 * * * * /opt/pelican-steam-updater/pelican-steam-updater run > /dev/null 2>&1

# After reboot — re-apply bind mounts (mounts do not survive reboot)
@reboot /opt/pelican-steam-updater/pelican-steam-updater sync > /dev/null 2>&1
```

To keep a log file instead of discarding output, redirect to a path (e.g. `>>/var/log/pelican-steam-updater.log 2>&1`). A ready-to-edit copy is in [`docs/crontab.example`](docs/crontab.example).

## Building from source

Requires Docker (no Go on the host):

```bash
make build        # linux/amd64 → dist/
make build-all    # linux amd64 + arm64
```
