package update

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/kalle/pelican-docker-mount-updater/internal/a2s"
	"github.com/kalle/pelican-docker-mount-updater/internal/config"
	"github.com/kalle/pelican-docker-mount-updater/internal/discord"
	"github.com/kalle/pelican-docker-mount-updater/internal/mount"
	"github.com/kalle/pelican-docker-mount-updater/internal/pelican"
	"github.com/kalle/pelican-docker-mount-updater/internal/profiles"
	"github.com/kalle/pelican-docker-mount-updater/internal/state"
	"github.com/kalle/pelican-docker-mount-updater/internal/steam"
	"github.com/kalle/pelican-docker-mount-updater/internal/ui"
	"github.com/kalle/pelican-docker-mount-updater/internal/util"
)

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

	profile, err := o.cfg.LoadProfile(group.Profile)
	if err != nil {
		return err
	}
	mainVol := filepath.Join(o.cfg.Paths.Volumes, group.Main.UUID)

	switch gs.Phase {
	case state.PhaseIdle:
		return o.tickIdle(group, profile, mainVol, gs)
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

func (o *Orchestrator) tickIdle(group config.GroupConfig, profile *profiles.Profile, mainVol string, gs *state.GroupState) error {
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

	mainBehind := remote != "" && (local == "" || buildIDLess(local, remote))
	kidsBehind := local != "" && gs.SyncedBuildID != "" && gs.SyncedBuildID != local

	switch {
	case mainBehind:
		o.log.Step(ui.StatusOK, "update available (buildid %s)", remote)
		o.recordPending(group, gs, remote)
	case kidsBehind:
		o.log.Step(ui.StatusOK, "children behind main (buildid %s, synced=%s)", local, gs.SyncedBuildID)
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

	if local != "" && !buildIDLess(local, gs.TargetBuildID) {
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

func buildIDLess(a, b string) bool {
	ai, aerr := strconv.ParseInt(a, 10, 64)
	bi, berr := strconv.ParseInt(b, 10, 64)
	if aerr != nil || berr != nil {
		return a < b
	}
	return ai < bi
}

func (o *Orchestrator) shouldCheckUpdate(group config.GroupConfig, gs *state.GroupState) bool {
	if gs.LastUpdateCheck.IsZero() {
		return true
	}
	return time.Since(gs.LastUpdateCheck) >= group.UpdateCheckInterval()
}

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

	o.log.Detail("pending_children=%d  target=%s", len(gs.PendingChildren), gs.TargetBuildID)

	if !o.log.IsMutating() {
		for _, id := range gs.PendingChildren {
			if child := group.ChildByUUID(id); child != nil {
				o.log.Step(ui.StatusDry, "would stop → sync → start child %s (%s:%d)",
					util.ShortUUID(id), child.QueryHost, child.QueryPort)
			}
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

func (o *Orchestrator) serverEmpty(srv config.ServerEndpoint, label string) bool {
	players, maxPlayers, qerr := a2s.Query(srv.QueryHost, srv.QueryPort)
	if qerr != nil {
		o.log.Step(ui.StatusStart, "Query %s  %s:%d", label, srv.QueryHost, srv.QueryPort)
		o.log.Detail("A2S: %v (treating as empty)", qerr)
		return true
	}
	playerLabel := fmt.Sprintf("%d players", players)
	if maxPlayers > 0 {
		playerLabel = fmt.Sprintf("%d/%d players", players, maxPlayers)
	}
	if players == 0 {
		o.log.Step(ui.StatusOK, "%s  %s:%d  empty", label, srv.QueryHost, srv.QueryPort)
		return true
	}
	o.log.Step(ui.StatusWait, "%s  %s:%d  %s", label, srv.QueryHost, srv.QueryPort, playerLabel)
	return false
}

func (o *Orchestrator) maybeMaintenance(group config.GroupConfig, gs *state.GroupState) {
	if !group.Maintenance.Enabled {
		o.log.Detail("maintenance disabled")
		return
	}
	interval := group.MaintenanceInterval()
	o.log.Step(ui.StatusStart, "Maintenance check (interval %s)", interval)
	servers := append([]config.ServerEndpoint{group.Main}, group.Children...)
	rebooted := 0

	for _, srv := range servers {
		short := util.ShortUUID(srv.UUID)
		if last, ok := gs.LastMaintenance[srv.UUID]; ok {
			if t, err := time.Parse(time.RFC3339, last); err == nil && time.Since(t) < interval {
				o.log.Detail("skip %s (rebooted %s ago)", short, time.Since(t).Round(time.Minute))
				continue
			}
		}

		res, err := o.client.GetResources(srv.UUID)
		if err != nil {
			o.log.Step(ui.StatusWarn, "maintenance %s: %v", short, err)
			continue
		}
		if res.CurrentState != "running" {
			o.log.Detail("skip %s (state=%s)", short, res.CurrentState)
			continue
		}
		uptime := time.Duration(res.Uptime) * time.Millisecond
		if uptime < interval {
			o.log.Detail("skip %s (uptime %s)", short, uptime.Round(time.Minute))
			continue
		}
		if !o.serverEmpty(srv, "maintenance "+short) {
			continue
		}

		o.log.Step(ui.StatusStart, "maintenance restart %s (uptime %s)", short, uptime.Round(time.Minute))
		if !o.log.IsMutating() {
			o.log.Step(ui.StatusDry, "would maintenance restart %s", short)
			continue
		}
		if err := o.client.Power(srv.UUID, "restart"); err != nil {
			o.log.Step(ui.StatusError, "maintenance restart %s failed: %v", short, err)
			continue
		}
		gs.RecordRestart(srv.UUID)
		rebooted++
		o.log.Step(ui.StatusOK, "maintenance restart sent for %s", short)
	}

	if rebooted == 0 {
		o.log.Step(ui.StatusOK, "maintenance: no reboots needed")
	}
}

func (o *Orchestrator) Test(groupName string) error {
	groups := o.cfg.Groups
	if groupName != "" {
		g, err := o.cfg.GroupByName(groupName)
		if err != nil {
			return err
		}
		groups = []config.GroupConfig{*g}
	}

	o.log.Heading("Connectivity")
	testedApps := map[int]bool{}
	for _, group := range groups {
		profile, err := o.cfg.LoadProfile(group.Profile)
		if err != nil {
			o.log.Step(ui.StatusError, "profile %s: %v", group.Profile, err)
			continue
		}
		if testedApps[profile.SteamAppID] {
			continue
		}
		testedApps[profile.SteamAppID] = true
		buildID, err := o.steam.Ping(profile.SteamAppID)
		if err != nil {
			o.log.Step(ui.StatusError, "steam %s app %d: %v", o.cfg.Steam.InfoAPI, profile.SteamAppID, err)
			continue
		}
		o.log.Step(ui.StatusOK, "steam app %d  buildid %s", profile.SteamAppID, buildID)
	}

	for _, group := range groups {
		profile, err := o.cfg.LoadProfile(group.Profile)
		if err != nil {
			return err
		}
		o.log.Section("Group " + group.Name)

		mainVol := filepath.Join(o.cfg.Paths.Volumes, group.Main.UUID)
		if _, err := os.Stat(mainVol); err != nil {
			o.log.Step(ui.StatusWarn, "main volume %s: %v", util.ShortUUID(group.Main.UUID), err)
		} else if info, err := o.steam.Check(profile.SteamAppID, mainVol, profile.ManifestRelative); err != nil {
			o.log.Step(ui.StatusWarn, "main install: %v", err)
		} else {
			switch {
			case info.Local == "":
				o.log.Step(ui.StatusWarn, "main install: no local manifest")
			case info.Remote == "":
				o.log.Detail("main install local buildid=%s", info.Local)
			case info.Local == info.Remote:
				o.log.Step(ui.StatusOK, "main install up to date  buildid %s", info.Local)
			default:
				o.log.Step(ui.StatusWarn, "main install outdated  local=%s  remote=%s", info.Local, info.Remote)
			}
		}

		o.testServer(group.Main, "main")
		for _, child := range group.Children {
			o.testServer(child, "child")
		}
	}

	if o.discord.Enabled() {
		o.log.Step(ui.StatusStart, "Discord webhook test")
		if err := o.discord.SendTest(); err != nil {
			o.log.Step(ui.StatusError, "discord: %v", err)
		} else {
			o.log.Step(ui.StatusOK, "discord test notification sent")
		}
	}

	o.log.Summary()
	if o.log.Errors() > 0 {
		return fmt.Errorf("test failed with %d error(s)", o.log.Errors())
	}
	return nil
}

func (o *Orchestrator) testServer(srv config.ServerEndpoint, role string) {
	short := util.ShortUUID(srv.UUID)
	addr := fmt.Sprintf("%s:%d", srv.QueryHost, srv.QueryPort)

	res, err := o.client.GetResources(srv.UUID)
	if err != nil {
		o.log.Step(ui.StatusError, "%-5s %s  %s  pelican: %v", role, short, addr, err)
		return
	}
	uptime := time.Duration(res.Uptime) * time.Millisecond

	players, maxPlayers, qerr := a2s.Query(srv.QueryHost, srv.QueryPort)
	if qerr != nil {
		o.log.Step(ui.StatusWarn, "%-5s %s  %s  %s uptime %s  a2s: %v",
			role, short, addr, res.CurrentState, uptime.Round(time.Second), qerr)
		return
	}

	playerLabel := fmt.Sprintf("%d players", players)
	if maxPlayers > 0 {
		playerLabel = fmt.Sprintf("%d/%d players", players, maxPlayers)
	}
	o.log.Step(ui.StatusOK, "%-5s %s  %s  %s uptime %s  %s",
		role, short, addr, res.CurrentState, uptime.Round(time.Second), playerLabel)
}

func (o *Orchestrator) Status() error {
	for _, group := range o.cfg.Groups {
		gs, err := o.store.Load(group.Name)
		if err != nil {
			o.log.Step(ui.StatusError, "group %s: %v", group.Name, err)
			continue
		}
		o.log.Section("Group " + group.Name)
		o.log.Detail("phase=%s", gs.Phase)
		if !gs.LastUpdateCheck.IsZero() {
			o.log.Detail("last_steam_check=%s", gs.LastUpdateCheck.Local().Format("2006-01-02 15:04:05"))
		}
		o.log.Detail("update_check_interval=%s", group.UpdateCheckInterval())
		o.log.Detail("defer_update=%s", group.DeferUpdateInterval())
		if group.Maintenance.Enabled {
			o.log.Detail("maintenance_reboot_interval=%s", group.MaintenanceInterval())
		}
		if gs.CachedRemoteBuildID != "" {
			o.log.Detail("cached_remote_buildid=%s", gs.CachedRemoteBuildID)
		}
		if gs.SyncedBuildID != "" {
			o.log.Detail("synced_buildid=%s", gs.SyncedBuildID)
		}
		if gs.TargetBuildID != "" {
			o.log.Detail("target_buildid=%s", gs.TargetBuildID)
		}
		if !gs.UpdateDetectedAt.IsZero() {
			o.log.Detail("update_detected_at=%s", gs.UpdateDetectedAt.Local().Format("2006-01-02 15:04:05"))
		}
		if len(gs.PendingChildren) > 0 {
			o.log.Detail("pending_children=%d", len(gs.PendingChildren))
		}
		if res, err := o.client.GetResources(group.Main.UUID); err != nil {
			o.log.Detail("main state: %v", err)
		} else {
			o.log.Detail("main state=%s uptime=%s", res.CurrentState, time.Duration(res.Uptime)*time.Millisecond)
		}
	}
	o.log.Summary()
	return nil
}

func (o *Orchestrator) CheckUpdate() error {
	for _, group := range o.cfg.Groups {
		profile, err := o.cfg.LoadProfile(group.Profile)
		if err != nil {
			return err
		}
		o.log.Section("Group " + group.Name)
		mainVol := filepath.Join(o.cfg.Paths.Volumes, group.Main.UUID)
		o.log.Step(ui.StatusStart, "Check Steam buildid (app %d)", profile.SteamAppID)
		info, err := o.steam.Check(profile.SteamAppID, mainVol, profile.ManifestRelative)
		if err != nil {
			o.log.Step(ui.StatusError, "steam check failed: %v", err)
			continue
		}
		o.log.Detail("remote=%s  local=%s", info.Remote, info.Local)
		if info.Update {
			o.log.Step(ui.StatusOK, "update available")
		} else {
			o.log.Step(ui.StatusOK, "up to date")
		}
	}
	o.log.Summary()
	return nil
}

func (o *Orchestrator) Sync(groupName string) error {
	groups := o.cfg.Groups
	if groupName != "" {
		g, err := o.cfg.GroupByName(groupName)
		if err != nil {
			return err
		}
		groups = []config.GroupConfig{*g}
	}

	syncer := mount.New(o.cfg.Paths.Volumes, o.log)
	for _, group := range groups {
		profile, err := o.cfg.LoadProfile(group.Profile)
		if err != nil {
			return err
		}
		o.log.Section(fmt.Sprintf("Group %s  ·  sync", group.Name))
		for _, child := range group.Children {
			if err := syncer.Sync(group.Main.UUID, child.UUID, profile, o.log.IsMutating()); err != nil {
				o.log.Step(ui.StatusError, "sync %s: %v", util.ShortUUID(child.UUID), err)
			}
		}
	}
	o.log.Summary()
	if o.log.Errors() > 0 {
		return fmt.Errorf("sync completed with errors")
	}
	return nil
}

func AcquireLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := flock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another instance is running (lock: %s)", path)
	}
	return func() { _ = f.Close() }, nil
}
