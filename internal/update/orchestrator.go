// Package update orchestrates Steam update phases and child mount sync.
package update

import (
	"fmt"

	"github.com/kandru/pelican-docker-mount-updater/internal/config"
	"github.com/kandru/pelican-docker-mount-updater/internal/discord"
	"github.com/kandru/pelican-docker-mount-updater/internal/pelican"
	"github.com/kandru/pelican-docker-mount-updater/internal/profiles"
	"github.com/kandru/pelican-docker-mount-updater/internal/state"
	"github.com/kandru/pelican-docker-mount-updater/internal/steam"
	"github.com/kandru/pelican-docker-mount-updater/internal/ui"
	"github.com/kandru/pelican-docker-mount-updater/internal/util"
)

// Orchestrator runs per-group Steam update + child mount sync ticks.
type Orchestrator struct {
	cfg       *config.Config
	log       *ui.Logger
	client    *pelican.Client
	steam     *steam.Checker
	store     *state.Store
	discord   *discord.Client
	nameCache map[string]string
}

func New(cfg *config.Config, log *ui.Logger) *Orchestrator {
	return &Orchestrator{
		cfg:       cfg,
		log:       log,
		client:    pelican.NewClient(cfg.Pelican.PanelURL, cfg.Pelican.APIKey),
		steam:     steam.NewChecker(cfg.Steam.InfoAPI),
		store:     state.NewStore(cfg.StateFile()),
		discord:   discord.New(cfg.Discord.WebhookURL),
		nameCache: map[string]string{},
	}
}

func (o *Orchestrator) notifyDiscord(msg string) {
	if !o.discord.Enabled() || !o.log.IsMutating() {
		return
	}
	if err := o.discord.Send(msg); err != nil {
		o.log.Step(ui.StatusWarn, "discord: %v", err)
	} else {
		o.log.Detail("discord notification sent")
	}
}

// notifyUpdated sends: "<group> - <hostname> has been updated from <old> to <new>"
func (o *Orchestrator) notifyUpdated(groupName, serverUUID, oldBuild, newBuild string) {
	host := o.serverName(serverUUID)
	if oldBuild == "" {
		oldBuild = "unknown"
	}
	if newBuild == "" {
		newBuild = "unknown"
	}
	o.notifyDiscord(fmt.Sprintf("%s - %s has been updated from %s to %s", groupName, host, oldBuild, newBuild))
}

func (o *Orchestrator) serverName(serverUUID string) string {
	if name, ok := o.nameCache[serverUUID]; ok {
		return name
	}
	host, err := o.client.ServerName(serverUUID)
	if err != nil || host == "" {
		host = util.ShortUUID(serverUUID)
	}
	o.nameCache[serverUUID] = host
	return host
}

func (o *Orchestrator) Run(groupName string) error {
	groups, err := o.resolveGroups(groupName)
	if err != nil {
		o.log.Step(ui.StatusError, "%v", err)
		o.log.Summary()
		return err
	}
	for _, group := range groups {
		if err := o.runGroup(group); err != nil {
			o.log.Step(ui.StatusError, "group %s: %v", group.Name, err)
		}
	}
	o.log.Summary()
	if o.log.Errors() > 0 {
		return fmt.Errorf("%d error(s) during run", o.log.Errors())
	}
	return nil
}

func (o *Orchestrator) runGroup(group config.GroupConfig) error {
	gs, err := o.store.Load(group.Name)
	if err != nil {
		return err
	}
	o.log.Section(fmt.Sprintf("Group %s  ·  phase=%s", group.Name, gs.Phase))

	profile, err := profiles.Load(group.Profile)
	if err != nil {
		return err
	}

	switch gs.Phase {
	case state.PhaseIdle:
		return o.tickIdle(group, profile, gs)
	case state.PhaseAwaitMainEmpty:
		return o.tickAwaitMainEmpty(group, profile, gs)
	case state.PhaseAwaitChildren:
		return o.tickAwaitChildren(group, profile, gs)
	default:
		o.log.Step(ui.StatusWarn, "unknown phase %q — resetting to idle", gs.Phase)
		gs.Phase = state.PhaseIdle
		return o.store.Save(group.Name, gs)
	}
}
