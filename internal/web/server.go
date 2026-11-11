package web

import (
	"bufio"
	"context"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"webvirtcloud-go/internal/audit"
	"webvirtcloud-go/internal/command"
	"webvirtcloud-go/internal/config"
	"webvirtcloud-go/internal/console"
	"webvirtcloud-go/internal/id"
	"webvirtcloud-go/internal/libvirt"
	"webvirtcloud-go/internal/secret"
	"webvirtcloud-go/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Server struct {
	cfg             config.Config
	db              *store.Store
	cipher          *secret.Cipher
	runner          *command.Runner
	libvirt         *libvirt.Service
	audit           *audit.Recorder
	tokens          *console.Tokens
	proxy           *console.Proxy
	log             *slog.Logger
	tmpl            map[string]*template.Template
	dummy           string
	http            *http.Server
	trustedNets     []*net.IPNet
	consoleSessions atomic.Int64
	consoleSlots    chan struct{}
	consoleMu       sync.Mutex
	consoleBackends map[*console.Backend]struct{}
	touchSlots      chan struct{}
	touchWG         sync.WaitGroup
	catalogMu       sync.Mutex
	catalogCache    map[string]*commandDiscoveryEntry
}

type commandDiscoveryEntry struct {
	discovery command.Discovery
	cachedAt  time.Time
	inFlight  chan struct{}
}

const sessionTouchWorkers = 8

type pageData struct {
	Title     string
	Nav       string
	User      *store.User
	CSRF      string
	Flash     string
	FlashType string
	Data      any
}

func New(cfg config.Config, db *store.Store, cipher *secret.Cipher, runner *command.Runner, recorder *audit.Recorder, log *slog.Logger) (*Server, error) {
	if cfg.ConsoleMaxSessions <= 0 {
		cfg.ConsoleMaxSessions = config.DefaultMaxConsoleSessions
	}
	if cfg.LoginIPMaxFailures <= 0 {
		cfg.LoginIPMaxFailures = cfg.LoginMaxFailures * 5
		if cfg.LoginIPMaxFailures < 1 {
			cfg.LoginIPMaxFailures = config.DefaultLoginIPMaxFailures
		}
	}
	if cfg.LoginGlobalMaxFailures <= 0 {
		cfg.LoginGlobalMaxFailures = cfg.LoginIPMaxFailures * 8
	}
	if cfg.MaxSessionsPerUser <= 0 {
		cfg.MaxSessionsPerUser = config.DefaultMaxSessionsPerUser
	}
	db.SetCipher(cipher)
	tokens, err := console.NewTokens(cipher.DerivedKey("console"), cfg.ConsoleTokenTTL)
	if err != nil {
		return nil, err
	}
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	dummy, err := authDummyHash()
	if err != nil {
		return nil, err
	}
	trustedNets, err := parseTrustedNets(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:             cfg,
		db:              db,
		cipher:          cipher,
		runner:          runner,
		libvirt:         libvirt.New(db, cipher, runner),
		audit:           recorder,
		tokens:          tokens,
		log:             log,
		tmpl:            tmpl,
		dummy:           dummy,
		trustedNets:     trustedNets,
		consoleSlots:    make(chan struct{}, cfg.ConsoleMaxSessions),
		consoleBackends: make(map[*console.Backend]struct{}),
		touchSlots:      make(chan struct{}, sessionTouchWorkers),
		catalogCache:    make(map[string]*commandDiscoveryEntry),
	}
	s.proxy = console.NewProxy(tokens, cfg.TrustProxy, s.isTrustedProxy)
	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 * 1024,
	}
	return s, nil
}

