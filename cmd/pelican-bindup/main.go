//go:build linux

// Command pelican-bindup: cron CLI for Pelican Wings main/child bind-mount sync and updates.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kandru/pelican-bindup/internal/config"
	"github.com/kandru/pelican-bindup/internal/selfupdate"
	"github.com/kandru/pelican-bindup/internal/ui"
	"github.com/kandru/pelican-bindup/internal/update"
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
		switch cfg.Mode {
		case config.ModeProd, config.ModeDryRun, config.ModeCheckOnly:
		default:
			fmt.Fprintf(os.Stderr, "config error: mode must be %q, %q, or %q (got %q)\n",
				config.ModeProd, config.ModeDryRun, config.ModeCheckOnly, cfg.Mode)
			return 1
		}
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
		if err := up.Run(); err != nil {
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
	fmt.Fprintf(os.Stderr, `pelican-bindup — One install, many servers — bound together, kept up to date.

Usage:
  pelican-bindup <command> [flags]

Commands:
  run            cron tick (update orchestration + reboot; -group to limit)
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
