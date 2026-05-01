package observability_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-sunabar-payments/internal/platform/observability"
)

func TestHTTPMiddleware_GeneratesIDWhenAbsent(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw := observability.HTTPMiddleware(logger)

	var seenID string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenID = observability.CorrelationIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(rec, req)

	if seenID == "" {
		t.Errorf("handler に相関 ID が伝搬していない")
	}
	if got := rec.Header().Get(observability.CorrelationIDHeader); got == "" {
		t.Errorf("レスポンスヘッダに相関 ID が無い")
	}
}

func TestHTTPMiddleware_PreservesIncomingID(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw := observability.HTTPMiddleware(logger)

	var seen string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = observability.CorrelationIDFromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(observability.CorrelationIDHeader, "incoming-id-1")
	h.ServeHTTP(rec, req)

	if seen != "incoming-id-1" {
		t.Errorf("incoming ID 不採用: %q", seen)
	}
}

func TestHTTPMiddleware_LogsStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	mw := observability.HTTPMiddleware(logger)

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(rec, req)

	out := buf.String()
	if !strings.Contains(out, "status=418") {
		t.Errorf("status ログが無い: %s", out)
	}
	if !strings.Contains(out, "correlation_id=") {
		t.Errorf("correlation_id ログが無い: %s", out)
	}
}
