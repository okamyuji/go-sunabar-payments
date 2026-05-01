package sunabar_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-sunabar-payments/internal/platform/sunabar"
)

func TestNewStaticTokenSource_RejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := sunabar.NewStaticTokenSource("")
	if !errors.Is(err, sunabar.ErrEmptyToken) {
		t.Errorf("err = %v, want ErrEmptyToken", err)
	}
}

func TestStaticTokenSource_ReturnsToken(t *testing.T) {
	t.Parallel()
	src, err := sunabar.NewStaticTokenSource("xyz")
	if err != nil {
		t.Fatalf("NewStaticTokenSource: %v", err)
	}
	got, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != "xyz" {
		t.Errorf("Token = %q, want %q", got, "xyz")
	}
}

func TestNewOAuth2TokenSource_RejectsEmptyConfig(t *testing.T) {
	t.Parallel()
	if _, err := sunabar.NewOAuth2TokenSource(sunabar.OAuth2Config{}); !errors.Is(err, sunabar.ErrOAuth2EmptyEndpoint) {
		t.Errorf("err = %v, want ErrOAuth2EmptyEndpoint", err)
	}
	if _, err := sunabar.NewOAuth2TokenSource(sunabar.OAuth2Config{TokenURL: "http://x"}); !errors.Is(err, sunabar.ErrOAuth2EmptyCredentials) {
		t.Errorf("err = %v, want ErrOAuth2EmptyCredentials", err)
	}
}

func TestOAuth2TokenSource_FetchesAndCachesToken(t *testing.T) {
	t.Parallel()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_in":120,"token_type":"Bearer"}`))
	}))
	t.Cleanup(srv.Close)

	src, err := sunabar.NewOAuth2TokenSource(sunabar.OAuth2Config{
		TokenURL: srv.URL, ClientID: "id", ClientSecret: "sec",
	})
	if err != nil {
		t.Fatalf("NewOAuth2TokenSource: %v", err)
	}
	got, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != "tok-1" {
		t.Errorf("token = %q, want tok-1", got)
	}
	// 2 回目はキャッシュから ( HTTP 呼ばれない ) 。
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("Token cached: %v", err)
	}
	if calls != 1 {
		t.Errorf("HTTP calls = %d, want 1 ( キャッシュ ) ", calls)
	}
}

func TestOAuth2TokenSource_ServerErrorPropagated(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	t.Cleanup(srv.Close)

	src, err := sunabar.NewOAuth2TokenSource(sunabar.OAuth2Config{
		TokenURL: srv.URL, ClientID: "id", ClientSecret: "sec",
	})
	if err != nil {
		t.Fatalf("NewOAuth2TokenSource: %v", err)
	}
	_, err = src.Token(context.Background())
	if err == nil {
		t.Errorf("err = nil, want non-nil")
	}
}
