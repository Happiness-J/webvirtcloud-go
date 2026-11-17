package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"webvirtcloud-go/internal/store"
)

type platformExport struct {
	Version   string       `json:"version"`
	CreatedAt time.Time    `json:"created_at"`
	Hosts     []hostExport `json:"hosts"`
	Checksum  string       `json:"checksum"`
}

type hostExport struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

func (s *Server) platformExport(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.db.ListHosts(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	body := platformExport{Version: "platform-1", CreatedAt: time.Now().UTC(), Hosts: make([]hostExport, 0, len(hosts))}
	for _, host := range hosts {
		body.Hosts = append(body.Hosts, hostExport{Name: host.Name, Description: host.Description, Enabled: host.Enabled})
	}
	raw, err := json.Marshal(struct {
		Version   string       `json:"version"`
		CreatedAt time.Time    `json:"created_at"`
		Hosts     []hostExport `json:"hosts"`
	}{body.Version, body.CreatedAt, body.Hosts})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	sum := sha256.Sum256(raw)
	body.Checksum = hex.EncodeToString(sum[:])
	out, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "platform.export", RemoteIP: s.clientIP(r), Detail: map[string]any{"hosts": len(body.Hosts), "checksum": body.Checksum}})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=webvirtcloud-platform.json")
	_, _ = w.Write(out)
}

func (s *Server) platformRestore(w http.ResponseWriter, r *http.Request) {
	if r.Form.Get("confirm") != "yes" {
		s.redirectError(w, r, "/platform", "恢复平台配置需要确认")
		return
	}
	file, _, err := r.FormFile("bundle")
	if err != nil {
		s.redirectError(w, r, "/platform", "请选择导出的平台配置")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		s.redirectError(w, r, "/platform", "无法读取导出文件")
		return
	}
	var body platformExport
	if err := json.Unmarshal(raw, &body); err != nil || body.Version != "platform-1" {
		s.redirectError(w, r, "/platform", "导出文件格式无效")
		return
	}
	canonical, _ := json.Marshal(struct {
		Version   string       `json:"version"`
		CreatedAt time.Time    `json:"created_at"`
		Hosts     []hostExport `json:"hosts"`
	}{body.Version, body.CreatedAt, body.Hosts})
	sum := sha256.Sum256(canonical)
	if !strings.EqualFold(body.Checksum, hex.EncodeToString(sum[:])) {
		s.redirectError(w, r, "/platform", "完整性校验未通过")
		return
	}
	sess, _ := sessionFrom(r.Context())
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "platform.restore_checked", RemoteIP: s.clientIP(r), Detail: map[string]any{"hosts": len(body.Hosts), "checksum": body.Checksum}})
	http.Redirect(w, r, flashURL("/platform", "导出包校验通过。数据库恢复请使用运维文档中的数据库备份流程，页面不直接写回数据库。", "ok"), http.StatusSeeOther)
}

func (s *Server) platformPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "platform", pageData{Title: "平台备份", Nav: "users"})
}