func parseTemplates() (map[string]*template.Template, error) {
	pages, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	funcs := template.FuncMap{
		"date": func(t time.Time) string {
			if t.IsZero() || t.Unix() <= 0 {
				return "-"
			}
			return t.Local().Format("2006-01-02 15:04:05")
		},
		"short": func(s string) string {
			if len([]rune(s)) <= 12 {
				return s
			}
			return string([]rune(s)[:12]) + "…"
		},
		"statusClass": func(status string) string {
			switch status {
			case "succeeded", "running", "active":
				return "ok"
			case "failed", "cancelled", "inactive", "shut off":
				return "bad"
			case "queued":
				return "warn"
			default:
				return "muted"
			}
		},
		"roleName": func(role string) string {
			switch role {
			case "admin":
				return "管理员"
			case "operator":
				return "运维"
			case "desktop":
				return "桌面用户"
			case "viewer":
				return "只读"
			default:
				return role
			}
		},
		"toolStatus": func(available bool) string {
			if available {
				return "已安装"
			}
			return "缺失"
		},
		"jobHostLabel":     jobHostLabel,
		"pathEscape":       func(s string) string { return url.PathEscape(s) },
		"add":              func(a, b int) int { return a + b },
		"sub":              func(a, b int) int { return a - b },
		"consoleAvailable": consoleAvailable,
		"auditDetail":      auditDetailText,
	}
	out := make(map[string]*template.Template)
	for _, page := range pages {
		name := strings.TrimSuffix(strings.TrimPrefix(page, "templates/"), ".html")
		if name == "base" {
			continue
		}
		t, err := template.New(name).Funcs(funcs).ParseFS(templateFS, "templates/base.html", page)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		out[name] = t
	}
	return out, nil
}

func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.Addr, err)
	}
	defer s.closeConsoleBackends()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	purgeDone := make(chan struct{})
	go func() {
		defer close(purgeDone)
		s.purgeSessions(runCtx, s.log)
	}()
	go func() {
		s.log.Info("web server listening", "addr", ln.Addr().String())
		errCh <- s.http.Serve(ln)
	}()
	var runErr error
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	}

	// Hijacked WebSocket connections are not tracked by http.Server.Shutdown.
	// Close the VNC/SSH backends first so the proxy goroutines can exit and
	// the process does not leave console tunnels behind during a restart.
	s.closeConsoleBackends()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	shutdownErr := s.http.Shutdown(shutdownCtx)
	shutdownCancel()
	if runErr == nil {
		runErr = shutdownErr
	}
	if shutdownErr != nil {
		_ = s.http.Close()
	}
	cancel()
	select {
	case <-purgeDone:
	case <-time.After(5 * time.Second):
		s.log.Warn("timed out waiting for session cleanup to stop")
	}
	if shutdownErr == nil {
		// No handlers can register another touch after a clean Shutdown.
		s.touchWG.Wait()
	}
	return runErr
}

func (s *Server) trackConsoleBackend(backend *console.Backend) func() {
	if backend == nil {
		return func() {}
	}
	s.consoleMu.Lock()
	if s.consoleBackends == nil {
		s.consoleBackends = make(map[*console.Backend]struct{})
	}
	s.consoleBackends[backend] = struct{}{}
	s.consoleMu.Unlock()
	return func() {
		s.consoleMu.Lock()
		delete(s.consoleBackends, backend)
		s.consoleMu.Unlock()
	}
}

