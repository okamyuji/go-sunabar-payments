package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger 環境変数 APP_LOG_LEVEL に従って log/slog の Logger を構築する。
// 出力は JSON 構造化ログ。 stdout に書く ( コンテナ標準 ) 。
func NewLogger(w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	level := levelFromEnv(os.Getenv("APP_LOG_LEVEL"))
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}

// LoggerFromContext context に積まれた相関 ID を持つ Logger を返す。
// 相関 ID 未設定時は base ロガーをそのまま返す。
func LoggerFromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	if base == nil {
		base = slog.Default()
	}
	id := CorrelationIDFromContext(ctx)
	if id == "" {
		return base
	}
	return base.With("correlation_id", id)
}

func levelFromEnv(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return slog.LevelInfo
	}
	return slog.LevelInfo
}
