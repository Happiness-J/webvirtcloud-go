package command

import "testing"

func TestValidateMDEVDefine(t *testing.T) {
	ok := "<device><name>mdev_0123abcd</name><parent>pci_0000_00_02_0</parent><capability type='mdev'><type id='nvidia-300'/></capability></device>"
	if err := ValidateMDEVDefine(ok); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMDEVDefine(ok + "<script>"); err == nil {
		t.Fatal("extra xml accepted")
	}
}
