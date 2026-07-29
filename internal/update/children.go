package update

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/kandru/pelican-docker-mount-updater/internal/config"
	"github.com/kandru/pelican-docker-mount-updater/internal/mount"
	"github.com/kandru/pelican-docker-mount-updater/internal/profiles"
	"github.com/kandru/pelican-docker-mount-updater/internal/state"
	"github.com/kandru/pelican-docker-mount-updater/internal/steam"
	"github.com/kandru/pelican-docker-mount-updater/internal/ui"
	"github.com/kandru/pelican-docker-mount-updater/internal/util"
)

func (o *Orchestrator) tickAwaitMainEmpty(group config.GroupConfig, profile *profiles.Profile, gs *state.GroupState) error {
	o.log.Detail("target_buildid=%s", gs.TargetBuildID)

	mainVol := filepath.Join(o.cfg.Paths.Volumes, group.Main.UUID)
	local, err := o.steam.LocalBuildID(mainVol, profile.ManifestRelative)
	if err != nil {
		o.log.Step(ui.StatusError, "read main buildid: %v", err)
		return err
	}

	// Main already updated (e.g. maintenance) while we waited for empty — skip redundant restart.
	if gs.TargetBuildID != "" && local != "" && !steam.Less(local, gs.TargetBuildID) {
		o.log.Step(ui.StatusOK, "main already on target %s — skipping restart, syncing children", gs.TargetBuildID)
		if !o.log.IsMutating() {
			o.log.Step(ui.StatusDry, "would sync children")
			return o.store.Save(group.Name, gs)
		}
		o.notifyUpdated(group.Name, group.Main.UUID, gs.SyncedBuildID, gs.TargetBuildID)
		flagChildren(group, gs)
		if err := o.store.Save(group.Name, gs); err != nil {
			return err
		}
		return o.tickAwaitChildren(group, profile, gs)
	}

	if !o.serverEmpty(group.Main, "main") {
		o.log.Step(ui.StatusWait, "main not empty — retry next run")
		return o.store.Save(group.Name, gs)
	}

	oldBuild := gs.SyncedBuildID
	if local != "" {
		oldBuild = local
	}

	o.log.Step(ui.StatusStart, "Restart main %s (SteamCMD updates on restart)", util.ShortUUID(group.Main.UUID))
	if !o.log.IsMutating() {
		o.log.Step(ui.StatusDry, "would restart main %s", util.ShortUUID(group.Main.UUID))
		return o.store.Save(group.Name, gs)
	}
	if err := o.client.Power(group.Main.UUID, "restart"); err != nil {
		o.log.Step(ui.StatusError, "main restart failed: %v", err)
		return err
	}
	gs.RecordRestart(group.Main.UUID)
	flagChildren(group, gs)
	o.log.Step(ui.StatusOK, "main restart sent — waiting for main buildid then %d child(ren)", len(gs.PendingChildren))
	o.notifyUpdated(group.Name, group.Main.UUID, oldBuild, gs.TargetBuildID)
	return o.store.Save(group.Name, gs)
}

func flagChildren(group config.GroupConfig, gs *state.GroupState) {
	gs.Phase = state.PhaseAwaitChildren
	gs.PendingChildren = make([]string, len(group.Children))
	for i, c := range group.Children {
		gs.PendingChildren[i] = c.UUID
	}
}

