package update

import (
	"fmt"
	"path/filepath"

	"github.com/kandru/pelican-docker-mount-updater/internal/config"
	"github.com/kandru/pelican-docker-mount-updater/internal/profiles"
	"github.com/kandru/pelican-docker-mount-updater/internal/ui"
)

func (o *Orchestrator) mainVolume(group config.GroupConfig) string {
	return filepath.Join(o.cfg.Paths.Volumes, group.Main.UUID)
}

func (o *Orchestrator) mainLocalBuildID(group config.GroupConfig, profile *profiles.Profile) (string, error) {
	local, err := o.steam.LocalBuildID(o.mainVolume(group), profile.ManifestRelative)
	if err != nil {
		o.log.Step(ui.StatusError, "read main buildid: %v", err)
		return "", err
	}
	return local, nil
}

// eachGroup loads each group's profile and calls fn. Profile load errors are logged and skipped.
func (o *Orchestrator) eachGroup(groups []config.GroupConfig, fn func(config.GroupConfig, *profiles.Profile) error) {
	for _, group := range groups {
		profile, err := profiles.Load(group.Profile)
		if err != nil {
			o.log.Step(ui.StatusError, "group %s profile %s: %v", group.Name, group.Profile, err)
			continue
		}
		if err := fn(group, profile); err != nil {
			o.log.Step(ui.StatusError, "group %s: %v", group.Name, err)
		}
	}
}

func (o *Orchestrator) finish(what string) error {
	o.log.Summary()
	if o.log.Errors() > 0 {
		return fmt.Errorf("%s completed with %d error(s)", what, o.log.Errors())
	}
	return nil
}

func syncedLabel(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
