package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yusuke0610/etoki"
	githubauth "github.com/yusuke0610/etoki/internal/adapter/auth/github"
	"github.com/yusuke0610/etoki/internal/adapter/github"
	"github.com/yusuke0610/etoki/internal/adapter/llm"
)

// このファイルは環境変数を読むものを置く。名前を知っているのは配線だけで
// （ADR 0083）、usage と README の表は readme_test.go が結んでいる。

// envEncryptionKey は保存するトークンを暗号化する鍵。
const envEncryptionKey = "ETOKI_TOKEN_ENCRYPTION_KEY"

// envPublicURL は認可から戻ってくる先の組み立てに使う。
const envPublicURL = "ETOKI_PUBLIC_URL"

// envWebDir はビルド済みフロントエンドの置き場所。未設定なら画面を配らない。
const envWebDir = "ETOKI_WEB_DIR"

// envAllowedOrigins はループバック以外に追加で許すオリジン（カンマ区切り）。
const envAllowedOrigins = "ETOKI_ALLOWED_ORIGINS"

// defaultDBPath は ETOKI_DB_PATH が未設定のときに使う SQLite ファイル。
const defaultDBPath = "etoki.db"

func dbPath() string {
	if p := os.Getenv("ETOKI_DB_PATH"); p != "" {
		return p
	}
	return defaultDBPath
}

// llmLimits は環境変数から実行の上限を組み立てる（ADR 0044）。
//
// **未設定と 0 を区別する。** 未設定は「既定のまま」、0 は設定の誤りとして
// 落とす。0 を無制限と読ませると、未設定（既定 1）と 0（無制限）で意味が
// 逆向きになる。矛盾の判定そのものは etoki.New が持つので、ここは読み取りだけ。
func llmLimits() (etoki.LLMLimits, error) {
	maxConcurrent, err := positiveInt("ETOKI_LLM_MAX_CONCURRENT")
	if err != nil {
		return etoki.LLMLimits{}, err
	}
	rateLimit, err := positiveInt("ETOKI_LLM_RATE_LIMIT")
	if err != nil {
		return etoki.LLMLimits{}, err
	}

	var window time.Duration
	if raw := os.Getenv("ETOKI_LLM_RATE_WINDOW"); raw != "" {
		window, err = time.ParseDuration(raw)
		if err != nil {
			return etoki.LLMLimits{}, fmt.Errorf("ETOKI_LLM_RATE_WINDOW: %w", err)
		}
		if window <= 0 {
			return etoki.LLMLimits{}, fmt.Errorf(
				"ETOKI_LLM_RATE_WINDOW must be positive, got %q", raw)
		}
	}

	return etoki.LLMLimits{
		MaxConcurrent: maxConcurrent,
		RateLimit:     rateLimit,
		RateWindow:    window,
	}, nil
}

// positiveInt は 1 以上の整数として環境変数を読む。未設定なら 0。
//
// **読めない値を既定に倒さない。** 倒すと、設定したつもりの上限が黙って外れる
// （中核思想 3）。
func positiveInt(name string) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s must be at least 1, got %q", name, raw)
	}
	return n, nil
}

// setupHints は「設定されていない」ときに添える案内を、各アダプタが読む
// 環境変数の名前から組む（ADR 0083）。
//
// **名前を知っているのはここだけにする。** サーバーの本文にも画面の文言にも
// 書き写さない。写すと名前を変えた日に、存在しない変数を案内し続ける。
func setupHints() etoki.SetupHints {
	return etoki.SetupHints{
		// どちらかがあれば組み立てる（newLLMClient）。
		LLM:            fmt.Sprintf("set %s or %s", llm.EnvAPIKey, llm.EnvBaseURL),
		GitHub:         "set " + github.EnvToken,
		Auth:           fmt.Sprintf("set %s and %s", githubauth.EnvClientID, githubauth.EnvClientSecret),
		AllowedOrigins: fmt.Sprintf("set %s if etoki is bound beyond loopback", envAllowedOrigins),
	}
}

// splitList はカンマ区切りの環境変数を要素に分ける。空要素は捨てる。
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}
