package web

import "webvirtcloud-go/internal/libvirt"

// snapshotSummary counts the existing snapshot tree. It never deletes or
// restores a snapshot.
type snapshotSummary struct {
	Count   int
	Current string
	Depth   int
	Parents []snapshotParent
}

type snapshotParent struct {
	Name     string
	Children int
}

func summarizeSnapshots(items []libvirt.Snapshot) snapshotSummary {
	summary := snapshotSummary{}
	children := map[string]int{}
	for _, item := range items {
		summary.Count++
		if item.Depth > summary.Depth {
			summary.Depth = item.Depth
		}
		if item.Current && summary.Current == "" {
			summary.Current = item.Name
		}
		if item.Parent != "" {
			children[item.Parent]++
		}
	}
	for _, item := range items {
		if children[item.Name] > 0 {
			summary.Parents = append(summary.Parents, snapshotParent{Name: item.Name, Children: children[item.Name]})
		}
	}
	return summary
}
