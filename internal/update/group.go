package update

import (
	"fmt"
	"path/filepath"

	"github.com/kandru/pelican-bindup/internal/config"
	"github.com/kandru/pelican-bindup/internal/profiles"
	"github.com/kandru/pelican-bindup/internal/ui"
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

// childProfile returns the profile for a child's sync exclusions and query protocol.
// Falls back to the group profile when the child has no override. Steam/FSM still uses groupProfile.
func (o *Orchestrator) childProfile(group config.GroupConfig, child config.ServerEndpoint, groupProfile *profiles.Profile) (*profiles.Profile, error) {
	name := group.ChildProfileName(child)
	if name == group.Profile {
		return groupProfile, nil
	}
	return profiles.Load(name)
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
