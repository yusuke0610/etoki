package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// boardSummary は一覧で返すボード。シーンは大きいので含めない。
type boardSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// boardDetail はシーンを含むボード。
type boardDetail struct {
	boardSummary
	Scene string `json:"scene"`
}

func toSummary(b port.Board) boardSummary {
	return boardSummary{
		ID:        b.ID,
		Name:      b.Name,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
	}
}

type createBoardRequest struct {
	Name  string `json:"name"`
	Scene string `json:"scene"`
}

type saveSceneRequest struct {
	Scene string `json:"scene"`
}

// syncItemResponse は前回作成した draft issue 1 件。
type syncItemResponse struct {
	ItemID  string `json:"itemId"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	LocalID string `json:"localId"`
	Parent  string `json:"parentLocalId,omitempty"`
}

// annotationResponse は注釈 1 つの状態。
type annotationResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Granularity string `json:"granularity"`
	State       string `json:"state"`

	// LastSyncedAt と Items は前回実行の記録。未実行なら省略する。
	LastSyncedAt *time.Time         `json:"lastSyncedAt,omitempty"`
	Items        []syncItemResponse `json:"items,omitempty"`
}

// handlers はユースケース層への入口をまとめる。
type handlers struct {
	boards      *usecase.BoardService
	annotations *usecase.AnnotationService
	// interpretations は nil でもよい。その場合は 503 を返す。
	interpretations *usecase.InterpretationService
	// creations は nil でもよい。その場合は 503 を返す。
	creations *usecase.CreationService
	logger    *slog.Logger
}

func (h *handlers) createBoard(c *gin.Context) {
	var req createBoardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.badRequest(c, err)
		return
	}

	b, err := h.boards.Create(c.Request.Context(), req.Name, req.Scene)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusCreated, boardDetail{boardSummary: toSummary(b), Scene: b.Scene})
}

func (h *handlers) listBoards(c *gin.Context) {
	boards, err := h.boards.List(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}

	// nil を返すと JSON が null になる。一覧は常に配列にする。
	out := make([]boardSummary, 0, len(boards))
	for _, b := range boards {
		out = append(out, toSummary(b))
	}

	c.JSON(http.StatusOK, out)
}

func (h *handlers) getBoard(c *gin.Context) {
	b, err := h.boards.Find(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	if b == nil {
		h.notFound(c)
		return
	}

	c.JSON(http.StatusOK, boardDetail{boardSummary: toSummary(*b), Scene: b.Scene})
}

func (h *handlers) saveScene(c *gin.Context) {
	var req saveSceneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.badRequest(c, err)
		return
	}

	if err := h.boards.SaveScene(c.Request.Context(), c.Param("id"), req.Scene); err != nil {
		h.fail(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *handlers) listAnnotations(c *gin.Context) {
	states, err := h.annotations.ListStates(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	if states == nil {
		// ボードが無い場合と、注釈が 0 件の場合を区別する必要がある。
		// ListStates はボードが無いときだけ nil を返す。
		if b, findErr := h.boards.Find(c.Request.Context(), c.Param("id")); findErr != nil {
			h.fail(c, findErr)
			return
		} else if b == nil {
			h.notFound(c)
			return
		}
	}

	out := make([]annotationResponse, 0, len(states))
	for _, s := range states {
		out = append(out, toAnnotationResponse(s))
	}

	c.JSON(http.StatusOK, out)
}

func toAnnotationResponse(s usecase.AnnotationState) annotationResponse {
	res := annotationResponse{
		ID:          s.Annotation.ID,
		Name:        s.Annotation.Name,
		Granularity: string(s.Annotation.Granularity),
		State:       string(s.State),
	}

	if s.LatestRun != nil {
		syncedAt := s.LatestRun.CreatedAt
		res.LastSyncedAt = &syncedAt

		res.Items = make([]syncItemResponse, 0, len(s.LatestRun.Items))
		for _, it := range s.LatestRun.Items {
			out := syncItemResponse{
				ItemID:  it.ItemID,
				Kind:    string(it.Kind),
				Title:   it.Title,
				LocalID: it.LocalID,
			}
			if it.ParentLocalID != nil {
				out.Parent = *it.ParentLocalID
			}
			res.Items = append(res.Items, out)
		}
	}

	return res
}

// fail はユースケース層のエラーを HTTP ステータスに写す。
//
// 分岐をここ 1 箇所に閉じることで、ハンドラ側がステータスコードを
// 気にしなくて済む。
func (h *handlers) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usecase.ErrInvalidInput):
		h.badRequest(c, err)
	case errors.Is(err, port.ErrNotFound):
		h.notFound(c)
	default:
		// レスポンスには内部情報を載せないが、原因が分からないままだと
		// 手元で調べようがない。サーバー側には必ず残す。
		h.logger.ErrorContext(c.Request.Context(), "unhandled error",
			slog.String("path", c.Request.URL.Path),
			slog.Any("error", err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

func (h *handlers) badRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

func (h *handlers) notFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
}
