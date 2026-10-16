package baseurl_test

import (
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/baseurl"
)

func TestParse_Rejects(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"スキームが無い":   "api.example.test",
		"対応しないスキーム": "ftp://example.test",
		"壊れた URL":   "http://[::1",
		// スキームは通るがホストが無い。呼び出し時まで失敗が遅れる。
		"ホストが無い":     "http://",
		"スラッシュが足りない": "https:api.example.test",
		// 送り先は base にパスを足した文字列なので、足したぶんが path に入らない。
		"クエリつき":       "https://api.example.test?x=1",
		"空のクエリ":       "https://api.example.test?",
		"fragment つき": "https://api.example.test#x",
		"空の fragment": "https://api.example.test#",
		"パスのあとのクエリ":   "https://api.example.test/api/v3?x=1",
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := baseurl.Parse("github", raw)
			if err == nil {
				t.Fatalf("Parse(%q) = nil, want error", raw)
			}
			// どの送り先の設定が誤っているかを名乗る。
			if !strings.HasPrefix(err.Error(), "etoki: invalid github base url") {
				t.Errorf("error = %q, want to name the github base url", err)
			}
		})
	}
}

// http を許すかどうかはここで決めない。外への http も形としては通す。
func TestParse_Accepts(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://api.example.test",
		"https://api.example.test/api/v3/",
		"http://api.example.test",
		"http://127.0.0.1:8090",
		"http://[::1]:8090",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			u, err := baseurl.Parse("llm", raw)
			if err != nil {
				t.Fatalf("Parse(%q) = %v, want nil", raw, err)
			}
			if u.Hostname() == "" {
				t.Errorf("Parse(%q).Hostname() が空", raw)
			}
		})
	}
}

// URL に埋め込まれた資格情報をエラーに載せない。
func TestParse_RedactsCredentials(t *testing.T) {
	t.Parallel()

	_, err := baseurl.Parse("llm", "https://user:secret@api.example.test?x=1")
	if err == nil {
		t.Fatal("Parse() = nil, want error")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error leaks the password: %v", err)
	}
}
