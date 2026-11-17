package libvirt

import "testing"

func TestParseSRIOVFunction(t *testing.T) {
	xmlText := `<device><name>pci_0000_03_10_1</name><capability type="pci"><address domain="0x0000" bus="0x03" slot="0x10" function="0x1"/><virt_functions/></capability></device>`
	device, ok := parseNodeDevice("pci_0000_03_10_1", "pci", xmlText)
	if !ok || !device.SRIOV || device.Kind != "pci" {
		t.Fatalf("%+v %v", device, ok)
	}
}
