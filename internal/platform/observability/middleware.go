package observability

import (
	"log/slog"
	"net/http"
	"time"
)

// HTTPMiddleware 相関 ID の取り出し / 生成、 リクエストログ、 ステータス記録を行う共通ミドルウェア。
// 標準 net/http の middleware パターン ( http.Handler を受けて http.Handler を返す ) で実装する。
func HTTPMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			id := r.Header.Get(CorrelationIDHeader)
			ctx := WithCorrelationID(r.Context(), id)
			r = r.WithContext(ctx)
			w.Header().Set(CorrelationIDHeader, CorrelationIDFromContext(ctx))

			rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			LoggerFromContext(ctx, logger).Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// responseRecorder ステータスコードを記録するための ResponseWriter ラッパ。
type responseRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader 記録してから委譲する。
func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
