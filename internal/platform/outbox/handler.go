package outbox

import "context"

// Handler 配信された Event を処理する。
// 受信側冪等性は Relay が event_processed テーブルで担保するため、 Handler 自体は
// 業務処理に集中してよい ( ただし副作用が冪等でない場合は MarkProcessed を併用する ) 。
type Handler interface {
	Handle(ctx context.Context, event Event) error
}

// HandlerFunc Handler の関数アダプタ。
type HandlerFunc func(ctx context.Context, event Event) error

// Handle HandlerFunc 自身を呼び出す。
func (f HandlerFunc) Handle(ctx context.Context, event Event) error { return f(ctx, event) }
