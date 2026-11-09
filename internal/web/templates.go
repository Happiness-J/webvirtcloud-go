package web

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data pageData) {
	s.renderStatus(w, r, http.StatusOK, name, data)
}

func (s *Server) renderStatus(w http.ResponseWriter, r *http.Request, status int, name string, data pageData) {
	t, ok := s.tmpl[name]
	if !ok {
		s.log.Error("missing template", "name", name)
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	if sess, ok := sessionFrom(r.Context()); ok {
		data.User = &sess.User
		if data.CSRF == "" {
			data.CSRF = sess.CSRFToken
		}
	}
	if data.Flash == "" {
		data.Flash = truncate(r.URL.Query().Get("msg"), 500)
		data.FlashType = "ok"
		if r.URL.Query().Get("type") == "error" {
			data.FlashType = "error"
		}
	}
	var rendered bytes.Buffer
	if err := t.ExecuteTemplate(&rendered, "base", data); err != nil {
		s.log.Error("render template", "name", name, "error", err)
		http.Error(w, "template rendering failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = rendered.WriteTo(w)
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	s.renderStatus(w, r, status, "error", pageData{Title: title, Data: map[string]any{"Status": status, "Message": message}})
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "path", r.URL.Path, "error", err)
	s.renderError(w, r, http.StatusInternalServerError, "服务异常", "操作未能完成，请查看服务日志。")
}

func (s *Server) logRequestError(r *http.Request, operation string, err error) {
	s.log.Warn(operation, "request_id", requestIDFrom(r.Context()), "path", r.URL.Path, "error", err)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "页面不存在", "请求的地址不存在。")
}

func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/novnc/app/") ||
			strings.HasPrefix(r.URL.Path, "/novnc/vendor/browser-es-module-loader/") ||
			r.URL.Path == "/novnc/vendor/promise.js" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		files.ServeHTTP(w, r)
	})
}

func flashURL(path, message, kind string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%smsg=%s&type=%s", path, sep, url.QueryEscape(message), url.QueryEscape(kind))
}

func truncate(s string, max int) string {
	return truncateRunes(s, max)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
