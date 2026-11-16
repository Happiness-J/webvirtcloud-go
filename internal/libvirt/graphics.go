package libvirt

import "encoding/xml"

type GraphicsDisplay struct {
	Type     string
	Address  string
	Port     int
	Password bool
}

func ParseGraphics(xmlText string) []GraphicsDisplay {
	var doc domainXML
	if err := xml.Unmarshal([]byte(xmlText), &doc); err != nil {
		return nil
	}
	out := make([]GraphicsDisplay, 0, len(doc.Devices.Graphics))
	for _, item := range doc.Devices.Graphics {
		if item.Type != "vnc" && item.Type != "spice" {
			continue
		}
		address := item.Listen
		for _, listener := range item.Listeners {
			if listener.Address != "" {
				address = listener.Address
				break
			}
		}
		out = append(out, GraphicsDisplay{Type: item.Type, Address: address, Port: item.Port, Password: item.Passwd != ""})
	}
	return out
}
