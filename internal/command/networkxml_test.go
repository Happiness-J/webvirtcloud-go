package command

import "testing"

func TestNetworkXML(t *testing.T) {
	xmlText, err := NetworkXML(NetworkDefinition{Name: "lab", Mode: "nat", Address: "192.0.2.0", Prefix: 24})
	if err != nil || ValidateNetworkXML(xmlText) != nil {
		t.Fatalf("nat xml=%q err=%v", xmlText, err)
	}
	if xmlText != "<network><name>lab</name><forward mode='nat'/><ip address='192.0.2.0' prefix='24'><dhcp><range start='192.0.2.10' end='192.0.2.254'/></dhcp></ip></network>" {
		t.Fatalf("nat xml=%s", xmlText)
	}
	xmlText, err = NetworkXML(NetworkDefinition{Name: "lab", Mode: "bridge", Bridge: "br0"})
	if err != nil || ValidateNetworkXML(xmlText) != nil || xmlText != "<network><name>lab</name><forward mode='bridge'/><bridge name='br0'/></network>" {
		t.Fatalf("bridge xml=%q err=%v", xmlText, err)
	}
	if _, err = NetworkXML(NetworkDefinition{Name: "lab", Mode: "nat", Address: "192.0.2.5", Prefix: 24}); err == nil {
		t.Fatal("host address accepted as network address")
	}
	if _, err = NetworkXML(NetworkDefinition{Name: "lab", Mode: "bridge", Bridge: "../br0"}); err == nil {
		t.Fatal("invalid bridge accepted")
	}
	if err = ValidateNetworkXML("<network><name>lab</name><forward mode='nat'/></network>"); err != nil {
		t.Fatal(err)
	}
}
