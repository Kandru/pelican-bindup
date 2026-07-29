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

func (o *Orchestrator) tickAwaitMainEmpty(group config.GroupConfig, gs *state.GroupState) error {
	o.log.Detail("target_buildid=%s", gs.TargetBuildID)
	if !o.serverEmpty(group.Main, "main") {
		o.log.Step(ui.StatusWait, "main not empty — retry next run")
		return o.store.Save(group.Name, gs)
	}

	o.log.Step(ui.StatusStart, "Restart main %s", util.ShortUUID(group.Main.UUID))
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
	o.log.Step(ui.StatusOK, "main restart sent — %d child(ren) pending sync", len(gs.PendingChildren))
	o.notifyDiscord(fmt.Sprintf("**%s** — main server restarted for Steam update (buildid %s)", group.Name, gs.TargetBuildID))
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
	if target == "" {
		mainVol := filepath.Join(o.cfg.Paths.Volumes, group.Main.UUID)
		if local, err := o.steam.LocalBuildID(mainVol, profile.ManifestRelative); err == nil {
			target = local
		}
	}

	o.log.Detail("pending_children=%d  target=%s", len(gs.PendingChildren), target)

	if !o.log.IsMutating() {
		for _, id := range gs.PendingChildren {
			child := group.ChildByUUID(id)
			if child == nil {
				continue
			}
			if target != "" && !o.childNeedsSync(id, profile, target) {
				o.log.Step(ui.StatusOK, "child %s already on buildid %s", util.ShortUUID(id), target)
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

		if target != "" && !o.childNeedsSync(childUUID, profile, target) {
			bid, _ := o.childBuildID(childUUID, profile)
			o.log.Step(ui.StatusOK, "child %s already on buildid %s — skip", util.ShortUUID(childUUID), bid)
			continue
		}

		if !o.serverEmpty(*child, "child "+util.ShortUUID(childUUID)) {
			remaining = append(remaining, childUUID)
			continue
		}
		if err := o.syncChild(group, childUUID, profile, syncer, gs); err != nil {
			o.log.Step(ui.StatusError, "child %s: %v", util.ShortUUID(childUUID), err)
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

func (o *Orchestrator) childBuildID(childUUID string, profile *profiles.Profile) (string, error) {
	vol := filepath.Join(o.cfg.Paths.Volumes, childUUID)
	return o.steam.LocalBuildID(vol, profile.ManifestRelative)
}

// childNeedsSync is true when the child volume has no buildid or an older one than target.
func (o *Orchestrator) childNeedsSync(childUUID string, profile *profiles.Profile, target string) bool {
	if target == "" {
		return true
	}
	bid, err := o.childBuildID(childUUID, profile)
	if err != nil || bid == "" {
		return true
	}
	return steam.Less(bid, target)
}

func (o *Orchestrator) syncChild(group config.GroupConfig, childUUID string, profile *profiles.Profile, syncer *mount.Syncer, gs *state.GroupState) error {
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
	gs.RecordRestart(childUUID)
	o.log.Step(ui.StatusOK, "child %s started", short)
	o.notifyDiscord(fmt.Sprintf("**%s** — child `%s` synced and restarted (buildid %s)", group.Name, short, gs.TargetBuildID))
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
