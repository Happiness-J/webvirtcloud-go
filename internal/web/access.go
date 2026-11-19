package web

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"webvirtcloud-go/internal/auth"
	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/store"
)

func (s *Server) acceptTOTP(user store.User, code string) bool {
	if !user.TOTPEnabled() {
		return true
	}
	return validTOTP(user.TOTPSecret, code, time.Now())
}

func (s *Server) accountSecurityPage(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	user, err := s.db.GetUserByID(r.Context(), sess.User.ID)
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	s.renderAccountSecurity(w, r, user, sess.TokenHash, "", "")
}

func (s *Server) renderAccountSecurity(w http.ResponseWriter, r *http.Request, user store.User, currentTokenHash, flash, createdToken string) {
	sessions, err := s.db.ListSessions(r.Context(), user.ID, currentTokenHash)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	tokens, err := s.db.ListAPITokens(r.Context(), user.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	pending, _ := pendingTOTP(r)
	s.render(w, r, "account_security", pageData{Title: "账号安全", Nav: "account", Flash: flash, FlashType: "ok", Data: map[string]any{
		"TOTPEnabled":  user.TOTPEnabled(),
		"Pending":      pending,
		"Sessions":     sessions,
		"Tokens":       tokens,
		"Roles":        tokenRoles(user.Role),
		"CreatedToken": createdToken,
	}})
}

func tokenRoles(role string) []string {
	switch role {
	case "admin":
		return []string{"viewer", "operator", "admin"}
	case "operator":
		return []string{"viewer", "operator"}
	case "desktop":
		return []string{"desktop"}
	default:
		return []string{"viewer"}
	}
}

func (s *Server) accountTOTPStart(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	user, err := s.db.GetUserByID(r.Context(), sess.User.ID)
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	if user.TOTPEnabled() {
		s.redirectError(w, r, "/account/security", "验证器已经启用")
		return
	}
	if !auth.VerifyPasswordContext(r.Context(), r.Form.Get("password"), user.PasswordHash) {
		s.audit.Record(r.Context(), store.Audit{ActorID: user.ID, ActorName: user.Username, Action: "auth.totp_failed", RemoteIP: s.clientIP(r)})
		s.redirectError(w, r, "/account/security", "当前密码不正确")
		return
	}
	secret, err := newTOTPSecret()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.setPendingTOTP(w, secret)
	http.Redirect(w, r, flashURL("/account/security", "验证器密钥已生成，请用验证器扫码后输入 6 位验证码确认", "ok"), http.StatusSeeOther)
}

func (s *Server) accountTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	secret, ok := pendingTOTP(r)
	if !ok || !validTOTP(secret, r.Form.Get("code"), time.Now()) {
		s.redirectError(w, r, "/account/security", "验证码不正确")
		return
	}
	if err := s.db.SetTOTPSecret(r.Context(), sess.User.ID, secret); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.clearPendingTOTP(w)
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "auth.totp_enabled", RemoteIP: s.clientIP(r)})
	http.Redirect(w, r, flashURL("/account/security", "双因素认证已启用，下次登录需要验证码", "ok"), http.StatusSeeOther)
}

func (s *Server) accountTOTPDisable(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	user, err := s.db.GetUserByID(r.Context(), sess.User.ID)
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	if !user.TOTPEnabled() || !validTOTP(user.TOTPSecret, r.Form.Get("code"), time.Now()) {
		s.audit.Record(r.Context(), store.Audit{ActorID: user.ID, ActorName: user.Username, Action: "auth.totp_failed", RemoteIP: s.clientIP(r)})
		s.redirectError(w, r, "/account/security", "验证码不正确")
		return
	}
	if err := s.db.SetTOTPSecret(r.Context(), user.ID, ""); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.audit.Record(r.Context(), store.Audit{ActorID: user.ID, ActorName: user.Username, Action: "auth.totp_disabled", RemoteIP: s.clientIP(r)})
	http.Redirect(w, r, flashURL("/account/security", "双因素认证已关闭", "ok"), http.StatusSeeOther)
}

