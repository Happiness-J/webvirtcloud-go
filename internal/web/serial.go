package web

import (
	"context"

	"github.com/gorilla/websocket"

	"net/http"
	"net/url"
	"time"
	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/store"

	consolepkg "webvirtcloud-go/internal/console"
	"webvirtcloud-go/internal/libvirt"
)

func (s *Server) serialPage(w http.ResponseWriter, r *http.Request) {
	host, err := s.db.GetHost(r.Context(), r.PathValue("host"))
	if err != nil {
		s.hostLookupError(w, r, err)
		return
	}
	vmName := r.PathValue("vm")
	if err := validateSerialName(vmName); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "名称无效", "虚拟机名称无效")
		return
	}
	allowed, accessErr := s.canUseConsole(r, host.ID, vmName)
	if accessErr != nil || !allowed {
		s.renderError(w, r, http.StatusForbidden, "无权打开串口", "当前账号没有该虚拟机的授权。")
		return
	}
	sess, _ := sessionFrom(r.Context())
	token, err := s.tokens.Issue(host.ID, vmName, sess.User.ID, consolepkg.ScopeWebSocket, time.Now())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	wsPath := "/serial/" + url.PathEscape(host.ID) + "/" + url.PathEscape(vmName) + "/ws?token=" + url.QueryEscape(token)
	s.render(w, r, "serial", pageData{Title: "串口终端 · " + vmName, Nav: "vms", Data: map[string]any{"HostID": host.ID, "HostName": host.Name, "VM": vmName, "WSURL": wsPath}})
}

func (s *Server) serialWS(w http.ResponseWriter, r *http.Request) {
	claims, host, token, ok := s.validateConsoleRequest(w, r, consolepkg.ScopeWebSocket)
	if !ok {
		return
	}
	if !s.claimConsoleToken(w, r, token, claims) {
		return
	}
	uri, err := s.hostURI(r.Context(), host)
	if err != nil {
		http.Error(w, "serial backend unavailable", http.StatusBadGateway)
		return
	}
	target, err := command.ResolveTarget(uri)
	if err != nil {
		http.Error(w, "serial backend unavailable", http.StatusBadGateway)
		return
	}
	request := target.Request("virsh", []string{"console", claims.VM, "--safe"}, "", false, 30*time.Minute)
	if err := s.runner.Validate(request); err != nil {
		http.Error(w, "serial backend unavailable", http.StatusBadGateway)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	output := make(chan command.Result, 1)
	go func() {
		result, _ := s.runner.Run(ctx, request)
		output <- result
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case result := <-output:
			if result.Output() != "" {
				_ = conn.WriteMessage(websocket.TextMessage, []byte(result.Output()))
			}
			return
		default:
			_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
			_, message, readErr := conn.ReadMessage()
			if readErr == nil && len(message) > 0 {
				request.Stdin += string(message)
			}
		}
	}
}

func (s *Server) hostURI(ctx context.Context, host store.Host) (string, error) {
	return libvirt.DecryptURI(s.cipher, host)
}

func validateSerialName(name string) error { return libvirt.ValidateName(name) }
