package update

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kandru/pelican-docker-mount-updater/internal/a2s"
	"github.com/kandru/pelican-docker-mount-updater/internal/config"
	"github.com/kandru/pelican-docker-mount-updater/internal/mount"
	"github.com/kandru/pelican-docker-mount-updater/internal/profiles"
	"github.com/kandru/pelican-docker-mount-updater/internal/ui"
	"github.com/kandru/pelican-docker-mount-updater/internal/util"
)

func (o *Orchestrator) resolveGroups(groupName string) ([]config.GroupConfig, error) {
	if groupName == "" {
		return o.cfg.Groups, nil
	}
	g, err := o.cfg.GroupByName(groupName)
	if err != nil {
		return nil, err
	}
	return []config.GroupConfig{*g}, nil
}

func (o *Orchestrator) Test(groupName string) error {
	groups, err := o.resolveGroups(groupName)
	if err != nil {
		return err
	}

	o.log.Heading("Connectivity")
	testedApps := map[int]bool{}
	for _, group := range groups {
		profile, err := profiles.Load(group.Profile)
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
		profile, err := profiles.Load(group.Profile)
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

	o.log.Step(ui.StatusOK, "%-5s %s  %s  %s uptime %s  %s",
		role, short, addr, res.CurrentState, uptime.Round(time.Second), playerLabel(players, maxPlayers))
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
		profile, err := profiles.Load(group.Profile)
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
	groups, err := o.resolveGroups(groupName)
	if err != nil {
		return err
	}

	syncer := mount.New(o.cfg.Paths.Volumes, o.log)
	for _, group := range groups {
		profile, err := profiles.Load(group.Profile)
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
