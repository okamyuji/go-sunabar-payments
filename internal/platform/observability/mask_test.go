package observability_test

import (
	"strings"
	"testing"

	"go-sunabar-payments/internal/platform/observability"
)

func TestMaskAccountNumber(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"1234567", "***4567"},
		{"1234", "****"},
		{"123", "***"},
		{"", ""},
		{"001-1234567", "******4567"}, // 区切り文字除去後 10 桁、 末尾 4 桁残す
	}
	for _, c := range cases {
		if got := observability.MaskAccountNumber(c.in); got != c.want {
			t.Errorf("Mask(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMaskAccessToken(t *testing.T) {
	t.Parallel()
	long := "abcdefghijklmnop"
	got := observability.MaskAccessToken(long)
	if !strings.HasPrefix(got, "abcd") {
		t.Errorf("先頭 4 文字残らず = %q", got)
	}
	if !strings.Contains(got, "16 chars") {
		t.Errorf("文字数表示が無い = %q", got)
	}
	short := observability.MaskAccessToken("abc")
	if short != "***" {
		t.Errorf("短いトークンの全マスク失敗 = %q", short)
	}
}
