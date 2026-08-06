// Package profiles loads embedded per-game sync exclusion rules.
package profiles

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kandru/pelican-docker-mount-updater/internal/a2s"
	"github.com/kandru/pelican-docker-mount-updater/internal/bfbc2"
	"github.com/kandru/pelican-docker-mount-updater/internal/quake3"
	"gopkg.in/yaml.v3"
)

//go:embed *.yaml
var fs embed.FS

const (
	QueryA2S    = "a2s"
	QueryBFBC2  = "bfbc2"
	QueryQuake3 = "quake3"
)

type Profile struct {
	SteamAppID       int                 `yaml:"steam_app_id"`
	ManifestRelative string              `yaml:"manifest_relative"`
	QueryProtocol    string              `yaml:"query_protocol"`
	ExcludeDirs      []string            `yaml:"exclude_dirs"`
	ExcludeFiles     []string            `yaml:"exclude_files"`
	ExcludePatterns  []string            `yaml:"exclude_patterns"`
	MountOnly        map[string][]string `yaml:"mount_only"`
}

var (
	loadMu sync.Mutex
	loaded = map[string]*Profile{}
)

func Load(name string) (*Profile, error) {
	loadMu.Lock()
	defer loadMu.Unlock()
	if p, ok := loaded[name]; ok {
		return p, nil
	}
	data, err := fs.ReadFile(name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("profile %q not found (available: %s)", name, strings.Join(Names(), ", "))
	}
	p, err := parseProfile(name, data)
	if err != nil {
		return nil, err
	}
	loaded[name] = p
	return p, nil
}

func parseProfile(name string, data []byte) (*Profile, error) {
	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile %q: %w", name, err)
	}
	if p.QueryProtocol == "" {
		p.QueryProtocol = QueryA2S
	}
	switch p.QueryProtocol {
	case QueryA2S, QueryBFBC2, QueryQuake3:
	default:
		return nil, fmt.Errorf("profile %q: unknown query_protocol %q (want %s, %s, or %s)",
			name, p.QueryProtocol, QueryA2S, QueryBFBC2, QueryQuake3)
	}
	if p.SteamAppID != 0 && p.ManifestRelative == "" {
		p.ManifestRelative = fmt.Sprintf("steamapps/appmanifest_%d.acf", p.SteamAppID)
	}
	return &p, nil
}

// SteamEnabled is true when this profile polls Steam for buildids.
func (p *Profile) SteamEnabled() bool {
	return p.SteamAppID != 0
}

// QueryPlayers returns current/max players using the profile's query protocol.
func (p *Profile) QueryPlayers(host string, port int) (players, maxPlayers int, err error) {
	switch p.QueryProtocol {
	case QueryBFBC2:
		return bfbc2.Query(host, port)
	case QueryQuake3:
		return quake3.Query(host, port)
	default:
		return a2s.Query(host, port)
	}
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
	if mountDir, child, ok := p.mountOnlyChild(rel); ok {
		if child == "" {
			return false
		}
		for _, pattern := range p.MountOnly[mountDir] {
			if matched, _ := filepath.Match(pattern, child); matched {
				return false
			}
		}
		return true
	}
	return false
}

func (p *Profile) DirContainsExclusions(rel string) bool {
	rel = filepath.ToSlash(rel)
	prefix := rel + "/"
	for _, excl := range p.ExcludeDirs {
		if strings.HasPrefix(excl, prefix) {
			return true
		}
	}
	for _, excl := range p.ExcludeFiles {
		if strings.HasPrefix(excl, prefix) {
			return true
		}
	}
	if _, ok := p.MountOnly[rel]; ok {
		return true
	}
	for mountDir := range p.MountOnly {
		if strings.HasPrefix(mountDir, prefix) {
			return true
		}
	}
	return false
}

// mountOnlyChild reports the mount_only directory and its immediate child name
// when rel is that directory or a path beneath it.
func (p *Profile) mountOnlyChild(rel string) (mountDir, child string, ok bool) {
	for mountDir, patterns := range p.MountOnly {
		_ = patterns
		if rel == mountDir {
			return mountDir, "", true
		}
		prefix := mountDir + "/"
		if strings.HasPrefix(rel, prefix) {
			rest := rel[len(prefix):]
			child = rest
			if idx := strings.Index(rest, "/"); idx >= 0 {
				child = rest[:idx]
			}
			return mountDir, child, true
		}
	}
	return "", "", false
}
