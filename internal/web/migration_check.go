package web

import (
	"net/http"
	"strings"

	"webvirtcloud-go/internal/libvirt"
)

type migrationCheck struct {
	OK      bool
	Reasons []string
}

func (s *Server) migrationPrecheck(r *http.Request, sourceID string, devices []libvirt.BlockDevice) migrationCheck {
	healthRows, _ := s.db.ListHostHealth(r.Context())
	sourceReachable := false
	targetReachable := false
	for _, item := range healthRows {
		if item.HostID == sourceID {
			sourceReachable = item.Reachable
		} else if item.Reachable {
			targetReachable = true
		}
	}
	sourceActive, _ := s.db.HostHasActiveJobs(r.Context(), sourceID)
	targetActive := false
	return checkMigration(sourceReachable, targetReachable, sourceActive, targetActive, devices)
}

func checkMigration(sourceReachable, targetReachable bool, sourceActive, targetActive bool, devices []libvirt.BlockDevice) migrationCheck {
	var reasons []string
	if !sourceReachable {
		reasons = append(reasons, "源主机最近一次探活不可达")
	}
	if !targetReachable {
		reasons = append(reasons, "没有可达的其他目标主机")
	}
	if sourceActive || targetActive {
		reasons = append(reasons, "源主机或目标主机已有活动写任务，迁移前需要避开锁冲突")
	}
	shared := false
	for _, device := range devices {
		if device.Device == "disk" && sharedDiskSource(device.Source) {
			shared = true
		}
	}
	if !shared {
		reasons = append(reasons, "没有发现共享存储磁盘，本地 qcow2 或 ISO 不能直接在线迁移")
	}
	return migrationCheck{OK: len(reasons) == 0, Reasons: reasons}
}

func sharedDiskSource(source string) bool {
	source = strings.TrimSpace(source)
	if source == "" || source == "-" {
		return false
	}
	lower := strings.ToLower(source)
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "/dev/") {
		return true
	}
	return strings.Contains(source, ":/") && !strings.HasPrefix(source, "/var/lib/libvirt/images/")
}
