//go:build linux

// Command pelican-steam-updater: cron CLI for Pelican Steam main/child mount sync.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kandru/pelican-docker-mount-updater/internal/config"
	"github.com/kandru/pelican-docker-mount-updater/internal/selfupdate"
	"github.com/kandru/pelican-docker-mount-updater/internal/ui"
	"github.com/kandru/pelican-docker-mount-updater/internal/update"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		usage()
		return 2
	}
	command := strings.ToLower(os.Args[1])
	if command == "version" {
		fmt.Println(version)
		return 0
	}
	if command == "-h" || command == "--help" || command == "help" {
		usage()
		return 0
	}

	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	defaultConfig := config.DefaultConfigPath()
	configPath := fs.String("config", defaultConfig, "path to config file")
	modeFlag := fs.String("mode", "", "override mode: prod, dry-run, check-only")
	groupFlag := fs.String("group", "", "limit to a single group (run, sync, test)")
	noColor := fs.Bool("no-color", false, "disable ANSI colors")
	if err := fs.Parse(normalizeFlagNames(os.Args[2:])); err != nil {
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}
	if *modeFlag != "" {
		cfg.Mode = config.Mode(strings.ToLower(*modeFlag))
	}

	log := ui.New(cfg.Mode, *noColor)
	if cfg.Logging.Enabled {
		if err := log.OpenFile(cfg.LogFile(), cfg.Logging.RetainHours); err != nil {
			fmt.Fprintf(os.Stderr, "log file error: %v\n", err)
			return 1
		}
		defer log.Close()
	}
	log.Banner(version, command)
	if cfg.Logging.Enabled {
		retain := "last run only"
		if cfg.Logging.RetainHours > 0 {
			retain = fmt.Sprintf("last %dh", cfg.Logging.RetainHours)
		}
		log.Detail("log_file=%s  (%s)", cfg.LogFile(), retain)
	}

	orch := update.New(cfg, log)

	switch command {
	case "run":
		if cfg.Mode == config.ModeCheckOnly {
			return exitErr(orch.CheckUpdate())
		}
		unlock, err := update.AcquireLock(cfg.LockFile())
		if err != nil {
			log.Step(ui.StatusError, "%v", err)
			log.Summary()
			return 1
		}
		defer unlock()
		return exitErr(orch.Run(*groupFlag))

	case "status":
		return exitErr(orch.Status())

	case "check-update":
		return exitErr(orch.CheckUpdate())

	case "test":
		return exitErr(orch.Test(*groupFlag))

	case "sync":
		return exitErr(orch.Sync(*groupFlag))

	case "self-update":
		if !cfg.SelfUpdate.Enabled {
			log.Step(ui.StatusWarn, "self_update.enabled is false in config")
			log.Summary()
			return 1
		}
		up := selfupdate.New(cfg.SelfUpdate.GitHubRepo, version, log)
		if err := up.Run(""); err != nil {
			log.Step(ui.StatusError, "%v", err)
			log.Summary()
			return 1
		}
		log.Summary()
		return 0

	default:
		usage()
		return 2
	}
}

func exitErr(err error) int {
	if err != nil {
		return 1
	}
	return 0
}

// normalizeFlagNames lowercases flag names only (-Group → -group), leaving values intact.
func normalizeFlagNames(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if !strings.HasPrefix(a, "-") || a == "-" || a == "--" {
			out[i] = a
			continue
		}
		name, val, hasVal := strings.Cut(a, "=")
		out[i] = strings.ToLower(name)
		if hasVal {
			out[i] += "=" + val
		}
	}
	return out
}

func usage() {
	defaultConfig := config.DefaultConfigPath()
	fmt.Fprintf(os.Stderr, `pelican-steam-updater — Pelican Steam main/child updater

Usage:
  pelican-steam-updater <command> [flags]

Commands:
  run            cron tick (update orchestration + maintenance; -group to limit)
  status         show group phases and server states
  check-update   check Steam buildids only
  test           verify Steam API, Pelican panel, and A2S (-group to limit)
  sync           apply bind mounts (all groups in parallel, or -group for one)
  self-update    update binary from latest GitHub release
  version        print version

Flags:
  -config string   config file (default: %s, next to the binary)
  -mode string     override mode: prod, dry-run, check-only
  -group string    limit to one group (run, sync, test)
  -no-color        disable ANSI colors
`, defaultConfig)
}
