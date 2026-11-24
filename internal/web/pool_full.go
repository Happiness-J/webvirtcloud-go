package web

import "webvirtcloud-go/internal/libvirt"

// nearlyFullPool is a storage pool with less than one tenth free.
// The list is read-only and never migrates or grows a disk.
type nearlyFullPool struct {
	HostID   string
	HostName string
	Name     string
	Free     string
}

func nearlyFullPools(hostID, hostName string, pools []libvirt.Pool) []nearlyFullPool {
	var out []nearlyFullPool
	for _, pool := range pools {
		capacity, capOK := parseReadableBytes(pool.Capacity)
		free, freeOK := parseReadableBytes(pool.Available)
		if !capOK || !freeOK || capacity == 0 {
			continue
		}
		if free*10 < capacity {
			out = append(out, nearlyFullPool{HostID: hostID, HostName: hostName, Name: pool.Name, Free: pool.Available})
		}
	}
	return out
}
