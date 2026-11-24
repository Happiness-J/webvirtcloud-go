package jobs

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"webvirtcloud-go/internal/cluster"
	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/store"
	"webvirtcloud-go/internal/webhook"
)

func (w *Worker) reconcileCluster(ctx context.Context) {
	policy, err := w.store.ClusterPolicy(ctx)
	if err != nil {
		return
	}
	hosts, err := w.store.ListHosts(ctx)
	if err != nil {
		w.log.Warn("list cluster hosts", "error", err)
		return
	}
	previous := map[string]store.HostHealth{}
	if rows, err := w.store.ListHostHealth(ctx); err == nil {
		for _, row := range rows {
			previous[row.HostID] = row
		}
	}
	views := make([]cluster.Host, 0, len(hosts))
	byID := map[string]store.Host{}
	for _, host := range hosts {
		byID[host.ID] = host
		if ctx.Err() != nil {
			return
		}
		view := w.probeHost(ctx, host, previous[host.ID])
		views = append(views, view)
		// History is append-only and read-only. Recording it never places or migrates a VM.
		if view.Reachable {
			if err := w.store.RecordHostCapacity(ctx, store.HostCapacitySample{
				HostID: host.ID, CPUPercent: view.CPUPercent, MemPercent: view.MemPercent, RunningVMs: view.RunningVMs,
			}); err != nil {
				w.log.Warn("record host capacity", "host_id", host.ID, "error", err)
			}
		}
	}
	if _, err := w.store.PruneHostCapacity(ctx, time.Now().Add(-store.HostCapacityRetention)); err != nil && ctx.Err() == nil {
		w.log.Warn("prune host capacity", "error", err)
	}
	if !policy.Enabled {
		return
	}
	if err := w.store.EnsureSystemUser(ctx, store.User{ID: "system:cluster", Username: "system:cluster", PasswordHash: "disabled", Role: "operator"}); err != nil {
		w.log.Warn("ensure cluster user", "error", err)
		return
	}
	protected, err := w.store.ListProtectedVMs(ctx)
	if err != nil {
		return
	}
	for _, item := range protected {
		source := byID[item.HostID]
		health := previous[item.HostID]
		current := findView(views, item.HostID)
		if source.ID == "" || current.Reachable || health.Failures+1 < policy.FailAfter {
			continue
		}
		decision, err := cluster.Place(views, cluster.Request{VMName: item.VMName, SourceHostID: item.HostID, Policy: policy.Policy, RequireShared: true})
		if err != nil {
			w.log.Warn("place protected vm", "vm", item.VMName, "error", err)
			continue
		}
		if err := command.ValidateMigrationURIPublic(decision.URI); err != nil {
			continue
		}
		// The failed host cannot initiate a live migration. virsh define does
		// not start a domain, so create it from the shared XML in one command.
		xmlPath := "/var/lib/libvirt/images/ha/" + item.VMName + ".xml"
		raw, _ := json.Marshal([]string{"create", xmlPath, "--validate"})
		job := store.Job{ID: id.New("job"), UserID: "system:cluster", HostID: decision.HostID, Kind: "vm-action", Label: "故障重启 " + item.VMName, Tool: "virsh", ArgsJSON: string(raw), TimeoutSeconds: 300, DedupeKey: "ha:" + item.HostID + ":" + item.VMName}
		if err := w.store.EnqueueJob(ctx, job); err != nil && !store.IsDuplicateJob(err) {
			w.log.Warn("enqueue failover", "vm", item.VMName, "error", err)
		}
	}
}

func (w *Worker) probeHost(ctx context.Context, host store.Host, previous store.HostHealth) cluster.Host {
	view := cluster.Host{ID: host.ID, Name: host.Name, Enabled: host.Enabled, Maintenance: host.Maintenance}
	uri, err := w.cipher.OpenFor("host:"+host.ID, host.URICiphertext)
	if err != nil {
		return view
	}
	view.URI = strings.TrimSpace(uri)
	probeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	target, err := command.ResolveTarget(view.URI)
	reachable := false
	if err == nil {
		result, runErr := w.runner.Run(probeCtx, target.Request("virsh", []string{"list", "--name"}, "", true, 4*time.Second))
		reachable = runErr == nil
		if reachable {
			view.RunningVMs = countLines(result.Stdout)
			cpu, cpuErr := w.runner.Run(probeCtx, target.Request("virsh", []string{"nodecpustats", "--percent"}, "", true, 4*time.Second))
			mem, memErr := w.runner.Run(probeCtx, target.Request("virsh", []string{"nodememstats"}, "", true, 4*time.Second))
			if cpuErr == nil {
				view.CPUPercent = percent(field(cpu.Stdout, "usage"))
			}
			if memErr == nil {
				total := numberField(mem.Stdout, "total")
				free := numberField(mem.Stdout, "free")
				if total > 0 && free >= 0 && free <= total {
					view.MemPercent = (total - free) / total * 100
				}
			}
		}
	}
	view.Reachable = reachable
	health := store.HostHealth{HostID: host.ID, Reachable: reachable, RunningVMs: view.RunningVMs, CPUPercent: view.CPUPercent, MemPercent: view.MemPercent}
	if reachable {
		health.Failures = 0
	} else {
		health.Failures = previous.Failures + 1
		if previous.Failures == 0 && w.hooks != nil {
			w.hooks.Send(webhook.Event{Action: "host.unreachable", HostID: host.ID, Detail: map[string]any{"failures": health.Failures}})
		}
	}
	if err := w.store.RecordHostHealth(ctx, health); err != nil {
		w.log.Warn("record host health", "host_id", host.ID, "error", err)
	}
	return view
}

func findView(views []cluster.Host, id string) cluster.Host {
	for _, view := range views {
		if view.ID == id {
			return view
		}
	}
	return cluster.Host{}
}

func countLines(value string) int {
	count := 0
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func field(output, name string) string {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func numberField(output, name string) float64 {
	fields := strings.Fields(field(output, name))
	if len(fields) == 0 {
		return 0
	}
	raw := fields[0]
	number, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return number
}

func percent(value string) float64 {
	number, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(value), "%"), 64)
	if err != nil || number < 0 || number > 100 {
		return 0
	}
	return number
}
