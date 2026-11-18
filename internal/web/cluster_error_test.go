package web

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestClusterHostLookupSeparatesNotFound(t *testing.T) {
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	request := httptest.NewRequest(http.MethodGet, "/cluster", nil)
	missing := httptest.NewRecorder()
	server.clusterHostError(missing, request, pgx.ErrNoRows)
	missingURL, _ := url.Parse(missing.Header().Get("Location"))
	if !strings.Contains(missingURL.Query().Get("msg"), "主机不存在") {
		t.Fatalf("missing host location = %s", missing.Header().Get("Location"))
	}
	internal := httptest.NewRecorder()
	server.clusterHostError(internal, request, errors.New("database connection reset"))
	internalURL, _ := url.Parse(internal.Header().Get("Location"))
	location := internalURL.Query().Get("msg")
	if !strings.Contains(location, "读取主机失败") || strings.Contains(location, "database") {
		t.Fatalf("internal location = %s", location)
	}
}
