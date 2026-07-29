// Package profiles loads embedded per-game sync exclusion rules.
package profiles

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed *.yaml
var fs embed.FS

type Profile struct {
	SteamAppID       int      `yaml:"steam_app_id"`
	ManifestRelative string   `yaml:"manifest_relative"`
	ExcludeDirs      []string `yaml:"exclude_dirs"`
	ExcludeFiles     []string `yaml:"exclude_files"`
	ExcludePatterns  []string `yaml:"exclude_patterns"`
}

func Load(name string) (*Profile, error) {
	data, err := fs.ReadFile(name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("profile %q not found (available: %s)", name, strings.Join(Names(), ", "))
	}
	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile %q: %w", name, err)
	}
	if p.SteamAppID == 0 {
		return nil, fmt.Errorf("profile %q: steam_app_id is required", name)
	}
	if p.ManifestRelative == "" {
		p.ManifestRelative = fmt.Sprintf("steamapps/appmanifest_%d.acf", p.SteamAppID)
	}
	return &p, nil
}

func Names() []string {
	entries, err := fs.ReadDir(".")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	return names
}

func (p *Profile) IsExcluded(rel string) bool {
	rel = filepath.ToSlash(rel)
	base := filepath.Base(rel)
	for _, excl := range p.ExcludeFiles {
		if rel == excl {
			return true
		}
	}
	for _, excl := range p.ExcludeDirs {
		if rel == excl || strings.HasPrefix(rel, excl+"/") {
			return true
		}
	}
	for _, pattern := range p.ExcludePatterns {
		if ok, _ := filepath.Match(pattern, base); ok {
			return true
		}
	}
	return false
}

func (p *Profile) DirContainsExclusions(rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, excl := range append(p.ExcludeDirs, p.ExcludeFiles...) {
		if strings.HasPrefix(excl, rel+"/") {
			return true
		}
	}
	return false
}
