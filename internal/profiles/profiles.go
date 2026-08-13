// Package profiles loads embedded per-game sync exclusion rules.
package profiles

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kandru/pelican-bindup/internal/query"
	"gopkg.in/yaml.v3"
)

//go:embed *.yaml
var fs embed.FS

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
		p.QueryProtocol = query.Default
	}
	if !query.Known(p.QueryProtocol) {
		return nil, fmt.Errorf("profile %q: unknown query_protocol %q (want %s)",
			name, p.QueryProtocol, strings.Join(query.Names(), ", "))
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
	return query.Players(p.QueryProtocol, host, port)
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
		if matchExcludePattern(pattern, rel, base) {
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
	for _, pattern := range p.ExcludePatterns {
		pattern = normalizeExcludePattern(pattern)
		if !strings.Contains(pattern, "/") {
			continue
		}
		patternDir := pattern[:strings.LastIndex(pattern, "/")]
		if patternDir == rel || strings.HasPrefix(patternDir, prefix) || strings.HasPrefix(rel, patternDir+"/") {
			return true
		}
	}
	return false
}

// normalizeExcludePattern strips a leading "/" used to anchor a pattern to the volume root.
func normalizeExcludePattern(pattern string) string {
	pattern = filepath.ToSlash(pattern)
	if strings.HasPrefix(pattern, "/") {
		return pattern[1:]
	}
	return pattern
}

// matchExcludePattern matches exclude_patterns entries:
//   - "/*.ini" — root-level files only
//   - "instance/*.ini" — path glob under that directory
//   - "*.dem" — basename glob at any depth
func matchExcludePattern(pattern, rel, base string) bool {
	rootAnchored := strings.HasPrefix(pattern, "/")
	pattern = normalizeExcludePattern(pattern)
	if rootAnchored {
		if strings.Contains(pattern, "/") {
			ok, _ := filepath.Match(pattern, rel)
			return ok
		}
		return !strings.Contains(rel, "/") && matchGlob(pattern, base)
	}
	if strings.Contains(pattern, "/") {
		ok, _ := filepath.Match(pattern, rel)
		return ok
	}
	return matchGlob(pattern, base)
}

func matchGlob(pattern, name string) bool {
	ok, _ := filepath.Match(pattern, name)
	return ok
}

// mountOnlyChild reports the mount_only directory and its immediate child name
// when rel is that directory or a path beneath it.
func (p *Profile) mountOnlyChild(rel string) (mountDir, child string, ok bool) {
	for mountDir := range p.MountOnly {
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
