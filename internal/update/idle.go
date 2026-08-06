package update

import (
	"path/filepath"
	"time"

	"github.com/kandru/pelican-docker-mount-updater/internal/config"
	"github.com/kandru/pelican-docker-mount-updater/internal/profiles"
	"github.com/kandru/pelican-docker-mount-updater/internal/state"
	"github.com/kandru/pelican-docker-mount-updater/internal/steam"
	"github.com/kandru/pelican-docker-mount-updater/internal/ui"
	"github.com/kandru/pelican-docker-mount-updater/internal/util"
)

func (o *Orchestrator) tickIdle(group config.GroupConfig, profile *profiles.Profile, gs *state.GroupState) error {
	if !profile.SteamEnabled() {
		o.log.Detail("steam checks disabled for profile %s", group.Profile)
		if gs.TargetBuildID != "" {
			o.log.Detail("clearing stale pending update (steam disabled)")
			gs.ClearUpdatePending()
			gs.PendingChildren = nil
		}
		o.maybeMaintenance(group, profile, gs)
		return o.store.Save(group.Name, gs)
	}

	mainVol := filepath.Join(o.cfg.Paths.Volumes, group.Main.UUID)
	if err := o.evaluateUpdateState(group, profile, mainVol, gs); err != nil {
		return err
	}

	if gs.TargetBuildID != "" {
		return o.proceedPendingUpdate(group, profile, mainVol, gs)
	}

	o.maybeMaintenance(group, profile, gs)
	return o.store.Save(group.Name, gs)
}

func (o *Orchestrator) evaluateUpdateState(group config.GroupConfig, profile *profiles.Profile, mainVol string, gs *state.GroupState) error {
	var remote, local string

	if o.shouldCheckUpdate(group, gs) {
		o.log.Step(ui.StatusStart, "Check Steam buildid on main (app %d)", profile.SteamAppID)
		info, err := o.steam.Check(profile.SteamAppID, mainVol, profile.ManifestRelative)
		if err != nil {
			o.log.Step(ui.StatusError, "steam check failed: %v", err)
			return err
		}
		gs.LastUpdateCheck = time.Now().UTC()
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

	o.log.Detail("main remote=%s  local=%s  group_synced=%s", remote, local, gs.SyncedBuildID)
	o.bootstrapSynced(group, gs, local)
	o.logChildSyncStatus(group, gs, local)

	mainBehind := remote != "" && (local == "" || steam.Less(local, remote))
	kidsBehind := local != "" && o.anyChildBehind(group, gs, local)

	switch {
	case mainBehind:
		o.log.Step(ui.StatusOK, "main update available (buildid %s) — will update main first", remote)
		o.recordPending(group, gs, remote)
	case kidsBehind:
		o.log.Step(ui.StatusOK, "main on %s — children still need sync", local)
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

// bootstrapSynced seeds group + per-child markers once so first install does not mass-restart.
// New children added later are left unsynced so the kids-behind path runs mount sync after defer.
func (o *Orchestrator) bootstrapSynced(group config.GroupConfig, gs *state.GroupState, mainLocal string) {
	if mainLocal == "" {
		return
	}
	wasUninitialized := gs.SyncedBuildID == ""
	if wasUninitialized {
		gs.SyncedBuildID = mainLocal
		o.log.Detail("bootstrapped group synced_buildid=%s", mainLocal)
	}
	for _, child := range group.Children {
		if _, ok := gs.ChildSynced[child.UUID]; ok {
			continue
		}
		if wasUninitialized && gs.Phase == state.PhaseIdle && gs.TargetBuildID == "" {
			gs.MarkChildSynced(child.UUID, mainLocal)
			o.log.Detail("bootstrapped child %s synced=%s", util.ShortUUID(child.UUID), mainLocal)
		}
	}
}

func (o *Orchestrator) logChildSyncStatus(group config.GroupConfig, gs *state.GroupState, target string) {
	if target == "" || len(group.Children) == 0 {
		return
	}
	o.log.Step(ui.StatusStart, "Check children vs main buildid %s", target)
	for _, child := range group.Children {
		short := util.ShortUUID(child.UUID)
		synced := gs.ChildSynced[child.UUID]
		if synced == "" {
			synced = "(none)"
		}
		if gs.ChildNeedsSync(child.UUID, target) {
			o.log.Detail("child %s  synced=%s  needs sync", short, synced)
		} else {
			o.log.Detail("child %s  synced=%s  ok", short, synced)
		}
	}
}

func (o *Orchestrator) anyChildBehind(group config.GroupConfig, gs *state.GroupState, target string) bool {
	for _, child := range group.Children {
		if gs.ChildNeedsSync(child.UUID, target) {
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

	// Main already on target → only children remain (e.g. maintenance restarted main).
	if local != "" && !steam.Less(local, gs.TargetBuildID) {
		return o.advanceToChildren(group, profile, gs, gs.SyncedBuildID, "syncing children next")
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
	return o.tickAwaitMainEmpty(group, profile, gs)
}

func (o *Orchestrator) shouldCheckUpdate(group config.GroupConfig, gs *state.GroupState) bool {
	if gs.LastUpdateCheck.IsZero() {
		return true
	}
	return time.Since(gs.LastUpdateCheck) >= group.UpdateCheckInterval()
}
