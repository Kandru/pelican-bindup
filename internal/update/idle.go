package update

import (
	"path/filepath"
	"time"

	"github.com/derkalle4/pelican-docker-mount-updater/internal/config"
	"github.com/derkalle4/pelican-docker-mount-updater/internal/profiles"
	"github.com/derkalle4/pelican-docker-mount-updater/internal/state"
	"github.com/derkalle4/pelican-docker-mount-updater/internal/steam"
	"github.com/derkalle4/pelican-docker-mount-updater/internal/ui"
)

func (o *Orchestrator) tickIdle(group config.GroupConfig, profile *profiles.Profile, gs *state.GroupState) error {
	mainVol := filepath.Join(o.cfg.Paths.Volumes, group.Main.UUID)
	if err := o.evaluateUpdateState(group, profile, mainVol, gs); err != nil {
		return err
	}

	if gs.TargetBuildID != "" {
		return o.proceedPendingUpdate(group, profile, mainVol, gs)
	}

	o.maybeMaintenance(group, gs)
	return o.store.Save(group.Name, gs)
}

func (o *Orchestrator) evaluateUpdateState(group config.GroupConfig, profile *profiles.Profile, mainVol string, gs *state.GroupState) error {
	var remote, local string

	if o.shouldCheckUpdate(group, gs) {
		o.log.Step(ui.StatusStart, "Check Steam buildid (app %d)", profile.SteamAppID)
		info, err := o.steam.Check(profile.SteamAppID, mainVol, profile.ManifestRelative)
		gs.LastUpdateCheck = time.Now().UTC()
		if err != nil {
			o.log.Step(ui.StatusError, "steam check failed: %v", err)
			return err
		}
		remote, local = info.Remote, info.Local
		if remote != "" {
			gs.CachedRemoteBuildID = remote
		}
	} else {
		remaining := group.UpdateCheckInterval() - time.Since(gs.LastUpdateCheck)
		if remaining < 0 {
			remaining = 0
		}
		o.log.Step(ui.StatusWait, "Steam check skipped (next in %s)", remaining.Round(time.Minute))
		var err error
		local, err = o.steam.LocalBuildID(mainVol, profile.ManifestRelative)
		if err != nil {
			o.log.Step(ui.StatusError, "read main buildid: %v", err)
			return err
		}
		remote = gs.CachedRemoteBuildID
	}

	o.log.Detail("remote=%s  local=%s  synced=%s", remote, local, gs.SyncedBuildID)

	if local != "" && gs.SyncedBuildID == "" {
		gs.SyncedBuildID = local
		o.log.Detail("bootstrapped synced_buildid=%s", local)
	}

	mainBehind := remote != "" && (local == "" || steam.Less(local, remote))
	kidsBehind := local != "" && o.anyChildBehind(group, profile, local)

	switch {
	case mainBehind:
		o.log.Step(ui.StatusOK, "update available (buildid %s)", remote)
		o.recordPending(group, gs, remote)
	case kidsBehind:
		o.log.Step(ui.StatusOK, "children behind main (buildid %s)", local)
		o.recordPending(group, gs, local)
	default:
		o.log.Step(ui.StatusOK, "no update needed")
		if gs.TargetBuildID != "" {
			o.log.Detail("clearing stale pending update")
			gs.ClearUpdatePending()
		}
	}
	return nil
}

func (o *Orchestrator) anyChildBehind(group config.GroupConfig, profile *profiles.Profile, target string) bool {
	for _, child := range group.Children {
		if o.childNeedsSync(child.UUID, profile, target) {
			return true
		}
	}
	return false
}

func (o *Orchestrator) recordPending(group config.GroupConfig, gs *state.GroupState, target string) {
	if gs.UpdateDetectedAt.IsZero() {
		gs.UpdateDetectedAt = time.Now().UTC()
		gs.TargetBuildID = target
		o.log.Detail("defer started (%s)", group.DeferUpdateInterval())
		return
	}
	if gs.TargetBuildID != target {
		o.log.Detail("target refined %s → %s", gs.TargetBuildID, target)
		gs.TargetBuildID = target
		return
	}
	o.log.Detail("defer in progress since %s", gs.UpdateDetectedAt.Local().Format("2006-01-02 15:04:05"))
}

func (o *Orchestrator) proceedPendingUpdate(group config.GroupConfig, profile *profiles.Profile, mainVol string, gs *state.GroupState) error {
	remaining := group.DeferUpdateInterval() - time.Since(gs.UpdateDetectedAt)
	if remaining > 0 {
		o.log.Step(ui.StatusWait, "update defer (%s remaining, target=%s)", remaining.Round(time.Minute), gs.TargetBuildID)
		return o.store.Save(group.Name, gs)
	}

	local, err := o.steam.LocalBuildID(mainVol, profile.ManifestRelative)
	if err != nil {
		o.log.Step(ui.StatusError, "read main buildid: %v", err)
		return err
	}

	if local != "" && !steam.Less(local, gs.TargetBuildID) {
		o.log.Step(ui.StatusOK, "main already on target %s — syncing children", gs.TargetBuildID)
		if !o.log.IsMutating() {
			o.log.Step(ui.StatusDry, "would sync children")
			return o.store.Save(group.Name, gs)
		}
		flagChildren(group, gs)
		if err := o.store.Save(group.Name, gs); err != nil {
			return err
		}
		return o.tickAwaitChildren(group, profile, gs)
	}

	o.log.Step(ui.StatusOK, "defer elapsed — waiting for empty main to restart")
	if !o.log.IsMutating() {
		o.log.Step(ui.StatusDry, "would restart main when empty")
		return o.store.Save(group.Name, gs)
	}

	gs.Phase = state.PhaseAwaitMainEmpty
	if err := o.store.Save(group.Name, gs); err != nil {
		return err
	}
	return o.tickAwaitMainEmpty(group, gs)
}

func (o *Orchestrator) shouldCheckUpdate(group config.GroupConfig, gs *state.GroupState) bool {
	if gs.LastUpdateCheck.IsZero() {
		return true
	}
	return time.Since(gs.LastUpdateCheck) >= group.UpdateCheckInterval()
}
