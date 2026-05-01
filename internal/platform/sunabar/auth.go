package sunabar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
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

// OAuth2Config OAuth2 クライアントクレデンシャルフローの設定。
type OAuth2Config struct {
	// TokenURL トークンエンドポイントの URL ( 本番 BaaS が提供する ) 。
	TokenURL string
	// ClientID OAuth2 クライアント ID 。
	ClientID string
	// ClientSecret OAuth2 クライアントシークレット。
	ClientSecret string
	// Scope 必要に応じて指定するスコープ。 空文字列なら省略。
	Scope string
	// HTTPClient nil ならタイムアウト 10 秒のデフォルトを使う。
	HTTPClient *http.Client
	// SkewSeconds expires_in からこの秒数を引いて早めに更新する ( 既定 30 秒 ) 。
	SkewSeconds int
}

// OAuth2TokenSource 本番 BaaS 用の OAuth2 クライアントクレデンシャルフロー実装。
// 取得したアクセストークンをキャッシュし、 期限切れ間際に再取得する。
type OAuth2TokenSource struct {
	cfg       OAuth2Config
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// 静的エラー。
var (
	// ErrOAuth2EmptyEndpoint TokenURL が空のときに返る。
	ErrOAuth2EmptyEndpoint = errors.New("sunabar OAuth2: empty token url")
	// ErrOAuth2EmptyCredentials ClientID か ClientSecret が空のときに返る。
	ErrOAuth2EmptyCredentials = errors.New("sunabar OAuth2: empty client credentials")
)

// NewOAuth2TokenSource OAuth2 トークンソースを作る。
func NewOAuth2TokenSource(cfg OAuth2Config) (*OAuth2TokenSource, error) {
	if cfg.TokenURL == "" {
		return nil, ErrOAuth2EmptyEndpoint
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, ErrOAuth2EmptyCredentials
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.SkewSeconds <= 0 {
		cfg.SkewSeconds = 30
	}
	return &OAuth2TokenSource{cfg: cfg}, nil
}

// Token キャッシュ済みトークンを返す。 期限切れか未取得なら TokenURL に POST して取得する。
func (s *OAuth2TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && time.Now().Before(s.expiresAt) {
		return s.token, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", s.cfg.ClientID)
	form.Set("client_secret", s.cfg.ClientSecret)
	if s.cfg.Scope != "" {
		form.Set("scope", s.cfg.Scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("oauth2 new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth2 do: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("oauth2 status=%d body=%s", res.StatusCode, string(body))
	}

	var resp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return "", fmt.Errorf("oauth2 decode: %w", err)
	}
	if resp.AccessToken == "" {
		return "", errors.New("oauth2 empty access_token in response")
	}
	expires := resp.ExpiresIn
	if expires <= s.cfg.SkewSeconds {
		expires = s.cfg.SkewSeconds + 1
	}
	s.token = resp.AccessToken
	s.expiresAt = time.Now().Add(time.Duration(expires-s.cfg.SkewSeconds) * time.Second)
	return s.token, nil
}
