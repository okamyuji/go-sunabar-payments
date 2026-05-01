package sunabar_test

import (
	"context"
	"errors"
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

func TestOAuth2TokenSource_NotImplemented(t *testing.T) {
	t.Parallel()
	src := &sunabar.OAuth2TokenSource{}
	_, err := src.Token(context.Background())
	if !errors.Is(err, sunabar.ErrOAuth2NotImplemented) {
		t.Errorf("err = %v, want ErrOAuth2NotImplemented", err)
	}
}
