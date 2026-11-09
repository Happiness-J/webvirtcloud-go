package web

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"webvirtcloud-go/internal/auth"
	"webvirtcloud-go/internal/store"
)

const (
	legacySessionCookie   = "wvc_session"
	legacyLoginCSRFCookie = "wvc_login_csrf"
	secureSessionCookie   = "__Host-wvc_session"
	secureLoginCSRFCookie = "__Host-wvc_login_csrf"
)

func tokenHash(token string) string { return auth.TokenHash(token) }

func authDummyHash() (string, error) {
	return auth.HashPassword("wvc-dummy-password-never-used")
}

func (s *Server) sessionCookieName() string {
	if s.cfg.SecureCookies {
		return secureSessionCookie
	}
	return legacySessionCookie
}

func (s *Server) loginCSRFCookieName() string {
	if s.cfg.SecureCookies {
		return secureLoginCSRFCookie
	}
	return legacyLoginCSRFCookie
}

func (s *Server) sessionCookieNames() []string {
	if s.cfg.SecureCookies {
		// __Host- cookies cannot be shadowed by a sibling subdomain. Falling
		// back to the legacy name in HTTPS mode would reintroduce that risk.
		return []string{secureSessionCookie}
	}
	return []string{legacySessionCookie}
}

func (s *Server) loginCSRFCookieNames() []string {
	if s.cfg.SecureCookies {
		return []string{secureLoginCSRFCookie}
	}
	return []string{legacyLoginCSRFCookie}
}

func (s *Server) cookiePath() string {
	if s.cfg.SecureCookies {
		return "/"
	}
	return "/"
}

func loginKeys(ip, username string) []string {
	// Usernames are case-sensitive in the database. Keep the exact trimmed
	// spelling so distinct accounts such as Alice and alice cannot share a
	// limiter bucket. Do not add a global username-only counter: unauthenticated
	// callers could then deliberately lock out a known account.
	username = strings.TrimSpace(username)
	return []string{
		"pair:" + auth.TokenHash("ip:"+ip+"\x00user:"+username),
		"ip:" + auth.TokenHash("ip:"+ip),
		"global:login",
	}
}

func reauthKeys(ip, userID string) []string {
	return []string{
		"reauth-pair:" + auth.TokenHash("ip:"+ip+"\x00user:"+userID),
		"reauth-ip:" + auth.TokenHash("ip:"+ip),
	}
}

