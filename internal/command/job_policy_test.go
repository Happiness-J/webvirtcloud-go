package command

import "testing"

func TestJobMinimumRole(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		tool    string
		args    []string
		want    string
		wantErr bool
	}{
		{name: "command admin", kind: "command", tool: "virsh", args: []string{"list"}, want: RoleAdmin},
		{name: "command zero argument", kind: "command", tool: "virt-what", want: RoleAdmin},
		{name: "command qemu", kind: "command", tool: "qemu-img", args: []string{"info", "a"}, want: RoleAdmin},
		{name: "host test", kind: "host-test", tool: "virsh", args: []string{"version"}, want: RoleOperator},
		{name: "host health", kind: "host-test", tool: "virt-host-validate", args: []string{"--quiet", "qemu"}, want: RoleOperator},
		{name: "vm action", kind: "vm-action", tool: "virsh", args: []string{"destroy", "vm"}, want: RoleOperator},
		{name: "vm undefine", kind: "vm-action", tool: "virsh", args: []string{"undefine", "vm"}, want: RoleAdmin},
		{name: "vm snapshot", kind: "vm-action", tool: "virsh", args: []string{"snapshot-create-as", "vm", "snap"}, want: RoleOperator},
		{name: "vm autostart disable", kind: "vm-action", tool: "virsh", args: []string{"autostart", "--disable", "vm"}, want: RoleOperator},
		{name: "storage grow", kind: "storage-action", tool: "virsh", args: []string{"vol-resize", "--pool", "images", "--delta", "disk.qcow2", "10G"}, want: RoleOperator},
		{name: "storage shrink", kind: "storage-action", tool: "virsh", args: []string{"vol-resize", "--pool", "images", "--delta", "--shrink", "disk.qcow2", "10G"}, want: RoleOperator},
		{name: "network start", kind: "network-action", tool: "virsh", args: []string{"net-start", "default"}, want: RoleOperator},
		{name: "online disk grow", kind: "disk-action", tool: "virsh", args: []string{"blockresize", "vm", "vda", "50000000000"}, want: RoleOperator},
		{name: "offline disk shrink", kind: "disk-action", tool: "qemu-img", args: []string{"resize", "--shrink", "/var/lib/libvirt/images/vm.qcow2", "-10G"}, want: RoleOperator},
		{name: "vm cpu persistent", kind: "vm-resource", tool: "virsh", args: []string{"setvcpus", "vm", "8", "--config"}, want: RoleOperator},
		{name: "vm cpu live persistent", kind: "vm-resource", tool: "virsh", args: []string{"setvcpus", "vm", "8", "--live", "--config"}, want: RoleOperator},
		{name: "vm cpu maximum", kind: "vm-resource", tool: "virsh", args: []string{"setvcpus", "vm", "16", "--maximum", "--config"}, want: RoleOperator},
		{name: "vm memory live persistent", kind: "vm-resource", tool: "virsh", args: []string{"setmem", "vm", "8388608", "--live", "--config"}, want: RoleOperator},
		{name: "missing args", kind: "host-test", tool: "virsh", wantErr: true},
		{name: "wrong command tool", kind: "command", tool: "unknown", args: []string{"list"}, wantErr: true},
		{name: "wrong vm tool", kind: "vm-action", tool: "qemu-img", args: []string{"info", "a"}, wantErr: true},
		{name: "vm rejects sensitive command", kind: "vm-action", tool: "virsh", args: []string{"secret-get-value", "secret"}, wantErr: true},
		{name: "vm rejects option injection", kind: "vm-action", tool: "virsh", args: []string{"start", "--connect"}, wantErr: true},
		{name: "vm rejects extra arguments", kind: "vm-action", tool: "virsh", args: []string{"destroy", "vm", "--graceful"}, wantErr: true},
		{name: "host test rejects other commands", kind: "host-test", tool: "virsh", args: []string{"nodeinfo"}, wantErr: true},
		{name: "host health rejects unsupported hypervisor", kind: "host-test", tool: "virt-host-validate", args: []string{"qemu", "--help"}, wantErr: true},
		{name: "unknown kind", kind: "unknown", tool: "virsh", args: []string{"list"}, wantErr: true},
		{name: "storage rejects unknown action", kind: "storage-action", tool: "virsh", args: []string{"vol-wipe", "disk"}, wantErr: true},
		{name: "storage rejects invalid amount", kind: "storage-action", tool: "virsh", args: []string{"vol-resize", "--pool", "images", "--delta", "disk.qcow2", "10X"}, wantErr: true},
		{name: "network rejects option injection", kind: "network-action", tool: "virsh", args: []string{"net-start", "--all"}, wantErr: true},
		{name: "disk rejects sign mismatch", kind: "disk-action", tool: "qemu-img", args: []string{"resize", "--shrink", "/var/lib/libvirt/images/vm.qcow2", "+10G"}, wantErr: true},
		{name: "disk rejects relative path", kind: "disk-action", tool: "qemu-img", args: []string{"resize", "vm.qcow2", "+10G"}, wantErr: true},
		{name: "vm resource rejects option injection", kind: "vm-resource", tool: "virsh", args: []string{"setvcpus", "--connect", "4", "--config"}, wantErr: true},
		{name: "vm resource rejects unbounded cpu", kind: "vm-resource", tool: "virsh", args: []string{"setvcpus", "vm", "2048", "--config"}, wantErr: true},
		{name: "vm resource rejects unknown flags", kind: "vm-resource", tool: "virsh", args: []string{"setmem", "vm", "8388608", "--current"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := JobMinimumRole(tc.kind, tc.tool, tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("JobMinimumRole() = %q, nil; want error", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("JobMinimumRole() = %q, %v; want %q, nil", got, err, tc.want)
			}
		})
	}
}

func TestRoleAllows(t *testing.T) {
	tests := []struct {
		role, minimum string
		want          bool
	}{
		{RoleAdmin, RoleAdmin, true},
		{RoleOperator, RoleAdmin, false},
		{RoleAdmin, RoleOperator, true},
		{RoleOperator, RoleOperator, true},
		{"viewer", RoleOperator, false},
		{RoleAdmin, "unknown", false},
	}
	for _, tc := range tests {
		if got := RoleAllows(tc.role, tc.minimum); got != tc.want {
			t.Errorf("RoleAllows(%q, %q) = %v, want %v", tc.role, tc.minimum, got, tc.want)
		}
	}
}
