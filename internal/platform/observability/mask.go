package observability

import "strings"

// MaskAccountNumber 口座番号をログ向けにマスクする。
// 末尾 4 桁のみ残し、 先頭は * で埋める。 5 桁未満ならすべて * 。
func MaskAccountNumber(s string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	if len(digits) <= 4 {
		return strings.Repeat("*", len(digits))
	}
	return strings.Repeat("*", len(digits)-4) + digits[len(digits)-4:]
}

// MaskAccessToken アクセストークンをログ向けに先頭 4 文字 + 末尾の長さで表現する。
// 8 文字未満なら全マスク。
func MaskAccessToken(s string) string {
	if len(s) < 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + "..." + "(" + lenStr(len(s)) + " chars)"
}

func lenStr(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}
