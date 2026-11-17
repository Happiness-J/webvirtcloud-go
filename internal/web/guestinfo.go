package web

import "net/http"

func guestText(s *Server, r *http.Request, hostID, vm, kind string) string {
	var text string
	var err error
	if kind == "os" {
		text, err = s.libvirt.GuestOSInfo(r.Context(), hostID, vm)
	} else {
		text, err = s.libvirt.GuestFilesystems(r.Context(), hostID, vm)
	}
	if err != nil || text == "" {
		return "guest agent 未返回信息。请安装并启动 QEMU Guest Agent，或挂载工具 ISO。"
	}
	if len(text) > 4000 {
		return text[:4000]
	}
	return text
}
