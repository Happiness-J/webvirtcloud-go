package web

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"webvirtcloud-go/internal/config"
	"webvirtcloud-go/internal/store"
)

func TestSafeNext(t *testing.T) {
	valid := []string{
		"/",
		"/hosts",
		"/hosts/host_1?page=2",
		"/vms",
		"/jobs/job_1",
		"/audit?page=3",
		"/users",
		"/account/password",
		"/console/host_1/vm_1",
	}
	for _, raw := range valid {
		if got := safeNext(raw); got == "" {
			t.Errorf("safeNext(%q) unexpectedly rejected", raw)
		}
	}
	invalid := []string{
		"",
		"//evil.example/path",
		"https://evil.example/",
		"http://evil.example/hosts",
		"javascript:alert(1)",
		"/\\evil",
		"/%2f%2fevil",
		"/%5cevil",
		"/hosts/../users",
		"/hosts/%2e%2e/users",
		"/console/host/vm/ws?token=secret",
		"/console/host/vm/password?token=secret",
		"/login?next=/hosts",
		"/not-allowed",
		"/hosts/%00",
		"/hosts%0aLocation:%20https://evil.example",
	}
	for _, raw := range invalid {
		if got := safeNext(raw); got != "" {
			t.Errorf("safeNext(%q) = %q, want rejection", raw, got)
		}
	}
}

