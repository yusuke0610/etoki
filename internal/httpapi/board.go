package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 境界の DTO は api/openapi.yaml から生成した apitypes を使う。ここで型を
// 手書きすると、フロントの型との一致を人が保つことになる（ADR 0011）。

func toSummary(b port.Board) apitypes.BoardSummary {
	return apitypes.BoardSummary{
		ID:        b.ID,
		Name:      b.Name,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
	}
}

// toDetail は BoardSummary の詰め替えを繰り返している。allOf は生成後には
// 平坦な struct になり、Go の埋め込みにはならないため共有できない。
//
// targetLocked は board だけでは決まらない。run の有無で決まるので引数で受ける。
func toDetail(b port.Board, targetLocked bool) apitypes.BoardDetail {
	return apitypes.BoardDetail{
		ID:              b.ID,
		Name:            b.Name,
		CreatedAt:       b.CreatedAt,
		UpdatedAt:       b.UpdatedAt,
		Scene:           b.Scene,
		RepositoryOwner: b.Target.RepositoryOwner,
		RepositoryName:  b.Target.RepositoryName,
		ProjectID:       b.Target.ProjectID,
		TargetLocked:    targetLocked,
	}
}

// toSyncItem は保存済みの draft issue 1 件を境界の DTO に詰め替える。
// 注釈の状態と作成結果の両方で返すので 1 箇所に置く。
func toSyncItem(it port.SyncItem) apitypes.SyncItem {
	out := apitypes.SyncItem{
		ItemID:  it.ItemID,
		Kind:    apitypes.ItemKind(it.Kind),
		Title:   it.Title,
		LocalID: it.LocalID,
	}
	if it.ParentLocalID != nil {
		out.ParentLocalID = *it.ParentLocalID
	}
	return out
}

func toSyncItems(items []port.SyncItem) []apitypes.SyncItem {
	out := make([]apitypes.SyncItem, 0, len(items))
	for _, it := range items {
		out = append(out, toSyncItem(it))
	}
	return out
}

// handlers はユースケース層への入口をまとめる。
type handlers struct {
	boards      *usecase.BoardService
	annotations *usecase.AnnotationService
	// interpretations は nil でもよい。その場合は 503 を返す。
	interpretations *usecase.InterpretationService
	// creations は nil でもよい。その場合は 503 を返す。
	creations *usecase.CreationService
	// catalog は作成先の候補一覧。nil でもよい。その場合は 503 を返す。
	catalog *usecase.GitHubCatalogService
	logger  *slog.Logger
}

func (h *handlers) createBoard(c *gin.Context) {
	var req apitypes.CreateBoardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.badRequest(c, err)
		return
	}

	b, err := h.boards.Create(c.Request.Context(), req.Name, req.Scene)
	if err != nil {
		h.fail(c, err)
		return
	}

	// 作ったばかりのボードに run はありえないので、照会せず false でよい。
	c.JSON(http.StatusCreated, toDetail(b, false))
}

func (h *handlers) listBoards(c *gin.Context) {
	boards, err := h.boards.List(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}

	// nil を返すと JSON が null になる。一覧は常に配列にする。
	out := make([]apitypes.BoardSummary, 0, len(boards))
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

	h.respondBoard(c, http.StatusOK, *b)
}

// setBoardTarget は draft issue の作成先をボードに設定する。
//
// 固定済みかどうかの判断はユースケース層が持つ。ここは 409 に写すだけ。
func (h *handlers) setBoardTarget(c *gin.Context) {
	var req apitypes.BoardTarget
	if err := c.ShouldBindJSON(&req); err != nil {
		h.badRequest(c, err)
		return
	}

	id := c.Param("id")
	target := port.BoardTarget{
		RepositoryOwner: req.RepositoryOwner,
		RepositoryName:  req.RepositoryName,
		ProjectID:       req.ProjectID,
	}

	if err := h.boards.SetTarget(c.Request.Context(), id, target); err != nil {
		h.fail(c, err)
		return
	}

	// 設定後の姿を返す。フロントが手元の値を組み立て直さずに済む。
	b, err := h.boards.Find(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	if b == nil {
		// SetTarget を通った直後なので、消えているのは異常事態。
		h.notFound(c)
		return
	}

	h.respondBoard(c, http.StatusOK, *b)
}

// respondBoard はボードを固定状態つきで返す。
func (h *handlers) respondBoard(c *gin.Context, status int, b port.Board) {
	locked, err := h.boards.TargetLocked(c.Request.Context(), b.ID)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(status, toDetail(b, locked))
}

func (h *handlers) saveScene(c *gin.Context) {
	var req apitypes.SaveSceneRequest
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

	out := make([]apitypes.AnnotationStatus, 0, len(states))
	for _, s := range states {
		out = append(out, toAnnotationStatus(s))
	}

	c.JSON(http.StatusOK, out)
}

func toAnnotationStatus(s usecase.AnnotationState) apitypes.AnnotationStatus {
	res := apitypes.AnnotationStatus{
		ID:          s.Annotation.ID,
		Name:        s.Annotation.Name,
		Granularity: apitypes.Granularity(s.Annotation.Granularity),
		State:       apitypes.SyncState(s.State),
	}

	if s.LatestRun != nil {
		syncedAt := s.LatestRun.CreatedAt
		res.LastSyncedAt = &syncedAt
		res.Items = toSyncItems(s.LatestRun.Items)
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
	case errors.Is(err, usecase.ErrTargetLocked):
		// 状態が食い違っていて進めない、という点で contentHash の不一致と同類。
		errorJSON(c, http.StatusConflict, err.Error())
	default:
		// レスポンスには内部情報を載せないが、原因が分からないままだと
		// 手元で調べようがない。サーバー側には必ず残す。
		h.logger.ErrorContext(c.Request.Context(), "unhandled error",
			slog.String("path", c.Request.URL.Path),
			slog.Any("error", err),
		)
		errorJSON(c, http.StatusInternalServerError, "internal error")
	}
}

func (h *handlers) badRequest(c *gin.Context, err error) {
	errorJSON(c, http.StatusBadRequest, err.Error())
}

func (h *handlers) notFound(c *gin.Context) {
	errorJSON(c, http.StatusNotFound, "not found")
}

// errorJSON はエラー本文を 1 つの型に揃える。gin.H を直に書くと、契約に
// 無いキーが混ざっても気づけない。
func errorJSON(c *gin.Context, status int, message string) {
	c.JSON(status, apitypes.ErrorResponse{Error: message})
}
