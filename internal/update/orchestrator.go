// Package update orchestrates Steam update phases and child mount sync.
package update

import (
	"fmt"

	"github.com/derkalle4/pelican-docker-mount-updater/internal/config"
	"github.com/derkalle4/pelican-docker-mount-updater/internal/discord"
	"github.com/derkalle4/pelican-docker-mount-updater/internal/pelican"
	"github.com/derkalle4/pelican-docker-mount-updater/internal/profiles"
	"github.com/derkalle4/pelican-docker-mount-updater/internal/state"
	"github.com/derkalle4/pelican-docker-mount-updater/internal/steam"
	"github.com/derkalle4/pelican-docker-mount-updater/internal/ui"
)

// Orchestrator runs per-group Steam update + child mount sync ticks.
type Orchestrator struct {
	cfg     *config.Config
	log     *ui.Logger
	client  *pelican.Client
	steam   *steam.Checker
	store   *state.Store
	discord *discord.Client
}

func New(cfg *config.Config, log *ui.Logger) *Orchestrator {
	return &Orchestrator{
		cfg:     cfg,
		log:     log,
		client:  pelican.NewClient(cfg.Pelican.PanelURL, cfg.Pelican.APIKey),
		steam:   steam.NewChecker(cfg.Steam.InfoAPI),
		store:   state.NewStore(cfg.StateFile()),
		discord: discord.New(cfg.Discord.WebhookURL),
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

func (o *Orchestrator) Run() error {
	for _, group := range o.cfg.Groups {
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
		return o.tickAwaitMainEmpty(group, gs)
	case state.PhaseAwaitChildren:
		return o.tickAwaitChildren(group, profile, gs)
	default:
		o.log.Step(ui.StatusWarn, "unknown phase %q — resetting to idle", gs.Phase)
		gs.Phase = state.PhaseIdle
		return o.store.Save(group.Name, gs)
	}
}
