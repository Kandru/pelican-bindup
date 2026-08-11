// Package config loads and validates YAML config plus sidecar paths.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kandru/pelican-bindup/internal/profiles"
	"github.com/kandru/pelican-bindup/internal/util"
	"gopkg.in/yaml.v3"
)

type Mode string

const (
	ModeProd      Mode = "prod"
	ModeDryRun    Mode = "dry-run"
	ModeCheckOnly Mode = "check-only"
)

type Config struct {
	Mode       Mode             `yaml:"mode"`
	Pelican    PelicanConfig    `yaml:"pelican"`
	Paths      PathsConfig      `yaml:"paths"`
	Steam      SteamConfig      `yaml:"steam"`
	SelfUpdate SelfUpdateConfig `yaml:"self_update"`
	Logging    LoggingConfig    `yaml:"logging"`
	Discord    DiscordConfig    `yaml:"discord"`
	Groups     []GroupConfig    `yaml:"groups"`

	configPath string
}

// LoggingConfig controls the optional sidecar log file (<config-name>.log).
// Disabled by default — stdout always gets every step. When enabled:
//   retain_hours: 0  → keep only the current run (file rewritten each invocation)
//   retain_hours: N  → keep the last N hours; older lines are dropped at start
type LoggingConfig struct {
	Enabled     bool `yaml:"enabled"`
	RetainHours int  `yaml:"retain_hours"`
}

type DiscordConfig struct {
	WebhookURL string `yaml:"webhook_url"`
}

type PelicanConfig struct {
	PanelURL string `yaml:"panel_url"`
	APIKey   string `yaml:"api_key"`
}

type PathsConfig struct {
	Volumes string `yaml:"volumes"`
}

type SteamConfig struct {
	InfoAPI string `yaml:"info_api"`
}

type SelfUpdateConfig struct {
	GitHubRepo string `yaml:"github_repo"`
	Enabled    bool   `yaml:"enabled"`
}

type RebootConfig struct {
	Enabled       bool `yaml:"enabled"`
	IntervalHours int  `yaml:"interval_hours"`
}

// RebootOverride optionally overrides group reboot settings on a child server.
type RebootOverride struct {
	Enabled       *bool `yaml:"enabled,omitempty"`
	IntervalHours *int  `yaml:"interval_hours,omitempty"`
}

type GroupConfig struct {
	Name                     string           `yaml:"name"`
	Profile                  string           `yaml:"profile"`
	UpdateCheckIntervalHours float64          `yaml:"update_check_interval_hours"`
	DeferUpdateMinutes       int              `yaml:"defer_update_minutes"`
	Reboot                   RebootConfig     `yaml:"reboot"`
	Main                     ServerEndpoint   `yaml:"main"`
	Children                 []ServerEndpoint `yaml:"children"`
}

func (g *GroupConfig) UpdateCheckInterval() time.Duration {
	if g.UpdateCheckIntervalHours <= 0 {
		return time.Hour
	}
	return time.Duration(g.UpdateCheckIntervalHours * float64(time.Hour))
}

func (g *GroupConfig) DeferUpdateInterval() time.Duration {
	if g.DeferUpdateMinutes <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(g.DeferUpdateMinutes) * time.Minute
}

func (c RebootConfig) Interval() time.Duration {
	if c.IntervalHours <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(c.IntervalHours) * time.Hour
}

// RebootFor returns reboot settings for a server, merging optional child overrides.
func (g *GroupConfig) RebootFor(srv ServerEndpoint) RebootConfig {
	cfg := g.Reboot
	if srv.Reboot != nil {
		if srv.Reboot.Enabled != nil {
			cfg.Enabled = *srv.Reboot.Enabled
		}
		if srv.Reboot.IntervalHours != nil && *srv.Reboot.IntervalHours > 0 {
			cfg.IntervalHours = *srv.Reboot.IntervalHours
		}
	}
	return cfg
}

type ServerEndpoint struct {
	UUID      string `yaml:"uuid"`
	QueryHost string `yaml:"query_host"`
	QueryPort int    `yaml:"query_port"`
	// Profile optionally overrides the group profile for this child only.
	// Sync still copies from main; exclusions / query_protocol follow this profile.
	// Ignored on main (group.profile always applies).
	Profile string `yaml:"profile,omitempty"`
	// Reboot optionally overrides group reboot settings for this child only.
	// Ignored on main (group.reboot always applies).
	Reboot *RebootOverride `yaml:"reboot,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	cfg.configPath = path
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	c.Mode = Mode(strings.ToLower(string(c.Mode)))
	if c.Mode == "" {
		c.Mode = ModeProd
	}
	if c.Paths.Volumes == "" {
		c.Paths.Volumes = "/var/lib/pelican/volumes"
	}
	if c.Steam.InfoAPI == "" {
		c.Steam.InfoAPI = "https://api.steamcmd.net/v1/info"
	}
	if c.Logging.RetainHours < 0 {
		c.Logging.RetainHours = 0
	}
}

func (c *Config) validate() error {
	switch c.Mode {
	case ModeProd, ModeDryRun, ModeCheckOnly:
	default:
		return fmt.Errorf("mode must be %q, %q, or %q (got %q)", ModeProd, ModeDryRun, ModeCheckOnly, c.Mode)
	}
	if c.Pelican.PanelURL == "" {
		return fmt.Errorf("pelican.panel_url is required")
	}
	if c.Pelican.APIKey == "" || c.Pelican.APIKey == "ptlc_REPLACE_ME" {
		return fmt.Errorf("pelican.api_key must be set")
	}
	if len(c.Groups) == 0 {
		return fmt.Errorf("at least one group is required")
	}
	for _, g := range c.Groups {
		if g.Name == "" {
			return fmt.Errorf("group name is required")
		}
		if g.Profile == "" {
			return fmt.Errorf("group %q: profile is required", g.Name)
		}
		if g.Main.UUID == "" {
			return fmt.Errorf("group %q: main uuid is required", g.Name)
		}
		if g.Main.Profile != "" {
			return fmt.Errorf("group %q: main must not set profile (use group profile)", g.Name)
		}
		if g.Main.Reboot != nil {
			return fmt.Errorf("group %q: main must not set reboot (use group reboot)", g.Name)
		}
		if _, err := profiles.Load(g.Profile); err != nil {
			return fmt.Errorf("group %q: %w", g.Name, err)
		}
		for i, child := range g.Children {
			if child.UUID == "" {
				return fmt.Errorf("group %q: children[%d]: uuid is required", g.Name, i)
			}
			if child.Profile == "" {
				continue
			}
			if _, err := profiles.Load(child.Profile); err != nil {
				return fmt.Errorf("group %q: children[%d] (%s): %w", g.Name, i, util.ShortUUID(child.UUID), err)
			}
		}
	}
	return nil
}

// ChildProfileName returns the profile used for a child's sync exclusions and query protocol.
// Empty child.Profile falls back to the group profile. Source files still come from main.
func (g *GroupConfig) ChildProfileName(child ServerEndpoint) string {
	if child.Profile != "" {
		return child.Profile
	}
	return g.Profile
}

func (c *Config) GroupByName(name string) (*GroupConfig, error) {
	for i := range c.Groups {
		if strings.EqualFold(c.Groups[i].Name, name) {
			return &c.Groups[i], nil
		}
	}
	return nil, fmt.Errorf("group %q not found", name)
}

func (g *GroupConfig) ChildByUUID(uuid string) *ServerEndpoint {
	for i := range g.Children {
		if g.Children[i].UUID == uuid {
			return &g.Children[i]
		}
	}
	return nil
}
