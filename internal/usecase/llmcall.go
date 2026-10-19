package usecase

import (
	"errors"
	"fmt"

	"github.com/yusuke0610/etoki/port"
)

// LLM の呼び出しが失敗した原因のうち、打ち手が違うもの（ADR 0086）。
//
// 解釈と図のドラフト生成の両方で返る。原因が分からない失敗は
// ErrLLMUnavailable（interpretation.go）のまま。
var (
	// ErrLLMTruncated は応答が出力の上限で打ち切られたことを表す。
	//
	// 頼み方では直らない。向こうのコンテキスト長か出力の上限を広げる話で、
	// ErrInterpretationFailed / ErrDiagramFailed（出力の形が違った）とも別。
	ErrLLMTruncated = errors.New("etoki: llm output was truncated")

	// ErrLLMRefused はモデルが応答を拒んだことを表す。設定の誤りではない。
	ErrLLMRefused = errors.New("etoki: llm refused to answer")

	// ErrLLMOverloaded は上流の LLM が混んでいるか、一時的に応答できないことを表す。
	//
	// **ErrRateLimited / ErrConcurrencyLimited とは相手が違う。** あちらは etoki
	// 自身が置いた上限（ADR 0044）で、こちらは向こうが断った。畳むと、画面が
	// 「etoki の設定で決まる上限」と「向こうの混雑」を言い分けられない。
	ErrLLMOverloaded = errors.New("etoki: llm upstream is busy")
)

// llmCauses は port の sentinel を、ユースケース層の sentinel に写す表。
var llmCauses = []struct {
	port, usecase error
}{
	{port.ErrLLMTruncated, ErrLLMTruncated},
	{port.ErrLLMRefused, ErrLLMRefused},
	{port.ErrLLMOverloaded, ErrLLMOverloaded},
}

// llmCallError は llm.Complete の失敗を、ユースケース層のエラーに包み直す。
//
// 解釈と図のドラフト生成は**どちらもここを通す。** 片方だけ写すと、同じ
// 打ち切りが解釈では llm_truncated、図では llm_unavailable になる。
//
// **sentinel を入れ子にしない。** port の sentinel を名乗るなら、ユースケース層の
// sentinel だけを %w で包み、元のエラーは %v で文言だけ残す。両方を %w で包むと
// errors.Is が port の sentinel まで辿れてしまい、httpapi の表が「sentinel は
// 入れ子にならない」前提で 1 つに決めている写し先が並び順しだいになる。
//
// 名乗らなければ ErrLLMUnavailable に落とす。外部の LLMClient 実装は名乗らなくて
// よい（port の doc コメント）。こちらは元のエラーを %w のまま包む。
// context.Canceled を辿って「待っていた相手がいなくなった」を見分けるのに
// 使っている（httpapi の failLLM）。
func llmCallError(attempt int, err error) error {
	for _, c := range llmCauses {
		if errors.Is(err, c.port) {
			return fmt.Errorf("%w (attempt %d): %v", c.usecase, attempt, err) //nolint:errorlint // 入れ子にしないため %v で文言だけ残す
		}
	}
	return fmt.Errorf("%w (attempt %d): %w", ErrLLMUnavailable, attempt, err)
}
