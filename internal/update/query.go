package update

import (
	"fmt"

	"github.com/kandru/pelican-bindup/internal/config"
	"github.com/kandru/pelican-bindup/internal/profiles"
	"github.com/kandru/pelican-bindup/internal/ui"
)

func playerLabel(players, maxPlayers int) string {
	if maxPlayers > 0 {
		return fmt.Sprintf("%d/%d players", players, maxPlayers)
	}
	return fmt.Sprintf("%d players", players)
}

// serverEmpty is true when the game query reports 0 players, or the query fails (fail-open).
func (o *Orchestrator) serverEmpty(srv config.ServerEndpoint, profile *profiles.Profile, label string) bool {
	proto := profile.QueryProtocol
	players, maxPlayers, qerr := profile.QueryPlayers(srv.QueryHost, srv.QueryPort)
	if qerr != nil {
		o.log.Step(ui.StatusStart, "Query %s  %s:%d", label, srv.QueryHost, srv.QueryPort)
		o.log.Detail("%s: %v (treating as empty)", proto, qerr)
		return true
	}
	if players == 0 {
		o.log.Step(ui.StatusOK, "%s  %s:%d  empty", label, srv.QueryHost, srv.QueryPort)
		return true
	}
	o.log.Step(ui.StatusWait, "%s  %s:%d  %s", label, srv.QueryHost, srv.QueryPort, playerLabel(players, maxPlayers))
	return false
}
