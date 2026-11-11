package libvirt

type Domain struct {
	HostID        string
	HostName      string
	Name          string
	ID            string
	State         string
	Autostart     string
	VCPUs         int
	MaxMemoryKiB  uint64
	UsedMemoryKiB uint64
}

type Snapshot struct {
	Name      string
	CreatedAt string
	State     string
}

type Pool struct {
	Name       string
	State      string
	Autostart  string
	Persistent string
	Capacity   string
	Allocation string
	Available  string
}

type Volume struct {
	Pool       string
	Name       string
	Path       string
	Type       string
	Capacity   string
	Allocation string
}

type BlockDevice struct {
	Type   string
	Device string
	Target string
	Source string
}

type Network struct {
	Name       string
	State      string
	Autostart  string
	Persistent string
}

type Console struct {
	Type     string
	Address  string
	Port     int
	Password string
}

type Backup struct {
	Name      string
	CreatedAt string
	State     string
}

type DomainDetail struct {
	Domain      Domain
	Info        map[string]string
	BlockDevs   string
	BlockList   []BlockDevice
	Ifaces      string
	Snapshots   []Snapshot
	XML         string
	BlockErrors string
	MemoryStats string
	Events      string
	GuestAgent  string
	Backups     []Backup
	// PowerRequest reports a pending guest ACPI shutdown or reboot request.
	// An empty value means libvirt currently reports no pending request.
	PowerRequest string
	// Warnings contains user-safe notices for optional detail queries that
	// failed. The VM page can still render its core information without hiding
	// partial libvirt outages.
	Warnings []string
}

// HostResourceStats contains the inexpensive, read-only libvirt measurements
// used by the operations workbench. Values intentionally remain as the
// human-readable strings reported by virsh so operators can correlate the UI
// with command-line diagnostics. Missing keys indicate a partial query
// failure; Warnings explains which measurement was unavailable.
type HostResourceStats struct {
	NodeInfo    map[string]string
	CPUStats    map[string]string
	MemoryStats map[string]string
	Warnings    []string
}

// DomainResourceStats is parsed from `virsh domstats`. Sizes are KiB and CPU
// time is nanoseconds, matching libvirt's documented output units.
type DomainResourceStats struct {
	Name           string
	State          string
	BalloonCurrent uint64
	BalloonMaximum uint64
	CPUTimeNanos   uint64
}
