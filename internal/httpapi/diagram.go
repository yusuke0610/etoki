package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
)

// generateDiagramDraft は図のドラフトの生成を始めて、ジョブを返す。LLM の応答は
// 待たない。
//
// **会話は保存しない**（ADR 0041）。DB にも GitHub にも触れない。キャンバスに
// 置くかどうかは結果を見た開発者が決める（中核思想 3）。
//
// **生成をリクエストに結びつけない**（ADR 0091）。結びつけると、生成中に画面を
// 離れたあともサーバーは走り続けて枠を握り、戻って押し直すと 429 に弾かれる。
func (h *handlers) generateDiagramDraft(c *gin.Context) {
	if h.diagramJobs == nil {
		h.llmNotConfigured(c)
		return
	}

	req, ok := h.bindDiagramRequest(c)
	if !ok {
		return
	}

	job, err := h.diagramJobs.Start(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusAccepted, toDiagramJobResponse(job, false))
}

// listDiagramJobs は自分の図のドラフト生成のジョブを返す。結果は含めない。
func (h *handlers) listDiagramJobs(c *gin.Context) {
	if h.diagramJobs == nil {
		h.llmNotConfigured(c)
		return
	}
	listJobs(h, c, h.diagramJobs, toDiagramJobResponse)
}

// getDiagramJob はジョブを 1 件返す。読んでも消えない。
func (h *handlers) getDiagramJob(c *gin.Context) {
	if h.diagramJobs == nil {
		h.llmNotConfigured(c)
		return
	}
	getJob(h, c, h.diagramJobs, toDiagramJobResponse)
}

// discardDiagramJob はジョブを捨てる。走っていれば止める。
func (h *handlers) discardDiagramJob(c *gin.Context) {
	if h.diagramJobs == nil {
		h.llmNotConfigured(c)
		return
	}
	discardJob(h, c, h.diagramJobs)
}

// toDiagramJobResponse はジョブを境界の DTO に詰め替える。
//
// 失敗の理由は解釈のジョブと同じ写し方（jobErrorResponse）。withResult が false
// なら結果は載せない（一覧）。**入力は常に載せる。** 開き直した画面が会話を
// 戻すのに一覧から読む。
func toDiagramJobResponse(job usecase.DiagramJob, withResult bool) apitypes.DiagramJob {
	out := apitypes.DiagramJob{
		ID:        job.ID,
		Status:    apitypes.DiagramJobStatus(job.Status),
		StartedAt: job.StartedAt,
		Request:   toDiagramRequestResponse(job.Input),
	}

	if withResult && job.Result != nil {
		out.Result = &apitypes.DiagramDraft{
			Kind:           apitypes.DiagramKind(job.Result.Kind),
			Mermaid:        job.Result.Mermaid,
			TurnsRemaining: job.Result.TurnsRemaining,
		}
	}
	if job.Err != nil {
		out.Error = jobErrorResponse(job.Err)
	}
	return out
}

// toDiagramRequestResponse は受け取った入力を、送られてきた形に戻す。
func toDiagramRequestResponse(req usecase.DiagramRequest) apitypes.GenerateDiagramRequest {
	// nil を返すと JSON が null になる。往復が無くても空配列にする。
	history := make([]apitypes.DiagramTurn, 0, len(req.History))
	for _, t := range req.History {
		history = append(history, apitypes.DiagramTurn{
			Prompt: t.Prompt, Mermaid: t.Mermaid, Internal: t.Internal,
		})
	}

	return apitypes.GenerateDiagramRequest{
		Kind:     apitypes.DiagramKind(req.Kind),
		Prompt:   req.Prompt,
		History:  &history,
		Internal: req.Internal,
	}
}

// maxDiagramBody は生成のリクエストボディを読む上限。
//
// **判定の正本はユースケース層**（usecase.MaxDiagramChatBytes）で、ここは
// 読み込みの歯止め。JSON の文字列は 1 バイトが `\u00XX` で 6 バイトになりうる
// ので、その最悪値で割り増して置く。2 倍で取ると、上限ちょうどの会話が歯止め
// 側で落ち、減らす必要のないものを減らせと言うことになる（ADR 0038 の
// maxSceneBody と同じ形）。
const maxDiagramBody = usecase.MaxDiagramChatBytes*6 + 4<<10

// bindDiagramRequest はリクエストボディを生成の入力に詰め替える。
//
// **値の検証はしない。** 種類も長さもユースケース層が見る。ハンドラにも置くと、
// API を通らない経路で抜ける（判定は 1 箇所）。
//
// 歯止めに引っかかったボディだけは別で、400 ではなく 413 に写す。**同じ
// 「積み上がりすぎた会話」が、ボディの大きさしだいで 400 と 413 に割れない
// ようにする**（failBind）。写し替えの表は errors.go にあるので、
// ここは sentinel を選ぶだけ。
func (h *handlers) bindDiagramRequest(c *gin.Context) (usecase.DiagramRequest, bool) {
	widenBody(c, maxDiagramBody)

	var body apitypes.GenerateDiagramRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		h.failBind(c, err, usecase.ErrDiagramChatTooLong)
		return usecase.DiagramRequest{}, false
	}

	req := usecase.DiagramRequest{
		Kind:     domain.DiagramKind(body.Kind),
		Prompt:   body.Prompt,
		Internal: body.Internal,
	}
	if body.History != nil {
		req.History = make([]usecase.DiagramTurn, 0, len(*body.History))
		for _, t := range *body.History {
			req.History = append(req.History, usecase.DiagramTurn{
				Prompt: t.Prompt, Mermaid: t.Mermaid, Internal: t.Internal,
			})
		}
	}

	return req, true
}