func validLoginUsername(username string) bool {
	if username == "" || !utf8.ValidString(username) || len(username) > 128 || utf8.RuneCountInString(username) > 64 {
		return false
	}
	for _, r := range username {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func (s *Server) reserveLoginAttempt(r *http.Request, keys []string) (bool, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	return s.db.ReserveLoginAttempt(ctx, keys, s.cfg.LoginMaxFailures, s.cfg.LoginIPMaxFailures, s.cfg.LoginGlobalMaxFailures, s.cfg.LoginWindow)
}

func (s *Server) completeLoginAttempt(keys []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.db.CompleteLoginAttempt(ctx, keys); err != nil {
		s.log.Error("complete login attempt", "error", err)
	}
}

func readNamedCookie(r *http.Request, names ...string) (string, bool) {
	for _, name := range names {
		cookies := r.Cookies()
		var value string
		count := 0
		for _, cookie := range cookies {
			if cookie.Name == name {
				value = cookie.Value
				count++
			}
		}
		if count > 1 {
			return "", false
		}
		if count == 1 {
			return value, true
		}
	}
	return "", true
}

func (s *Server) setLoginCSRFCookie(w http.ResponseWriter, value string, maxAge int) {
	// #nosec G124 -- Secure follows deployment mode, while HttpOnly and SameSite are always enabled.
	http.SetCookie(w, &http.Cookie{
		Name:     s.loginCSRFCookieName(),
		Value:    value,
		Path:     s.cookiePath(),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{legacySessionCookie, secureSessionCookie} {
		// #nosec G124 -- Secure follows deployment mode, while HttpOnly and SameSite are always enabled.
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
			Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
		})
	}
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := sessionFrom(r.Context()); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	csrf, csrfOK := readNamedCookie(r, s.loginCSRFCookieNames()...)
	if !csrfOK || len(csrf) < 20 {
		var err error
		csrf, err = auth.NewToken()
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.setLoginCSRFCookie(w, csrf, 600)
	}
	next := safeNext(r.URL.Query().Get("next"))
	s.render(w, r, "login", pageData{Title: "登录", CSRF: csrf, Data: map[string]any{"Next": next}})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "登录失败", "请求格式无效。")
		return
	}
	cookieValue, cookieOK := readNamedCookie(r, s.loginCSRFCookieNames()...)
	if !cookieOK || cookieValue == "" || subtle.ConstantTimeCompare([]byte(cookieValue), []byte(r.Form.Get("_csrf"))) != 1 {
		s.renderError(w, r, http.StatusForbidden, "登录失败", "表单已过期，请刷新后重试。")
		return
	}
	username := strings.TrimSpace(r.Form.Get("username"))
	password := r.Form.Get("password")
	ip := s.clientIP(r)
	if !validLoginUsername(username) || password == "" || len(password) > 4096 {
		s.log.Warn("invalid login input", "ip", ip, "username_bytes", len(username), "password_bytes", len(password))
		s.renderLoginFailure(w, r)
		return
	}
	keys := loginKeys(ip, username)
	allowed, err := s.reserveLoginAttempt(r, keys)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !allowed {
		// The public login endpoint must not turn every rate-limited attempt
		// into a serialized audit-table write; login_attempts already records
		// the failures that caused the limit.
		s.log.Warn("login rate limited", "ip", ip, "username", username)
		s.renderError(w, r, http.StatusTooManyRequests, "登录失败", "尝试次数过多，请稍后再试。")
		return
	}
	user, err := s.db.GetUserByUsername(r.Context(), username)
	if err != nil {
		if !store.IsNotFound(err) {
			s.serverError(w, r, err)
			return
		}
		_ = auth.VerifyPasswordContext(r.Context(), password, s.dummy)
		s.audit.Record(r.Context(), store.Audit{Action: "auth.login_failed", ActorName: username, RemoteIP: ip})
		s.renderLoginFailure(w, r)
		return
	}
	verified := auth.VerifyPasswordContext(r.Context(), password, user.PasswordHash)
	if user.Disabled || !verified {
		s.audit.Record(r.Context(), store.Audit{Action: "auth.login_failed", ActorID: user.ID, ActorName: user.Username, RemoteIP: ip})
		s.renderLoginFailure(w, r)
		return
	}
	token, err := auth.NewToken()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	csrf, err := auth.NewToken()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	expires := time.Now().Add(s.cfg.SessionTTL)
	if err := s.db.CreateSession(r.Context(), tokenHash(token), user.ID, user.PasswordHash, csrf, expires, ip, r.UserAgent(), s.cfg.MaxSessionsPerUser); err != nil {
		if errors.Is(err, store.ErrSessionCredentialsChanged) {
			s.audit.Record(r.Context(), store.Audit{Action: "auth.login_failed", ActorID: user.ID, ActorName: user.Username, RemoteIP: ip})
			s.renderLoginFailure(w, r)
			return
		}
		s.serverError(w, r, err)
		return
	}
	s.completeLoginAttempt(keys)
	// #nosec G124 -- Secure follows deployment mode, while HttpOnly and SameSite are always enabled.
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(s.cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	if s.cfg.SecureCookies {
		// #nosec G124 -- this deletion cookie is explicitly Secure and clears a prior legacy cookie.
		http.SetCookie(w, &http.Cookie{Name: legacySessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true})
	}
	s.setLoginCSRFCookie(w, "", -1)
	s.audit.Record(r.Context(), store.Audit{ActorID: user.ID, ActorName: user.Username, Action: "auth.login", RemoteIP: ip})
	target := safeNext(r.Form.Get("next"))
	if target == "" {
		target = "/"
	}
	// #nosec G710 -- target is produced by safeNext and restricted to local application paths.
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) renderLoginFailure(w http.ResponseWriter, r *http.Request) {
	csrf, err := auth.NewToken()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.setLoginCSRFCookie(w, csrf, 600)
	s.renderStatus(w, r, http.StatusUnauthorized, "login", pageData{
		Title: "登录", CSRF: csrf, Flash: "用户名、密码或账号状态不正确。", FlashType: "error",
		Data: map[string]any{"Next": safeNext(r.Form.Get("next"))},
	})
}

func safeNext(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\t\\") {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%00") {
		return ""
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.Scheme != "" || u.User != nil || u.Fragment != "" {
		return ""
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.ContainsAny(u.Path, "\\\x00\r\n") {
		return ""
	}
	decoded, err := url.PathUnescape(u.EscapedPath())
	if err != nil || strings.ContainsAny(decoded, "\\\x00\r\n") || strings.HasPrefix(decoded, "//") {
		return ""
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return ""
		}
	}
	allowedPrefixes := []string{"/hosts", "/vms", "/jobs", "/audit", "/users", "/account", "/console"}
	allowed := u.Path == "/"
	for _, prefix := range allowedPrefixes {
		if u.Path == prefix || strings.HasPrefix(u.Path, prefix+"/") {
			allowed = true
			break
		}
	}
	if !allowed {
		return ""
	}
	if strings.HasPrefix(u.Path, "/console/") {
		if query, err := url.ParseQuery(u.RawQuery); err != nil || query.Get("token") != "" {
			return ""
		}
	}
	return u.RequestURI()
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	if err := s.db.DeleteSession(r.Context(), sess.TokenHash); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.clearSessionCookies(w)
	s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username, Action: "auth.logout", RemoteIP: s.clientIP(r)})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) purgeSessions(ctx context.Context, log *slog.Logger) {
	purge := func() {
		n, err := s.db.PurgeExpiredSessions(ctx)
		if err != nil {
			log.Error("purge sessions", "error", err)
		} else if n > 0 {
			log.Info("purged expired sessions", "count", n)
		}
		if n, err := s.db.PurgeLoginAttempts(ctx, s.cfg.LoginWindow); err != nil {
			log.Error("purge login attempts", "error", err)
		} else if n > 0 {
			log.Info("purged login attempts", "count", n)
		}
		if n, err := s.db.PurgeExpiredConsoleTokens(ctx); err != nil {
			log.Error("purge console tokens", "error", err)
		} else if n > 0 {
			log.Info("purged console tokens", "count", n)
		}
	}

	// Do not wait an hour before removing stale state left by a previous run.
	purge()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}
