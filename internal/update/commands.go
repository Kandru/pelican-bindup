package update

import (
	"fmt"
	"os"
	"sync"
	"time"

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
		if !profile.SteamEnabled() || testedApps[profile.SteamAppID] {
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

	o.eachGroup(groups, func(group config.GroupConfig, profile *profiles.Profile) error {
		o.log.Section("Group " + group.Name)

		mainVol := o.mainVolume(group)
		if !profile.SteamEnabled() {
			o.log.Detail("steam checks disabled (profile %s)", group.Profile)
			if _, err := os.Stat(mainVol); err != nil {
				o.log.Step(ui.StatusWarn, "main volume %s: %v", util.ShortUUID(group.Main.UUID), err)
			} else {
				o.log.Step(ui.StatusOK, "main volume present")
			}
		} else if _, err := os.Stat(mainVol); err != nil {
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

		o.testServer(group.Main, profile, "main")
		for _, child := range group.Children {
			childProf, err := o.childProfile(group, child, profile)
			if err != nil {
				o.log.Step(ui.StatusError, "child %s profile: %v", util.ShortUUID(child.UUID), err)
				continue
			}
			o.testServer(child, childProf, "child")
		}
		return nil
	})

	if o.discord.Enabled() {
		o.log.Step(ui.StatusStart, "Discord webhook test")
		if err := o.discord.SendTest(); err != nil {
			o.log.Step(ui.StatusError, "discord: %v", err)
		} else {
			o.log.Step(ui.StatusOK, "discord test notification sent")
		}
	}

	return o.finish("test")
}

func (o *Orchestrator) testServer(srv config.ServerEndpoint, profile *profiles.Profile, role string) {
	short := util.ShortUUID(srv.UUID)
	addr := fmt.Sprintf("%s:%d", srv.QueryHost, srv.QueryPort)

	res, err := o.client.GetResources(srv.UUID)
	if err != nil {
		o.log.Step(ui.StatusError, "%-5s %s  %s  pelican: %v", role, short, addr, err)
		return
	}
	uptime := time.Duration(res.Uptime) * time.Millisecond

	players, maxPlayers, qerr := profile.QueryPlayers(srv.QueryHost, srv.QueryPort)
	if qerr != nil {
		o.log.Step(ui.StatusWarn, "%-5s %s  %s  %s uptime %s  %s: %v",
			role, short, addr, res.CurrentState, uptime.Round(time.Second), profile.QueryProtocol, qerr)
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
		for _, child := range group.Children {
			o.log.Detail("child %s synced=%s", util.ShortUUID(child.UUID), syncedLabel(gs.ChildSynced[child.UUID]))
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
	return o.finish("status")
}

func (o *Orchestrator) CheckUpdate() error {
	o.eachGroup(o.cfg.Groups, func(group config.GroupConfig, profile *profiles.Profile) error {
		o.log.Section("Group " + group.Name)
		if !profile.SteamEnabled() {
			o.log.Step(ui.StatusOK, "steam checks disabled (profile %s)", group.Profile)
			return nil
		}
		mainVol := o.mainVolume(group)
		o.log.Step(ui.StatusStart, "Check Steam buildid (app %d)", profile.SteamAppID)
		info, err := o.steam.Check(profile.SteamAppID, mainVol, profile.ManifestRelative)
		if err != nil {
			o.log.Step(ui.StatusError, "steam check failed: %v", err)
			return nil
		}
		o.log.Detail("remote=%s  local=%s", info.Remote, info.Local)
		if info.Update {
			o.log.Step(ui.StatusOK, "update available")
		} else {
			o.log.Step(ui.StatusOK, "up to date")
		}
		return nil
	})
	return o.finish("check-update")
}

const syncConcurrency = 4

func (o *Orchestrator) Sync(groupName string) error {
	groups, err := o.resolveGroups(groupName)
	if err != nil {
		return err
	}

	syncer := mount.New(o.cfg.Paths.Volumes, o.log)
	apply := o.log.IsMutating()
	sem := make(chan struct{}, syncConcurrency)
	var wg sync.WaitGroup

	o.eachGroup(groups, func(group config.GroupConfig, profile *profiles.Profile) error {
		o.log.Section(fmt.Sprintf("Group %s  ·  sync", group.Name))
		for _, child := range group.Children {
			childProf, err := o.childProfile(group, child, profile)
			if err != nil {
				o.log.Step(ui.StatusError, "sync %s profile: %v", util.ShortUUID(child.UUID), err)
				continue
			}
			wg.Add(1)
			go func(mainUUID, childUUID string, profile *profiles.Profile) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if err := syncer.Sync(mainUUID, childUUID, profile, apply); err != nil {
					o.log.Step(ui.StatusError, "sync %s: %v", util.ShortUUID(childUUID), err)
				}
			}(group.Main.UUID, child.UUID, childProf)
		}
		return nil
	})
	wg.Wait()

	return o.finish("sync")
}
