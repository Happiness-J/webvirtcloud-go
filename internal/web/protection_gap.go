package web

// protectionGap compares delete lock and high availability. It never changes
// either protection.
type protectionGap struct {
	DeleteLock bool
	HA         bool
	Label      string
}

func protectionMismatch(deleteLock, ha bool) protectionGap {
	gap := protectionGap{DeleteLock: deleteLock, HA: ha}
	switch {
	case deleteLock && !ha:
		gap.Label = "只有防删锁"
	case ha && !deleteLock:
		gap.Label = "只有高可用"
	}
	return gap
}