func (s *Server) closeConsoleBackends() {
	s.consoleMu.Lock()
	backends := make([]*console.Backend, 0, len(s.consoleBackends))
	for backend := range s.consoleBackends {
		backends = append(backends, backend)
	}
	s.consoleMu.Unlock()
	for _, backend := range backends {
		_ = backend.Close()
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.staticHandler()))
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /metrics", s.metrics)
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/favicon.svg", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)

	mux.Handle("GET /{$}", s.protect("viewer", s.dashboard))
	mux.Handle("POST /logout", s.protect("viewer", s.logout))

	mux.Handle("GET /hosts", s.protect("viewer", s.hostsPage))
	mux.Handle("GET /hosts/new", s.protect("admin", s.hostNewPage))
	mux.Handle("POST /hosts/new", s.protect("admin", s.hostCreate))
	mux.Handle("GET /hosts/{id}", s.protect("viewer", s.hostDetail))
	mux.Handle("GET /hosts/{id}/ops", s.protect("viewer", s.hostOpsPage))
	mux.Handle("GET /hosts/{id}/edit", s.protect("admin", s.hostEditPage))
	mux.Handle("POST /hosts/{id}/edit", s.protect("admin", s.hostUpdate))
	mux.Handle("POST /hosts/{id}/delete", s.protect("admin", s.hostDelete))
	mux.Handle("POST /hosts/{id}/test", s.protect("operator", s.hostTest))
	mux.Handle("POST /hosts/{id}/health", s.protect("operator", s.hostHealth))
	mux.Handle("GET /hosts/{id}/vms", s.protect("viewer", s.hostVMsPage))
	mux.Handle("GET /hosts/{id}/storage", s.protect("viewer", s.storagePage))
	mux.Handle("POST /hosts/{id}/storage/action", s.protect("operator", s.storageAction))
	mux.Handle("GET /hosts/{id}/networks", s.protect("viewer", s.networksPage))
	mux.Handle("POST /hosts/{id}/networks/action", s.protect("operator", s.networkAction))
	mux.Handle("GET /hosts/{id}/command", s.protect("admin", s.commandPage))
	mux.Handle("POST /hosts/{id}/command", s.protect("admin", s.commandSubmit))
	mux.Handle("GET /hosts/{id}/command/catalog", s.protect("admin", s.commandCatalogAPI))
	mux.Handle("GET /vms", s.protect("viewer", s.allVMsPage))
	mux.Handle("GET /hosts/{id}/vms/{vm}", s.protect("viewer", s.vmPage))
	mux.Handle("POST /hosts/{id}/vms/{vm}/action", s.protect("operator", s.vmAction))
	mux.Handle("POST /hosts/{id}/vms/{vm}/disk-resize", s.protect("operator", s.vmDiskResize))
	mux.Handle("POST /hosts/{id}/vms/{vm}/resource", s.protect("operator", s.vmResourceAction))
	mux.Handle("POST /hosts/{id}/batch", s.protect("operator", s.vmBatchAction))
	mux.Handle("GET /jobs", s.protect("viewer", s.jobsPage))
	mux.Handle("GET /jobs/{id}", s.protect("viewer", s.jobPage))
	mux.Handle("POST /jobs/{id}/cancel", s.protect("viewer", s.jobCancel))
	mux.Handle("GET /api/jobs/{id}", s.protect("viewer", s.jobAPI))
	mux.Handle("GET /audit", s.protect("operator", s.auditPage))
	mux.Handle("GET /users", s.protect("admin", s.usersPage))
	mux.Handle("GET /users/new", s.protect("admin", s.userNewPage))
	mux.Handle("POST /users/new", s.protect("admin", s.userCreate))
	mux.Handle("POST /users/{id}/role", s.protect("admin", s.userRole))
	mux.Handle("POST /users/{id}/disabled", s.protect("admin", s.userDisabled))
	mux.Handle("POST /users/{id}/password", s.protect("admin", s.userPassword))
	mux.Handle("GET /users/{id}/access", s.protect("admin", s.userAccessPage))
	mux.Handle("POST /users/{id}/access", s.protect("admin", s.userAccessUpdate))
	mux.Handle("GET /account/password", s.protect("viewer", s.accountPasswordPage))
	mux.Handle("POST /account/password", s.protect("viewer", s.accountPassword))
	mux.Handle("GET /console/{host}/{vm}", s.protect("viewer", s.consolePage))
	mux.Handle("GET /console/{host}/{vm}/ws", s.protect("viewer", s.consoleWS))
	mux.Handle("GET /console/{host}/{vm}/password", s.protect("viewer", s.consolePassword))

	mux.HandleFunc("/", s.notFound)
	return s.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	// Keep recovery inside request logging so panics are still auditable, but
	// outside every other middleware so security headers and request/session
	// handling cannot bypass the recovery boundary.
	return s.requestID(s.requestLog(s.recoverer(s.securityHeaders(s.maxBody(s.session(next))))))
}

type requestIDKey struct{}

func requestIDFrom(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := id.New("req")
		w.Header().Set("X-Request-ID", requestID)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, requestID))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "error", rec, "path", r.URL.Path, "request_id", requestIDFrom(r.Context()), "stack", string(debug.Stack()))
				if responseCommitted(w) {
					return
				}
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.cfg.SecureCookies {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		state := &requestLogState{}
		r = r.WithContext(context.WithValue(r.Context(), requestLogStateKey{}, state))
		recorder := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		// Avoid logging static asset noise and VNC bytes.
		if !strings.HasPrefix(r.URL.Path, "/static/") && !strings.HasSuffix(r.URL.Path, "/ws") {
			s.log.Info("http", "request_id", requestIDFrom(r.Context()), "method", r.Method,
				"path", r.URL.Path, "status", recorder.status, "ip", s.clientIP(r),
				"user_id", state.userID, "duration", time.Since(start).String())
		}
	})
}

