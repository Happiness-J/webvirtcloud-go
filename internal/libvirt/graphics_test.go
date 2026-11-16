package libvirt

import "testing"

func TestParseGraphics(t *testing.T) {
	got := ParseGraphics(`<domain><devices><graphics type="vnc" port="5901" listen="127.0.0.1" passwd="secret"/><graphics type="spice" port="5902" listen="0.0.0.0"/></devices></domain>`)
	if len(got) != 2 || got[0].Type != "vnc" || !got[0].Password || got[1].Type != "spice" || got[1].Port != 5902 || got[1].Password {
		t.Fatalf("graphics=%#v", got)
	}
}
