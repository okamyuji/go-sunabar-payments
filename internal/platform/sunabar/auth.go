package sunabar

import (
	"context"
	"errors"
)

// AuthSource API 呼び出し時のアクセストークンを返す。
// sunabar ( サンドボックス ) では取得済みの長期トークンを直接返す Static を使い、
// 本番 BaaS では OAuth2.0 クライアントクレデンシャルで取得・更新する実装を使う。
type AuthSource interface {
	Token(ctx context.Context) (string, error)
}

// StaticTokenSource 事前取得済みのアクセストークンを保持する AuthSource 。
// sunabar のサービスサイトで取得したトークンを環境変数から渡す前提。
type StaticTokenSource struct {
	token string
}

// ErrEmptyToken 空文字列のトークンを渡されたときに返るエラー。
var ErrEmptyToken = errors.New("sunabar empty token")

// NewStaticTokenSource 静的トークンソースを生成する。 token が空なら ErrEmptyToken を返す。
func NewStaticTokenSource(token string) (*StaticTokenSource, error) {
	if token == "" {
		return nil, ErrEmptyToken
	}
	return &StaticTokenSource{token: token}, nil
}

// Token 保持中のアクセストークンを返す。
func (s *StaticTokenSource) Token(_ context.Context) (string, error) {
	return s.token, nil
}

// OAuth2TokenSource 本番 BaaS 用の OAuth2.0 クライアントクレデンシャルフロー実装のプレースホルダ。
// M3 では骨組みのみで M9 ( 実機検証フェーズ ) で TokenEndpoint からの取得処理を書く。
type OAuth2TokenSource struct{}

// ErrOAuth2NotImplemented OAuth2TokenSource が未実装であることを示すセンチネルエラー。
var ErrOAuth2NotImplemented = errors.New("sunabar OAuth2TokenSource not implemented")

// Token 未実装エラーを返す。
func (s *OAuth2TokenSource) Token(_ context.Context) (string, error) {
	return "", ErrOAuth2NotImplemented
}
