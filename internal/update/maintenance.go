package update

import (
	"time"

	"github.com/kandru/pelican-docker-mount-updater/internal/config"
	"github.com/kandru/pelican-docker-mount-updater/internal/state"
	"github.com/kandru/pelican-docker-mount-updater/internal/ui"
	"github.com/kandru/pelican-docker-mount-updater/internal/util"
)

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
