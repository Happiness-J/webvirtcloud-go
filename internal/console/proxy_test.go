package console

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"webvirtcloud-go/internal/libvirt"
)

func TestProxyEcho(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				_, _ = conn.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	backend, err := dialTCP(context.Background(), listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := NewTokens(make([]byte, 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	proxy := NewProxy(tokens, false, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = proxy.Upgrade(w, r, backend)
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	payload := []byte("RFB 003.008\n")
	if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, got, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q want %q", fmt.Sprintf("%q", got), fmt.Sprintf("%q", payload))
	}
}

func TestSameOrigin(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		origin    string
		forwarded string
		trusted   bool
		tls       bool
		want      bool
	}{
		{name: "direct http", host: "panel.example:8080", origin: "http://panel.example:8080", want: true},
		{name: "direct https", host: "panel.example", origin: "https://panel.example", tls: true, want: true},
		{name: "trusted proxy https", host: "panel.example", origin: "https://panel.example", forwarded: "https", trusted: true, want: true},
		{name: "trusted proxy https default port", host: "panel.example:443", origin: "https://panel.example", forwarded: "https", trusted: true, want: true},
		{name: "untrusted forwarded peer", host: "panel.example", origin: "https://panel.example", forwarded: "https", want: false},
		{name: "cross-origin forwarded header", host: "panel.example", origin: "https://evil.example", forwarded: "https", trusted: true, want: false},
		{name: "wrong scheme", host: "panel.example", origin: "http://panel.example", forwarded: "https", trusted: true, want: false},
		{name: "origin userinfo", host: "panel.example", origin: "https://user@panel.example", forwarded: "https", trusted: true, want: false},
		{name: "empty origin", host: "panel.example", origin: "", want: false},
		{name: "origin path", host: "panel.example", origin: "https://panel.example/path", forwarded: "https", trusted: true, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://"+tc.host+"/console/host/vm/ws", nil)
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			if tc.forwarded != "" {
				r.Header.Set("X-Forwarded-Proto", tc.forwarded)
			}
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			var trustedProxy func(*http.Request) bool
			if tc.trusted {
				trustedProxy = func(*http.Request) bool { return true }
			}
			if got := sameOrigin(r, trustedProxy); got != tc.want {
				t.Fatalf("sameOrigin()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestLocalConsoleAddressRewrite(t *testing.T) {
	tests := []struct {
		address string
		rewrite string
		want    string
	}{
		{address: "", want: "127.0.0.1"},
		{address: "0.0.0.0", want: "127.0.0.1"},
		{address: "::", want: "::1"},
		{address: "127.0.0.1", rewrite: "host.docker.internal", want: "host.docker.internal"},
		{address: "::1", rewrite: "host.docker.internal", want: "host.docker.internal"},
		{address: "localhost", rewrite: "host.docker.internal", want: "host.docker.internal"},
		{address: "0.0.0.0", rewrite: "host.docker.internal", want: "host.docker.internal"},
		{address: "192.168.10.20", rewrite: "host.docker.internal", want: "192.168.10.20"},
	}
	for _, tc := range tests {
		if got := localConsoleAddress(tc.address, tc.rewrite); got != tc.want {
			t.Errorf("localConsoleAddress(%q, %q) = %q, want %q", tc.address, tc.rewrite, got, tc.want)
		}
	}
}

func TestRemoteConsoleAddress(t *testing.T) {
	tests := []struct {
		name           string
		managementHost string
		address        string
		want           string
	}{
		{name: "specific private address", managementHost: "libvirt.example", address: "10.0.0.12", want: "10.0.0.12"},
		{name: "public management host fallback", managementHost: "libvirt.example", address: "", want: "libvirt.example"},
		{name: "ipv4 loopback fallback", managementHost: "libvirt.example", address: "127.0.0.1", want: "libvirt.example"},
		{name: "ipv6 loopback fallback", managementHost: "libvirt.example", address: "::1", want: "libvirt.example"},
		{name: "localhost fallback", managementHost: "libvirt.example", address: "localhost", want: "libvirt.example"},
		{name: "wildcard fallback", managementHost: "libvirt.example", address: "0.0.0.0", want: "libvirt.example"},
		{name: "ipv6 wildcard fallback", managementHost: "libvirt.example", address: "::", want: "libvirt.example"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := remoteConsoleAddress(tc.managementHost, tc.address); got != tc.want {
				t.Fatalf("remoteConsoleAddress(%q, %q) = %q, want %q", tc.managementHost, tc.address, got, tc.want)
			}
		})
	}
}

func TestProxyRejectsMissingOrigin(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	tokens, err := NewTokens(make([]byte, 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	proxy := NewProxy(tokens, false, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = proxy.Upgrade(w, r, &Backend{conn: serverConn})
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatal("WebSocket without Origin unexpectedly connected")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("handshake status = %#v, want 403", response)
	}
}

func TestSSHForwardAddress(t *testing.T) {
	valid := map[string]string{
		"":          "127.0.0.1",
		"0.0.0.0":   "127.0.0.1",
		"localhost": "127.0.0.1",
		"::":        "::1",
		"10.1.2.3":  "10.1.2.3",
		"127.0.0.1": "127.0.0.1",
	}
	for input, want := range valid {
		got, err := sshForwardAddress(input)
		if err != nil || got != want {
			t.Errorf("sshForwardAddress(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"8.8.8.8", "example.com", "169.254.1.1"} {
		if _, err := sshForwardAddress(input); err == nil {
			t.Errorf("sshForwardAddress(%q) unexpectedly succeeded", input)
		}
	}
}

func TestSSHDialArgsUsesURIPort(t *testing.T) {
	u, err := url.Parse("qemu+ssh://root@libvirt.example:2222/system?keyfile=%2Fetc%2Fwvc%2Fid_ed25519")
	if err != nil {
		t.Fatal(err)
	}
	args, err := sshDialArgs(u, u.Hostname(), "10.0.0.12", "5901")
	if err != nil {
		t.Fatal(err)
	}
	if !containsArgPair(args, "-p", "2222") {
		t.Fatalf("ssh args do not contain the URI port: %q", args)
	}
	if !containsArgPair(args, "-W", "10.0.0.12:5901") {
		t.Fatalf("ssh args do not contain the forwarding target: %q", args)
	}
	if !containsArgPair(args, "-i", "/etc/wvc/id_ed25519") {
		t.Fatalf("ssh args do not contain the key file: %q", args)
	}
	if len(args) < 2 || args[len(args)-2] != "--" || args[len(args)-1] != "root@libvirt.example" {
		t.Fatalf("ssh destination is not isolated after --: %q", args)
	}
}

func TestSSHDialArgsOmitsDefaultPort(t *testing.T) {
	u, err := url.Parse("qemu+ssh://libvirt.example/system")
	if err != nil {
		t.Fatal(err)
	}
	args, err := sshDialArgs(u, u.Hostname(), "127.0.0.1", "5900")
	if err != nil {
		t.Fatal(err)
	}
	for i, arg := range args {
		if arg == "-p" {
			t.Fatalf("default SSH port unexpectedly added at index %d: %q", i, args)
		}
	}
}

func containsArgPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestResolvePrivateTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addresses, err := resolvePrivateTargets(ctx, "localhost")
	if err != nil || len(addresses) == 0 {
		t.Fatalf("localhost: %v, %v", addresses, err)
	}
	for _, address := range addresses {
		if err := checkPrivateIP(address); err != nil {
			t.Fatalf("localhost resolved to a rejected address %s: %v", address, err)
		}
	}
	if _, err := resolvePrivateTargets(ctx, "8.8.8.8"); err == nil {
		t.Fatal("public VNC address was accepted")
	}
	if _, err := resolvePrivateTargets(ctx, "does-not-exist.invalid"); err == nil {
		t.Fatal("unresolvable VNC address was accepted")
	}
}

func TestDialValidatesURI(t *testing.T) {
	con := libvirt.Console{Address: "127.0.0.1", Port: 5900}
	if _, err := Dial(context.Background(), "qemu+ssh://-oProxyCommand=x/system", con, DialOptions{}); err == nil {
		t.Fatal("unsafe libvirt URI was accepted")
	}
	if _, err := Dial(context.Background(), "qemu+tcp://example/system", con, DialOptions{}); err == nil {
		t.Fatal("insecure remote VNC was accepted by default")
	}
}

func TestDialUsesConfiguredSSHBinary(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	fakeSSH := filepath.Join(dir, "custom-ssh")
	script := "#!/bin/sh\nprintf 'invoked\\n' > \"" + marker + "\"\nexec cat\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	backend, err := Dial(context.Background(), "qemu+ssh://root@kvm.example/system", libvirt.Console{
		Address: "127.0.0.1", Port: 5900,
	}, DialOptions{SSHPath: fakeSSH})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if data, readErr := os.ReadFile(marker); readErr == nil && strings.TrimSpace(string(data)) == "invoked" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("configured SSH binary was not executed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
