package libvirt

type Domain struct {
	HostID   string
	HostName string
	Name     string
	ID       string
	State    string
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

type DomainDetail struct {
	Domain    Domain
	Info      map[string]string
	BlockDevs string
	Ifaces    string
	Snapshots []Snapshot
	XML       string
	// Warnings contains user-safe notices for optional detail queries that
	// failed. The VM page can still render its core information without hiding
	// partial libvirt outages.
	Warnings []string
}
