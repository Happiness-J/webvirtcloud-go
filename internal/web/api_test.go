package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"webvirtcloud-go/internal/config"
	"webvirtcloud-go/internal/libvirt"
)

func TestAPITokenIdentityUsesLeastPrivilege(t *testing.T) {
	readToken := strings.Repeat("r", 32)
	operatorToken := strings.Repeat("o", 32)
	adminToken := strings.Repeat("a", 32)
	server := &Server{cfg: config.Config{MetricsToken: readToken, APIToken: operatorToken, APIAdminToken: adminToken}}
	for _, tc := range []struct {
		name  string
		token string
		role  string
		id    string
	}{
		{name: "read", token: readToken, role: "viewer", id: apiMetricsUserID},
		{name: "operator", token: operatorToken, role: "operator", id: apiOperatorUserID},
		{name: "admin", token: adminToken, role: "admin", id: apiAdminUserID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://panel.example/api/v1/hosts", nil)
			request.Header.Set("Authorization", "Bearer "+tc.token)
			user, ok := server.apiTokenIdentity(request)
			if !ok || user.Role != tc.role || user.ID != tc.id {
				t.Fatalf("identity = (%+v, %v), want role %s and ID %s", user, ok, tc.role, tc.id)
			}
		})
	}
	request := httptest.NewRequest(http.MethodGet, "http://panel.example/api/v1/hosts", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
	if _, ok := server.apiTokenIdentity(request); ok {
		t.Fatal("unknown token was accepted")
	}
}

func TestAPIProtectRejectsReadTokenForWrite(t *testing.T) {
	server := &Server{cfg: config.Config{MetricsToken: strings.Repeat("r", 32), APIToken: strings.Repeat("o", 32)}}
	handler := server.protect("operator", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "http://panel.example/api/v1/hosts/host/vms", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("r", 32))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "insufficient_scope") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAPITokenCanReachWriteRoute(t *testing.T) {
	server := &Server{cfg: config.Config{APIToken: strings.Repeat("o", 32)}}
	handler := server.protect("operator", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	request := httptest.NewRequest(http.MethodPost, "http://panel.example/api/v1/hosts/host/vms", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("o", 32))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestVMCreateArgsBlankDiskIsImmediatelyRunnable(t *testing.T) {
	server := &Server{cfg: config.Config{ImageDir: t.TempDir()}}
	args, err := server.vmCreateArgs(context.Background(), "host", "web-01", 2, 4, 40, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--name web-01", "--vcpus 2", "--memory 4096", "--disk size=40,format=qcow2", "--import", "--graphics vnc,listen=127.0.0.1", "--noautoconsole"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q do not contain %q", joined, want)
		}
	}
}

func TestVMCreateArgsRejectsInvalidOSVariant(t *testing.T) {
	server := &Server{cfg: config.Config{ImageDir: t.TempDir()}}
	_, err := server.vmCreateArgs(context.Background(), "host", "web-01", 2, 4, 40, "", "", "generic;reboot")
	if err == nil {
		t.Fatal("unsafe os_variant was accepted")
	}
}

func TestDecodeAPIJSONRejectsUnknownFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://panel.example/api/v1/test", strings.NewReader(`{"name":"ok","unexpected":true}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	var body struct {
		Name string `json:"name"`
	}
	if decodeAPIJSON(recorder, request, &body) {
		t.Fatal("unknown field was accepted")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestWriteAPIErrorUsesStableResponseShape(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://panel.example/api/v1/hosts", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, "req_test"))
	recorder := httptest.NewRecorder()
	writeAPIError(recorder, request, http.StatusBadGateway, "libvirt_unavailable", "Could not query libvirt")

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	var response apiErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "libvirt_unavailable" || response.Error == "" || response.RequestID != "req_test" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestAPIVMSummaryIncludesResourceMeasurements(t *testing.T) {
	summary := apiVMSummaryFromDomain(libvirt.Domain{
		Name: "web-01", ID: "7", State: "running", Autostart: "enable",
		VCPUs: 4, MaxMemoryKiB: 8388608, UsedMemoryKiB: 2097152,
	})
	if summary.VCPUs != 4 || summary.MaxMemoryKiB != 8388608 || summary.UsedMemoryKiB != 2097152 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}
