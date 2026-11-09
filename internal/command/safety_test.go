package command

import "testing"

func TestValidateAsyncModeRejectsDetachedAndInteractiveModes(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args []string
	}{
		{name: "storage daemon", tool: "qemu-storage-daemon", args: []string{"--daemonize", "--blockdev", "driver=qcow2"}},
		{name: "storage daemon abbreviation", tool: "qemu-storage-daemon", args: []string{"--daemoniz"}},
		{name: "pr helper", tool: "qemu-pr-helper", args: []string{"--daemon", "--socket", "/run/pr.sock"}},
		{name: "pr helper short", tool: "qemu-pr-helper", args: []string{"-d", "--socket", "/run/pr.sock"}},
		{name: "pr helper short cluster", tool: "qemu-pr-helper", args: []string{"-qvd", "--socket", "/run/pr.sock"}},
		{name: "vmsr helper", tool: "qemu-vmsr-helper", args: []string{"--daemon", "--socket", "/run/msr.sock"}},
		{name: "vmsr helper short", tool: "qemu-vmsr-helper", args: []string{"-d", "--socket", "/run/msr.sock"}},
		{name: "nbd fork", tool: "qemu-nbd", args: []string{"--fork", "--port", "10809", "/tmp/disk.qcow2"}},
		{name: "nbd fork abbreviation", tool: "qemu-nbd", args: []string{"--for", "--port", "10809", "/tmp/disk.qcow2"}},
		{name: "guestfish listener", tool: "guestfish", args: []string{"--listen"}},
		{name: "guestfish listener abbreviation", tool: "guestfish", args: []string{"--lis"}},
		{name: "guestmount default fork", tool: "guestmount", args: []string{"-a", "/tmp/disk.img", "-m", "/dev/sda1", "/mnt/guest"}},
		{name: "guestmount no-fork still blocks", tool: "guestmount", args: []string{"--no-fork", "-a", "/tmp/disk.img", "/mnt/guest"}},
		{name: "virt rescue requires tty", tool: "virt-rescue", args: []string{"-d", "vm01"}},
		{name: "virt p2v is graphical", tool: "virt-p2v", args: []string{"--iso"}},
		{name: "virt login shell requires tty", tool: "virt-login-shell", args: []string{"-c", "true"}},
		{name: "virt top interactive", tool: "virt-top", args: []string{"-n", "1"}},
		{name: "virt top batch without iterations", tool: "virt-top", args: []string{"-b"}},
		{name: "virt top script without iterations", tool: "virt-top", args: []string{"--script"}},
		{name: "virt top zero iterations", tool: "virt-top", args: []string{"-b", "-n", "0"}},
		{name: "virt top negative iterations", tool: "virt-top", args: []string{"-b", "--iterations=-1"}},
		{name: "builder repository", tool: "virt-builder-repository", args: []string{"--interactive", "/srv/repo"}},
		{name: "builder repository abbreviation", tool: "virt-builder-repository", args: []string{"--inter", "/srv/repo"}},
		{name: "builder repository short", tool: "virt-builder-repository", args: []string{"-i", "/srv/repo"}},
		{name: "builder repository short cluster", tool: "virt-builder-repository", args: []string{"-vi", "/srv/repo"}},
		{name: "interactive edit", tool: "virt-edit", args: []string{"-d", "vm01", "/etc/hosts"}},
		{name: "virsh start console", tool: "virsh", args: []string{"start", "vm01", "--console"}},
		{name: "virsh start console abbreviation", tool: "virsh", args: []string{"star", "vm01", "--console"}},
		{name: "virsh create console behind option", tool: "virsh", args: []string{"--quiet", "create", "vm.xml", "--console"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateAsyncMode(tc.tool, tc.args); err == nil {
				t.Fatalf("ValidateAsyncMode(%s, %v) accepted unsafe mode", tc.tool, tc.args)
			}
		})
	}
}

func TestValidateAsyncModeAllowsForegroundAndNonInteractiveModes(t *testing.T) {
	cases := []struct {
		tool string
		args []string
	}{
		{tool: "qemu-storage-daemon", args: []string{"--blockdev", "driver=qcow2"}},
		{tool: "qemu-pr-helper", args: []string{"--socket", "/run/pr.sock"}},
		{tool: "qemu-nbd", args: []string{"--port", "10809", "/tmp/disk.qcow2"}},
		{tool: "qemu-nbd", args: []string{"--format", "qcow2", "--port", "10809", "/tmp/disk.qcow2"}},
		{tool: "qemu-pr-helper", args: []string{"-f", "/run/pr.pid", "--socket", "/run/pr.sock"}},
		{tool: "guestfish", args: []string{"-a", "/tmp/disk.img", "cat", "/etc/hosts"}},
		{tool: "virt-top", args: []string{"-b", "-n", "1"}},
		{tool: "virt-top", args: []string{"-bn2"}},
		{tool: "virt-top", args: []string{"--script", "-n1"}},
		{tool: "virt-top", args: []string{"-b", "--iterations", "2"}},
		{tool: "virt-top", args: []string{"--batch", "--iterations=3"}},
		{tool: "virt-edit", args: []string{"--expr", "s/old/new/", "-d", "vm01", "/etc/hosts"}},
		{tool: "virt-edit", args: []string{"--exp", "s/old/new/", "-d", "vm01", "/etc/hosts"}},
		{tool: "virt-edit", args: []string{"-e", "s/old/new/", "-d", "vm01", "/etc/hosts"}},
		{tool: "virt-builder-repository", args: []string{"/srv/repo"}},
		{tool: "virt-builder-repository", args: []string{"-v", "/srv/repo"}},
		{tool: "qemu-storage-daemon", args: []string{"--help"}},
		{tool: "guestmount", args: []string{"--help"}},
		{tool: "guestmount", args: []string{"--version"}},
		{tool: "virt-rescue", args: []string{"--help"}},
		{tool: "virt-p2v", args: []string{"--version"}},
		{tool: "virt-login-shell", args: []string{"--help"}},
		{tool: "virsh", args: []string{"start", "vm01"}},
		{tool: "virsh", args: []string{"create", "vm.xml", "--paused"}},
	}
	for _, tc := range cases {
		if err := ValidateAsyncMode(tc.tool, tc.args); err != nil {
			t.Errorf("ValidateAsyncMode(%s, %v) = %v", tc.tool, tc.args, err)
		}
	}
}