type requestLogStateKey struct{}

type requestLogState struct {
	userID string
}

func setRequestUser(r *http.Request, userID string) {
	if state, ok := r.Context().Value(requestLogStateKey{}).(*requestLogState); ok {
		state.userID = userID
	}
}

func responseCommitted(w http.ResponseWriter) bool {
	if state, ok := w.(interface{ Committed() bool }); ok {
		return state.Committed()
	}
	return false
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	hijacked    bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader || w.hijacked {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.hijacked {
		return 0, errors.New("response has already been hijacked")
	}
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) Committed() bool { return w.wroteHeader || w.hijacked }

func (w *statusWriter) Flush() {
	if w.hijacked {
		return
	}
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.hijacked {
		return nil, nil, errors.New("response has already been hijacked")
	}
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	conn, rw, err := hijacker.Hijack()
	if err == nil {
		w.status = http.StatusSwitchingProtocols
		w.wroteHeader = true
		w.hijacked = true
	}
	return conn, rw, err
}

func (w *statusWriter) Push(target string, opts *http.PushOptions) error {
	if w.hijacked {
		return http.ErrNotSupported
	}
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (s *Server) maxBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		token, unambiguous := readNamedCookie(r, s.sessionCookieNames()...)
		if !unambiguous {
			s.log.Warn("ambiguous session cookies rejected", "request_id", requestIDFrom(r.Context()), "ip", s.clientIP(r))
			http.Error(w, "invalid session cookie", http.StatusBadRequest)
			return
		}
		if token != "" {
			sess, err := s.db.GetSession(r.Context(), tokenHash(token))
			if err == nil {
				r = r.WithContext(withSession(r.Context(), sess))
				setRequestUser(r, sess.User.ID)
				if time.Since(sess.LastSeen) > 5*time.Minute {
					s.touchSessionAsync(sess.TokenHash, s.clientIP(r), r.UserAgent())
				}
			} else if !store.IsNotFound(err) {
				s.log.Warn("load session", "error", err)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) touchSessionAsync(tokenHash, ip, userAgent string) bool {
	if s.touchSlots == nil {
		return false
	}
	select {
	case s.touchSlots <- struct{}{}:
	default:
		// A stale last_seen is harmless; the next request will retry.
		return false
	}
	s.touchWG.Add(1)
	go func() {
		defer s.touchWG.Done()
		defer func() { <-s.touchSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.db.TouchSession(ctx, tokenHash, ip, userAgent); err != nil {
			s.log.Warn("touch session", "error", err)
		}
	}()
	return true
}

func withSession(ctx context.Context, sess store.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, sess)
}

type sessionKey struct{}

func sessionFrom(ctx context.Context) (store.Session, bool) {
	v, ok := ctx.Value(sessionKey{}).(store.Session)
	return v, ok
}

func (s *Server) protect(role string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := sessionFrom(r.Context())
		if !ok {
			s.redirectLogin(w, r)
			return
		}
		if !roleAllowed(sess.User.Role, role) {
			s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username,
				Action: "authz.denied", RemoteIP: s.clientIP(r), Detail: map[string]any{"method": r.Method, "path": r.URL.Path, "required": role}})
			s.renderError(w, r, http.StatusForbidden, "权限不足", "当前账号没有执行此操作的权限。")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
			if err := r.ParseForm(); err != nil {
				s.renderError(w, r, http.StatusBadRequest, "请求无效", "无法解析提交的数据。")
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Form.Get("_csrf")), []byte(sess.CSRFToken)) != 1 {
				s.audit.Record(r.Context(), store.Audit{ActorID: sess.User.ID, ActorName: sess.User.Username,
					Action: "authz.csrf_failed", RemoteIP: s.clientIP(r), Detail: map[string]any{"method": r.Method, "path": r.URL.Path}})
				s.renderError(w, r, http.StatusForbidden, "CSRF 校验失败", "页面已过期，请刷新后重试。")
				return
			}
		}
		next(w, r)
	})
}

