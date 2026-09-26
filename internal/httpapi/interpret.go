package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// interpretAnnotation は注釈を LLM に解釈させて結果を返す。
//
// このエンドポイントは GitHub に何も作らず、sync_runs にも書かない。何を作るかは
// 結果を見た開発者が別途トリガーする（中核思想 3）。
func (h *handlers) interpretAnnotation(c *gin.Context) {
	if h.interpretations == nil {
		llmNotConfigured(c)
		return
	}

	images, ok := h.bindInterpretImages(c)
	if !ok {
		return
	}

	in, err := h.interpretations.Interpret(
		c.Request.Context(), c.Param("id"), c.Param("annotationId"), images)
	if err != nil {
		// 出力を弾いたのは、上限まで再送してもスキーマを満たさなかったとき。
		h.failLLM(c, err, usecase.ErrInterpretationFailed)
		return
	}

	c.JSON(http.StatusOK, toInterpretationResponse(in))
}

// maxInterpretBody は解釈のリクエストボディを読む上限。
//
// 画像そのものの上限はユースケース層が持つ（usecase.MaxImageBytes）。ここで
// 重ねて持つのは、上限を超えたボディを全部メモリに載せてからでないと判定できない
// のを避けるため。base64 は 4/3 に膨らむので、その分と JSON の包みを足して余裕を
// 取る。判定の正本はあくまでユースケース層側で、ここは読み込みの歯止めである。
const maxInterpretBody = usecase.MaxImageBytes*usecase.MaxImages*4/3 + 4<<10

// bindInterpretImages はリクエストボディから LLM に渡す画像を取り出す。
//
// ボディごと省略できる（ADR 0018）。省略されたときは画像なしで解釈する。
func (h *handlers) bindInterpretImages(c *gin.Context) ([]port.Image, bool) {
	widenBody(c, maxInterpretBody)

	var req apitypes.InterpretRequest
	// 空のボディは「画像なし」であってエラーではない。JSON デコーダは
	// その場合に io.EOF を返す。
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		// 歯止めに当たった失敗も契約の code に写す
		// （`.claude/rules/api-contract.md`）。写さないと、同じ「大きすぎる」が
		// ボディの大きさしだいで 400 と 413 に割れる。
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.fail(c, fmt.Errorf("%w: request body exceeds %d bytes",
				errRequestTooLarge, maxInterpretBody))
			return nil, false
		}

		h.badRequest(c, err)
		return nil, false
	}

	if req.Image == nil {
		return nil, true
	}

	// バイト数と形式はユースケース層が見る。ここは詰め替えるだけ。判定を
	// ハンドラにも置くと、API を通らない経路で抜ける。
	return []port.Image{{
		MediaType: string(req.Image.MediaType),
		Data:      req.Image.Data,
	}}, true
}

// toInterpretationResponse はドメインの解釈結果を境界の DTO に詰め替える。
func toInterpretationResponse(result usecase.InterpretationResult) apitypes.Interpretation {
	out := apitypes.Interpretation{
		Summary:     result.Interpretation.Summary,
		ContentHash: result.ContentHash,
		// nil を返すと JSON が null になる。常に配列にする。
		Items: make([]apitypes.InterpretedItem, 0, len(result.Interpretation.Items)),
	}

	for _, it := range result.Interpretation.Items {
		item := apitypes.InterpretedItem{
			LocalID: it.LocalID,
			Kind:    apitypes.ItemKind(it.Kind),
			Title:   it.Title,
			Body:    it.Body,
		}
		if it.ParentLocalID != nil {
			item.ParentLocalID = *it.ParentLocalID
		}
		// LLM が対応づけた候補。決めるのは開発者で、確認画面で外せる
		// （中核思想 3、ADR 0026）。
		if it.PreviousItemID != nil {
			item.PreviousItemID = *it.PreviousItemID
		}
		out.Items = append(out.Items, item)
	}

	return out
}