func (s *Server) accountSessionRevoke(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	publicID := strings.TrimSpace(r.PathValue("id"))
	current, err := s.db.DeleteUserSession(r.Context(), sess.User.ID, publicID, sess.TokenHash)
	if err != nil {
		if store.IsNotFound(err) {
			s.redirectError(w, r, "/account/security", "会话不存在或已失效")
			return
		}
		s.serverError(w, r, err)
		return
	}
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "auth.session_revoked", RemoteIP: s.clientIP(r)})
	if current {
		s.clearSessionCookies(w)
		http.Redirect(w, r, flashURL("/login", "当前会话已退出", "ok"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, flashURL("/account/security", "会话已吊销", "ok"), http.StatusSeeOther)
}

func (s *Server) accountTokenCreate(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	user, err := s.db.GetUserByID(r.Context(), sess.User.ID)
	if err != nil {
		s.userLookupError(w, r, err)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	if name == "" || utf8.RuneCountInString(name) > 64 || strings.ContainsAny(name, "\r\n") {
		s.redirectError(w, r, "/account/security", "令牌名称需要 1 到 64 个字符")
		return
	}
	role := r.Form.Get("role")
	if !allowedTokenRole(user.Role, role) {
		s.redirectError(w, r, "/account/security", "令牌权限不能高于当前账号")
		return
	}
	if user.TOTPEnabled() && !validTOTP(user.TOTPSecret, r.Form.Get("code"), time.Now()) {
		s.redirectError(w, r, "/account/security", "创建令牌需要当前验证码")
		return
	}
	raw, err := auth.NewToken()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var expires *time.Time
	switch r.Form.Get("ttl") {
	case "30":
		t := time.Now().Add(30 * 24 * time.Hour)
		expires = &t
	case "90":
		t := time.Now().Add(90 * 24 * time.Hour)
		expires = &t
	case "never":
	default:
		t := time.Now().Add(30 * 24 * time.Hour)
		expires = &t
	}
	token := store.APIToken{ID: id.New("tok"), UserID: user.ID, Name: name, TokenHash: tokenHash(raw), Role: role, ExpiresAt: expires}
	if err := s.db.CreateAPIToken(r.Context(), token); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.audit.Record(r.Context(), store.Audit{ActorID: user.ID, ActorName: user.Username, Action: "auth.token_created", RemoteIP: s.clientIP(r), Detail: map[string]any{"name": name, "role": role}})
	s.renderAccountSecurity(w, r, user, sess.TokenHash, "令牌已创建，只显示一次。", raw)
}

func allowedTokenRole(userRole, tokenRole string) bool {
	for _, role := range tokenRoles(userRole) {
		if role == tokenRole {
			return true
		}
	}
	return false
}

func (s *Server) accountTokenRevoke(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	if err := s.db.RevokeAPIToken(r.Context(), sess.User.ID, r.PathValue("id")); err != nil {
		if store.IsNotFound(err) {
			s.redirectError(w, r, "/account/security", "令牌不存在或已吊销")
			return
		}
		s.serverError(w, r, err)
		return
	}
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "auth.token_revoked", RemoteIP: s.clientIP(r), Detail: map[string]any{"token_id": r.PathValue("id")}})
	http.Redirect(w, r, flashURL("/account/security", "令牌已吊销", "ok"), http.StatusSeeOther)
}

func pendingTOTP(r *http.Request) (string, bool) {
	c, err := r.Cookie("wvc_totp_setup")
	if err != nil || len(c.Value) < 16 {
		return "", false
	}
	return c.Value, true
}

func (s *Server) setPendingTOTP(w http.ResponseWriter, secret string) {
	// #nosec G124 -- Secure follows deployment mode, while HttpOnly and SameSite are always enabled.
	http.SetCookie(w, &http.Cookie{Name: "wvc_totp_setup", Value: secret, Path: "/account/security", MaxAge: 600, HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
}

func (s *Server) clearPendingTOTP(w http.ResponseWriter) {
	// #nosec G124 -- Secure follows deployment mode, while HttpOnly and SameSite are always enabled.
	http.SetCookie(w, &http.Cookie{Name: "wvc_totp_setup", Value: "", Path: "/account/security", MaxAge: -1, HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
}

var errHostForbidden = errors.New("host access denied")
