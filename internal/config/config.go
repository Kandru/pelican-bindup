// Package config loads and validates YAML config plus sidecar paths.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/derkalle4/pelican-docker-mount-updater/internal/profiles"
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

type MaintenanceConfig struct {
	Enabled             bool `yaml:"enabled"`
	RebootIntervalHours int  `yaml:"reboot_interval_hours"`
}

type GroupConfig struct {
	Name                     string            `yaml:"name"`
	Profile                  string            `yaml:"profile"`
	UpdateCheckIntervalHours float64           `yaml:"update_check_interval_hours"`
	DeferUpdateMinutes       int               `yaml:"defer_update_minutes"`
	Maintenance              MaintenanceConfig `yaml:"maintenance"`
	Main                     ServerEndpoint    `yaml:"main"`
	Children                 []ServerEndpoint  `yaml:"children"`
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

func (g *GroupConfig) MaintenanceInterval() time.Duration {
	if g.Maintenance.RebootIntervalHours <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(g.Maintenance.RebootIntervalHours) * time.Hour
}

type ServerEndpoint struct {
	UUID      string `yaml:"uuid"`
	QueryHost string `yaml:"query_host"`
	QueryPort int    `yaml:"query_port"`
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
	for i := range c.Groups {
		if c.Groups[i].UpdateCheckIntervalHours == 0 {
			c.Groups[i].UpdateCheckIntervalHours = 1
		}
		if c.Groups[i].DeferUpdateMinutes == 0 {
			c.Groups[i].DeferUpdateMinutes = 30
		}
		if c.Groups[i].Maintenance.RebootIntervalHours == 0 {
			c.Groups[i].Maintenance.RebootIntervalHours = 24
		}
	}
}

func (c *Config) validate() error {
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
		if _, err := profiles.Load(g.Profile); err != nil {
			return fmt.Errorf("group %q: %w", g.Name, err)
		}
	}
	return nil
}

func (c *Config) GroupByName(name string) (*GroupConfig, error) {
	for i := range c.Groups {
		if c.Groups[i].Name == name {
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
