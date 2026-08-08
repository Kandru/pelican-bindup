// Package query dispatches player-count queries to registered game protocols.
package query

import (
	"fmt"
	"sort"

	"github.com/kandru/pelican-docker-mount-updater/internal/a2s"
	"github.com/kandru/pelican-docker-mount-updater/internal/bfbc2"
	"github.com/kandru/pelican-docker-mount-updater/internal/quake3"
)

// Func queries a game server for current and max player counts.
type Func func(host string, port int) (players, maxPlayers int, err error)

const (
	A2S     = "a2s"
	BFBC2   = "bfbc2"
	Quake3  = "quake3"
	Default = A2S
)

var registry = map[string]Func{
	A2S:    a2s.Query,
	BFBC2:  bfbc2.Query,
	Quake3: quake3.Query,
}

// Known reports whether proto is a registered query protocol.
func Known(proto string) bool {
	_, ok := registry[proto]
	return ok
}

// Names returns the registered protocol names, sorted.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Players queries using the named protocol.
func Players(proto, host string, port int) (players, maxPlayers int, err error) {
	fn, ok := registry[proto]
	if !ok {
		return 0, 0, fmt.Errorf("unknown query protocol %q", proto)
	}
	return fn(host, port)
}