func TestRedirectLoginDropsConsoleBearerToken(t *testing.T) {
	server := &Server{cfg: config.Config{}}
	request := httptest.NewRequest(http.MethodGet, "http://panel.example/console/host_1/vm_1/ws?token=secret", nil)
	recorder := httptest.NewRecorder()
	server.redirectLogin(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", recorder.Code)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	next := location.Query().Get("next")
	if next != "/console/host_1/vm_1" {
		t.Fatalf("next = %q, want console page without token", next)
	}
	if strings.Contains(recorder.Header().Get("Location"), "secret") {
		t.Fatal("console bearer token leaked into login redirect")
	}
}

func TestEffectiveJobSensitive(t *testing.T) {
	tests := []struct {
		name string
		job  store.Job
		want bool
	}{
		{name: "ordinary read-only command", job: store.Job{Kind: "command", Tool: "virsh", ArgsJSON: `["list","--all"]`}, want: false},
		{name: "sensitive command", job: store.Job{Kind: "command", Tool: "virsh", ArgsJSON: `["destroy","vm"]`}, want: true},
		{name: "stored sensitive flag", job: store.Job{Kind: "command", Tool: "virsh", ArgsJSON: `["list"]`, Sensitive: true}, want: true},
		{name: "malformed command args", job: store.Job{Kind: "command", Tool: "virsh", ArgsJSON: `{`}, want: true},
		{name: "stored sensitive flag on other job kind", job: store.Job{Kind: "vm-action", Tool: "virsh", ArgsJSON: `["destroy","vm"]`, Sensitive: true}, want: true},
		{name: "vm action remains visible", job: store.Job{Kind: "vm-action", Tool: "virsh", ArgsJSON: `["destroy","vm"]`}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveJobSensitive(tc.job); got != tc.want {
				t.Fatalf("effectiveJobSensitive() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTemplateEscapesPathAndQueryOnce(t *testing.T) {
	tmpl := template.Must(template.New("paths").Funcs(template.FuncMap{
		"pathEscape": url.PathEscape,
	}).Parse(`<a href="/hosts/h/vms/{{pathEscape .Name}}?pool={{.Pool}}">`))
	var out strings.Builder
	if err := tmpl.Execute(&out, map[string]string{"Name": "vm 1/测试", "Pool": "pool a&b"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, `vm%201%2F%E6%B5%8B%E8%AF%95`) {
		t.Fatalf("escaped path missing from %q", got)
	}
	href := got[strings.Index(got, `"`)+1 : strings.LastIndex(got, `"`)]
	parsed, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	if decoded := parsed.Query().Get("pool"); decoded != "pool a&b" {
		t.Fatalf("query value = %q, want original value; href=%q", decoded, href)
	}
}

func TestClientIPTrustedProxyParsing(t *testing.T) {
	nets, err := parseTrustedNets([]string{"10.0.0.0/8", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: config.Config{TrustProxy: true}, trustedNets: nets}
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{name: "untrusted peer cannot spoof", remoteAddr: "203.0.113.9:1234", xff: "198.51.100.7", want: "203.0.113.9"},
		{name: "right to left trusted chain", remoteAddr: "10.0.0.5:1234", xff: "198.51.100.7, 10.0.0.6", want: "198.51.100.7"},
		{name: "only trusted hops", remoteAddr: "10.0.0.5:1234", xff: "10.0.0.6, 10.0.0.7", want: "10.0.0.6"},
		{name: "invalid header rejected", remoteAddr: "10.0.0.5:1234", xff: "not-an-ip", want: "10.0.0.5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://panel.example/", nil)
			request.RemoteAddr = tc.remoteAddr
			request.Header.Set("X-Forwarded-For", tc.xff)
			if got := server.clientIP(request); got != tc.want {
				t.Fatalf("clientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrustedProxyUsesImmediatePeer(t *testing.T) {
	nets, err := parseTrustedNets([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		trustProxy bool
		remoteAddr string
		want       bool
	}{
		{name: "trust disabled", remoteAddr: "10.0.0.5:1234", want: false},
		{name: "trusted peer", trustProxy: true, remoteAddr: "10.0.0.5:1234", want: true},
		{name: "untrusted peer", trustProxy: true, remoteAddr: "203.0.113.9:1234", want: false},
		{name: "invalid peer", trustProxy: true, remoteAddr: "not-an-address", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{cfg: config.Config{TrustProxy: tc.trustProxy}, trustedNets: nets}
			request := httptest.NewRequest(http.MethodGet, "http://panel.example/", nil)
			request.RemoteAddr = tc.remoteAddr
			request.Header.Set("X-Forwarded-For", "10.0.0.5")
			request.Header.Set("X-Forwarded-Proto", "https")
			if got := server.isTrustedProxy(request); got != tc.want {
				t.Fatalf("isTrustedProxy() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidLoginUsername(t *testing.T) {
	valid := []string{"admin", "user.name-1_2", "运维账号"}
	for _, username := range valid {
		if !validLoginUsername(username) {
			t.Errorf("validLoginUsername(%q) rejected", username)
		}
	}
	invalid := []string{
		"",
		"admin\n",
		"admin\x00",
		"admin\u202e",
		string([]byte{0xff, 0xfe}),
		strings.Repeat("a", 65),
	}
	for _, username := range invalid {
		if validLoginUsername(username) {
			t.Errorf("validLoginUsername(%q) accepted", username)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	server := &Server{cfg: config.Config{SecureCookies: true}}
	handler := server.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://panel.example/", nil))
	headers := recorder.Header()
	for key, want := range map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "no-referrer",
		"Cache-Control":                "no-store",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
	} {
		if got := headers.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if csp := headers.Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'self'") || strings.Contains(csp, "connect-src *") {
		t.Fatalf("unexpected CSP: %q", csp)
	}
}

func TestMetricsAuthorization(t *testing.T) {
	token := strings.Repeat("m", 32)
	server := &Server{cfg: config.Config{MetricsToken: token}}
	request := httptest.NewRequest(http.MethodGet, "http://panel.example/metrics", nil)
	if server.metricsAuthorized(request) {
		t.Fatal("metrics authorized without a bearer token")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if !server.metricsAuthorized(request) {
		t.Fatal("metrics rejected the configured bearer token")
	}
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
	if server.metricsAuthorized(request) {
		t.Fatal("metrics accepted a wrong bearer token")
	}
}

func TestDuplicateCookiesAreRejected(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://panel.example/", nil)
	request.Header.Add("Cookie", legacySessionCookie+"=one")
	request.Header.Add("Cookie", legacySessionCookie+"=two")
	if _, ok := readNamedCookie(request, legacySessionCookie); ok {
		t.Fatal("duplicate session cookie was accepted")
	}
}