func roleAllowed(actual, minimum string) bool {
	rank := map[string]int{"viewer": 1, "operator": 2, "admin": 3}
	return rank[actual] >= rank[minimum] && rank[actual] > 0
}

func (s *Server) redirectLogin(w http.ResponseWriter, r *http.Request) {
	requestURI := r.URL.RequestURI()
	if strings.HasPrefix(r.URL.Path, "/console/") &&
		(strings.HasSuffix(r.URL.Path, "/ws") || strings.HasSuffix(r.URL.Path, "/password")) {
		// Console tokens are bearer capabilities in the query string. Redirect
		// back to the console page instead of preserving the token in a login URL.
		requestURI = strings.TrimSuffix(strings.TrimSuffix(r.URL.Path, "/ws"), "/password")
	}
	next := safeNext(requestURI)
	if next == "" {
		next = "/"
	}
	// #nosec G710 -- next is restricted to local application paths by safeNext.
	http.Redirect(w, r, "/login?next="+template.URLQueryEscaper(next), http.StatusSeeOther)
}

func (s *Server) clientIP(r *http.Request) string {
	remote := remoteIP(r.RemoteAddr)
	if remote == nil {
		return ""
	}
	if !s.isTrustedProxy(r) {
		return remote.String()
	}
	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if xff == "" {
		return remote.String()
	}
	parts := strings.Split(xff, ",")
	client := remote
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil {
			return remote.String()
		}
		client = ip
		if !ipTrusted(ip, s.trustedNets) {
			return ip.String()
		}
	}
	return client.String()
}

// isTrustedProxy checks the immediate peer, not X-Forwarded-For. Forwarded
// scheme information is only meaningful when the TCP peer itself is a known
// reverse proxy.
func (s *Server) isTrustedProxy(r *http.Request) bool {
	if !s.cfg.TrustProxy {
		return false
	}
	remote := remoteIP(r.RemoteAddr)
	return remote != nil && ipTrusted(remote, s.trustedNets)
}

func remoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	host = strings.TrimSpace(host)
	if i := strings.LastIndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	return net.ParseIP(host)
}

func parseTrustedNets(values []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !strings.Contains(value, "/") {
			ip := net.ParseIP(value)
			if ip == nil {
				return nil, fmt.Errorf("invalid trusted proxy address %q", value)
			}
			if ip.To4() != nil {
				value += "/32"
			} else {
				value += "/128"
			}
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", value, err)
		}
		nets = append(nets, network)
	}
	return nets, nil
}

func ipTrusted(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.readinessDependencies(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	workers, err := s.db.CountLiveWorkers(ctx, time.Minute)
	if err != nil || workers == 0 {
		http.Error(w, "no live job worker", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ready\n"))
}

// readinessDependencies validates only the local executable required by the
// host profiles that are actually enabled. A panel that manages qemu+ssh hosts
// therefore does not need a local virsh/qemu-img installation, while a local or
// qemu+tcp/tls host still fails readiness when virsh is absent. Optional QEMU
// utilities remain visible per command and do not make the whole control plane
// unready. Host reachability is deliberately excluded: one offline KVM host
// must not remove every healthy host from a load balancer.
func (s *Server) readinessDependencies(ctx context.Context) error {
	hosts, err := s.db.ListHosts(ctx)
	if err != nil {
		return errors.New("host configuration unavailable")
	}
	needsSSH, needsVirsh := false, false
	for _, host := range hosts {
		if !host.Enabled {
			continue
		}
		uri, err := libvirt.DecryptURI(s.cipher, host)
		if err != nil {
			s.log.Debug("readiness skipped invalid host configuration", "host", host.Name, "error", err)
			return errors.New("enabled host configuration is invalid")
		}
		target, err := command.ResolveTarget(uri)
		if err != nil {
			s.log.Debug("readiness skipped invalid host target", "host", host.Name, "error", err)
			return errors.New("enabled host configuration is invalid")
		}
		if target.Kind == command.TargetSSH {
			needsSSH = true
		} else {
			needsVirsh = true
		}
	}
	if needsSSH && !s.runner.SSHAvailable() {
		return errors.New("enabled qemu+ssh host requires an OpenSSH client")
	}
	if needsVirsh && !s.runner.ToolAvailable("virsh") {
		return errors.New("enabled local/libvirt host requires virsh")
	}
	return nil
}
