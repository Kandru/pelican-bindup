package update

import (
	"time"

	"github.com/kandru/pelican-bindup/internal/config"
	"github.com/kandru/pelican-bindup/internal/profiles"
	"github.com/kandru/pelican-bindup/internal/state"
	"github.com/kandru/pelican-bindup/internal/ui"
	"github.com/kandru/pelican-bindup/internal/util"
)

func (o *Orchestrator) maybeReboot(group config.GroupConfig, profile *profiles.Profile, gs *state.GroupState) {
	if !group.Reboot.Enabled {
		o.log.Detail("reboot disabled")
		return
	}
	o.log.Step(ui.StatusStart, "Reboot check (default interval %s)", group.Reboot.Interval())
	servers := append([]config.ServerEndpoint{group.Main}, group.Children...)
	rebooted := 0

	for _, srv := range servers {
		short := util.ShortUUID(srv.UUID)
		reboot := group.RebootFor(srv)
		if !reboot.Enabled {
			o.log.Detail("skip %s (reboot disabled)", short)
			continue
		}
		interval := reboot.Interval()
		if last, ok := gs.LastReboot[srv.UUID]; ok {
			if t, err := time.Parse(time.RFC3339, last); err == nil && time.Since(t) < interval {
				o.log.Detail("skip %s (rebooted %s ago)", short, time.Since(t).Round(time.Minute))
				continue
			}
		}

		res, err := o.client.GetResources(srv.UUID)
		if err != nil {
			o.log.Step(ui.StatusWarn, "reboot %s: %v", short, err)
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
		queryProf := profile
		if srv.UUID != group.Main.UUID {
			childProf, err := o.childProfile(group, srv, profile)
			if err != nil {
				o.log.Step(ui.StatusWarn, "reboot %s profile: %v", short, err)
				continue
			}
			queryProf = childProf
		}
		if !o.serverEmpty(srv, queryProf, "reboot "+short) {
			continue
		}

		o.log.Step(ui.StatusStart, "reboot %s (uptime %s)", short, uptime.Round(time.Minute))
		if !o.log.IsMutating() {
			o.log.Step(ui.StatusDry, "would reboot %s", short)
			continue
		}
		if err := o.client.Power(srv.UUID, "restart"); err != nil {
			o.log.Step(ui.StatusError, "reboot %s failed: %v", short, err)
			continue
		}
		gs.RecordRestart(srv.UUID)
		rebooted++
		o.log.Step(ui.StatusOK, "reboot sent for %s", short)
	}

	if rebooted == 0 {
		o.log.Step(ui.StatusOK, "reboot: none needed")
	}
}
