// Package baseurl は外へ出る呼び出しの送り先の起点（BaseURL）の形を検査する。
//
// GitHub と LLM のアダプタが読む。**検査を書き写さないためにここに置く**（#277）。
// どちらも送り先を base にパスを足した文字列で作るので、形の約束が同じになる。
// 写すと、片方だけ直したときにもう片方へ同じ穴（#138）が残る。
//
// **http を許すかどうかはここで決めない。** GitHub はループバックにだけ許し、
// LLM は鍵を持つときだけループバックに限る。変わる理由が別なので、判定は
// 呼び出し側が持つ（ループバックの判定は internal/loopback）。
package baseurl

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Parse は raw を送り先の起点として読み、形を検査する。
//
// what はエラーに載せる送り先の名前（"github" / "llm"）。エラーは
// `etoki: invalid <what> base url ...` の形で、URL に埋め込まれた資格情報は
// 伏せる（Redacted）。
func Parse(what, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		// *url.Error は入力の URL をそのまま文字列に載せるので、埋め込まれた
		// 資格情報まで出る（#284）。運ぶのは中の理由だけにする。
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("etoki: invalid %s base url: %w", what, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("etoki: invalid %s base url %q: scheme must be http or https",
			what, u.Redacted())
	}
	// url.Parse は "http://" や "https:api.github.com" も通す。スキームだけ見て
	// 通すと、ホストの無い URL が起動時の検証をすり抜け、呼び出したときに
	// "no Host in request URL" として初めて失敗する。
	if u.Hostname() == "" {
		return nil, fmt.Errorf("etoki: invalid %s base url %q: host is missing", what, u.Redacted())
	}
	// クエリと fragment は弾く。送り先は base にパスを足した文字列なので、
	// "?x=1" が付いていると足したぶんが path ではなくクエリの一部として飛ぶ。
	// 黙って別の URL を叩くより、起動時に落とす。空の "?"（ForceQuery）と
	// 空の "#" も同じ（#138）。
	if u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return nil, fmt.Errorf("etoki: invalid %s base url %q: query and fragment are not allowed",
			what, u.Redacted())
	}
	return u, nil
}
