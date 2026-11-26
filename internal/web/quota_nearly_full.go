package web

import "webvirtcloud-go/internal/store"

// nearlyFullQuota is one account whose reserved usage has reached at least
// eighty percent of a quota. The list only points at the account; it never
// changes the quota.
type nearlyFullQuota struct {
	UserID   string
	Username string
	Items    []string
}

func nearlyFullQuotas(users []store.User, quotaOf func(userID string) (store.UserQuota, error), usageOf func(userID string) (store.Usage, error)) []nearlyFullQuota {
	var out []nearlyFullQuota
	for _, user := range users {
		quota, err := quotaOf(user.ID)
		if err != nil {
			continue
		}
		usage, err := usageOf(user.ID)
		if err != nil {
			continue
		}
		items := quotaNearlyFull(usage, quota)
		if len(items) == 0 {
			continue
		}
		out = append(out, nearlyFullQuota{UserID: user.ID, Username: user.Username, Items: items})
	}
	return out
}

func quotaNearlyFull(usage store.Usage, quota store.UserQuota) []string {
	checks := []struct {
		label string
		used  int
		limit int
	}{
		{"虚拟机", usage.VMs, quota.MaxVMs},
		{"vCPU", usage.VCPUs, quota.MaxVCPUs},
		{"内存", usage.MemoryGB, quota.MaxMemoryGB},
		{"磁盘", usage.DiskGB, quota.MaxDiskGB},
	}
	var items []string
	for _, check := range checks {
		if check.limit > 0 && check.used*5 >= check.limit*4 {
			items = append(items, check.label)
		}
	}
	return items
}