func (o *Orchestrator) tickAwaitChildren(group config.GroupConfig, profile *profiles.Profile, gs *state.GroupState) error {
	if len(gs.PendingChildren) == 0 {
		if gs.TargetBuildID != "" {
			o.log.Step(ui.StatusStart, "re-flag children for sync (recovered state)")
			flagChildren(group, gs)
		} else {
			return o.finishChildren(group.Name, gs)
		}
	}

	target := gs.TargetBuildID
	mainVol := filepath.Join(o.cfg.Paths.Volumes, group.Main.UUID)
	mainLocal, err := o.steam.LocalBuildID(mainVol, profile.ManifestRelative)
	if err != nil {
		o.log.Step(ui.StatusError, "read main buildid: %v", err)
		return err
	}
	if target == "" {
		target = mainLocal
	}

	// Do not touch children until main has finished updating to the target buildid.
	if target != "" && (mainLocal == "" || steam.Less(mainLocal, target)) {
		o.log.Step(ui.StatusWait, "waiting for main update (local=%s target=%s)", mainLocal, target)
		return o.store.Save(group.Name, gs)
	}
	if target == "" {
		o.log.Step(ui.StatusWait, "waiting for main buildid after restart")
		return o.store.Save(group.Name, gs)
	}

	o.log.Step(ui.StatusOK, "main on target %s — checking children", target)
	o.log.Detail("pending_children=%d", len(gs.PendingChildren))

	if !o.log.IsMutating() {
		for _, id := range gs.PendingChildren {
			child := group.ChildByUUID(id)
			if child == nil {
				continue
			}
			if !gs.ChildNeedsSync(id, target) {
				o.log.Step(ui.StatusOK, "child %s already synced to %s", util.ShortUUID(id), target)
				continue
			}
			o.log.Step(ui.StatusDry, "would stop → sync → start child %s (%s:%d)",
				util.ShortUUID(id), child.QueryHost, child.QueryPort)
		}
		o.log.Step(ui.StatusWait, "%d child(ren) pending (retry on next run)", len(gs.PendingChildren))
		return o.store.Save(group.Name, gs)
	}

	syncer := mount.New(o.cfg.Paths.Volumes, o.log)
	var remaining []string

	for _, childUUID := range gs.PendingChildren {
		child := group.ChildByUUID(childUUID)
		if child == nil {
			o.log.Step(ui.StatusWarn, "unknown child %s — removing from pending", util.ShortUUID(childUUID))
			continue
		}
		short := util.ShortUUID(childUUID)

		if !gs.ChildNeedsSync(childUUID, target) {
			o.log.Step(ui.StatusOK, "child %s already synced to %s — skip", short, target)
			continue
		}

		o.log.Step(ui.StatusStart, "child %s needs sync to %s", short, target)
		if !o.serverEmpty(*child, "child "+short) {
			remaining = append(remaining, childUUID)
			continue
		}
		if err := o.syncChild(group, childUUID, profile, syncer, gs, target); err != nil {
			o.log.Step(ui.StatusError, "child %s: %v", short, err)
			remaining = append(remaining, childUUID)
		}
	}

	gs.PendingChildren = remaining
	if len(remaining) == 0 {
		return o.finishChildren(group.Name, gs)
	}
	o.log.Step(ui.StatusWait, "%d child(ren) still pending (retry on next run)", len(remaining))
	return o.store.Save(group.Name, gs)
}

func (o *Orchestrator) syncChild(group config.GroupConfig, childUUID string, profile *profiles.Profile, syncer *mount.Syncer, gs *state.GroupState, target string) error {
	short := util.ShortUUID(childUUID)
	o.log.Step(ui.StatusStart, "Stop child %s", short)
	if err := o.client.Power(childUUID, "stop"); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	if err := o.client.WaitForState(childUUID, "offline", 2*time.Minute); err != nil {
		o.log.Detail("wait offline: %v (continuing)", err)
	} else {
		o.log.Step(ui.StatusOK, "child %s offline", short)
	}

	if err := syncer.Sync(group.Main.UUID, childUUID, profile, true); err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	o.log.Step(ui.StatusStart, "Start child %s", short)
	if err := o.client.Power(childUUID, "start"); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	oldBuild := gs.ChildSynced[childUUID]
	gs.RecordRestart(childUUID)
	gs.MarkChildSynced(childUUID, target)
	o.log.Step(ui.StatusOK, "child %s synced and started (buildid %s)", short, target)
	o.notifyUpdated(group.Name, childUUID, oldBuild, target)
	return nil
}

func (o *Orchestrator) finishChildren(groupName string, gs *state.GroupState) error {
	if gs.TargetBuildID != "" {
		gs.SyncedBuildID = gs.TargetBuildID
	}
	gs.Phase = state.PhaseIdle
	gs.ClearUpdatePending()
	o.log.Step(ui.StatusOK, "all children synced — back to idle (synced=%s)", gs.SyncedBuildID)
	return o.store.Save(groupName, gs)
}
