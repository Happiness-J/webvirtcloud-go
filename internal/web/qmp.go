package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"webvirtcloud-go/internal/command"
)

func qmpQuery(kind string) (string, string, error) {
	switch kind {
	case "status":
		return "query-status", "QMP 状态", nil
	case "block":
		return "query-block", "QMP 块设备", nil
	default:
		return "", "", fmt.Errorf("只允许查询 QMP 状态或块设备")
	}
}

func qmpArgs(vm, kind string) ([]string, error) {
	commandName, _, err := qmpQuery(kind)
	if err != nil || command.ValidateActionOperandPublic(vm) != nil {
		return nil, fmt.Errorf("QMP 查询参数无效")
	}
	payload, err := json.Marshal(map[string]string{"execute": commandName})
	if err != nil {
		return nil, err
	}
	return []string{"qemu-monitor-command", vm, "--pretty", string(payload)}, nil
}

func qmpSummary(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "QMP 未返回结果。请确认虚拟机正在运行。"
	}
	if len(output) > 4000 {
		return output[:4000] + "\n…"
	}
	return output
}

func qmpPageQuery(s *Server, r *http.Request, hostID, vm, state, kind string) string {
	sess, _ := sessionFrom(r.Context())
	if sess.User.Role != "admin" && sess.User.Role != "operator" {
		return "当前角色不能查看 QMP。"
	}
	if state != "running" {
		return "虚拟机未运行，QMP 不可用。"
	}
	args, err := qmpArgs(vm, kind)
	if err != nil {
		return err.Error()
	}
	result, err := s.libvirt.RunQMPQuery(r.Context(), hostID, args, 10*time.Second)
	if err != nil && strings.TrimSpace(result.Output()) == "" {
		return "无法读取 QMP 信息，请检查主机连接。"
	}
	return qmpSummary(result.Output())
}
