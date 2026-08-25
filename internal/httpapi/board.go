package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 境界の DTO は api/openapi.yaml から生成した apitypes を使う。ここで型を
// 手書きすると、フロントの型との一致を人が保つことになる（ADR 0011）。

// role は操作者ごとに変わるので、ボードと一緒に受け取る。画面はこれで
// 出し入れを分ける（ADR 0017）。
func toSummary(a port.BoardAccess) apitypes.BoardSummary {
	return apitypes.BoardSummary{
		ID:              a.Board.ID,
		Name:            a.Board.Name,
		Role:            apitypes.BoardRole(a.Role),
		CreatedAt:       a.Board.CreatedAt,
		UpdatedAt:       a.Board.UpdatedAt,
		RepositoryOwner: a.Board.Target.RepositoryOwner,
		RepositoryName:  a.Board.Target.RepositoryName,
		ProjectID:       a.Board.Target.ProjectID,
		ProjectNumber:   a.Board.Target.ProjectNumber,
		ProjectTitle:    a.Board.Target.ProjectTitle,
		ProjectURL:      a.Board.Target.ProjectURL,
	}
}

// toDetail は BoardSummary の詰め替えを繰り返している。allOf は生成後には
// 平坦な struct になり、Go の埋め込みにはならないため共有できない。
//
// targetLocked は board だけでは決まらない。run の有無で決まるので引数で受ける。
func toDetail(a port.BoardAccess, targetLocked bool) apitypes.BoardDetail {
	return apitypes.BoardDetail{
		ID:              a.Board.ID,
		Name:            a.Board.Name,
		Role:            apitypes.BoardRole(a.Role),
		CreatedAt:       a.Board.CreatedAt,
		UpdatedAt:       a.Board.UpdatedAt,
		Scene:           a.Board.Scene,
		RepositoryOwner: a.Board.Target.RepositoryOwner,
		RepositoryName:  a.Board.Target.RepositoryName,
		ProjectID:       a.Board.Target.ProjectID,
		ProjectNumber:   a.Board.Target.ProjectNumber,
		ProjectTitle:    a.Board.Target.ProjectTitle,
		ProjectURL:      a.Board.Target.ProjectURL,
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
		Body:    it.Body,
		LocalID: it.LocalID,
		Action:  apitypes.SyncAction(it.Action),
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
	// members はボードの共有。nil でもよい。その場合は 503 を返す。
	members *usecase.BoardMemberService
	// access はそのボードで何ができるか。GitHub が未設定でも組み立てる。
	//
	// GitHub 側を確かめられないことは「分からない」として返るので、ここを
	// nil にする理由が無い（ADR 0017）。
	access *usecase.BoardAccessService
	// auth はログインとセッション。nil なら認証しない。
	//
	// nil のときは /api/auth/session が authRequired: false を返し、画面は
	// ログインを求めない（ADR 0015）。
	auth *usecase.AuthService
	// publicURL は認可から戻ってくる先の組み立てに使う。空ならリクエストの
	// Host から組む。
	publicURL string
	logger    *slog.Logger
}

func (h *handlers) createBoard(c *gin.Context) {
	var req apitypes.CreateBoardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.badRequest(c, err)
		return
	}

	b, err := h.boards.Create(c.Request.Context(), req.Name, req.Scene, port.BoardTarget{
		RepositoryOwner: req.RepositoryOwner,
		RepositoryName:  req.RepositoryName,
		ProjectID:       req.ProjectID,
		ProjectNumber:   req.ProjectNumber,
		ProjectTitle:    req.ProjectTitle,
		ProjectURL:      req.ProjectURL,
	})
	if err != nil {
		h.fail(c, err)
		return
	}

	// 作ったばかりのボードに run はありえないので、照会せず false でよい。
	// 作った本人は必ず owner（BoardService.Create）。
	c.JSON(http.StatusCreated,
		toDetail(port.BoardAccess{Board: b, Role: port.RoleOwner}, false))
}

func (h *handlers) listBoards(c *gin.Context) {
	boards, err := h.boards.List(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}

	// nil を返すと JSON が null になる。一覧は常に配列にする。
	out := make([]apitypes.BoardSummary, 0, len(boards))
	for _, a := range boards {
		out = append(out, toSummary(a))
	}

	c.JSON(http.StatusOK, out)
}

func (h *handlers) getBoard(c *gin.Context) {
	b, err := h.boards.Find(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
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
		ProjectNumber:   req.ProjectNumber,
		ProjectTitle:    req.ProjectTitle,
		ProjectURL:      req.ProjectURL,
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

	h.respondBoard(c, http.StatusOK, *b)
}

// refreshBoardTargetDisplay は作成先の表示用スナップショットだけを取り直す。
//
// **固定済みでも通る。** 固定するのは作成先そのものであって、表示用の値では
// ない（ADR 0037）。同じかどうかの判断はユースケース層が持つ。
func (h *handlers) refreshBoardTargetDisplay(c *gin.Context) {
	var req apitypes.BoardTargetDisplay
	if err := c.ShouldBindJSON(&req); err != nil {
		h.badRequest(c, err)
		return
	}

	id := c.Param("id")
	display := port.BoardTargetDisplay{
		ProjectNumber: req.ProjectNumber,
		ProjectTitle:  req.ProjectTitle,
		ProjectURL:    req.ProjectURL,
	}

	if err := h.boards.RefreshTargetDisplay(c.Request.Context(), id, req.ProjectID, display); err != nil {
		h.fail(c, err)
		return
	}

	// 更新後の姿を返す。フロントが手元の値を組み立て直さずに済む。
	b, err := h.boards.Find(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}

	h.respondBoard(c, http.StatusOK, *b)
}

// respondBoard はボードを固定状態つきで返す。
func (h *handlers) respondBoard(c *gin.Context, status int, a port.BoardAccess) {
	locked, err := h.boards.TargetLocked(c.Request.Context(), a.Board.ID)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(status, toDetail(a, locked))
}

func (h *handlers) saveScene(c *gin.Context) {
	var req apitypes.SaveSceneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.badRequest(c, err)
		return
	}

	updatedAt, err := h.boards.SaveScene(
		c.Request.Context(), c.Param("id"), req.Scene, req.BaseUpdatedAt)
	if err != nil {
		h.fail(c, err)
		return
	}

	// 保存後の版を返す。返さないと、クライアントは次の保存の基準を得るために
	// 毎回ボードを取り直すことになり、シーンまで運ぶ（ADR 0020）。
	c.JSON(http.StatusOK, apitypes.SaveSceneResponse{UpdatedAt: updatedAt})
}

func (h *handlers) listAnnotations(c *gin.Context) {
	states, err := h.annotations.ListStates(c.Request.Context(), c.Param("id"))
	if err != nil {
		// ボードが無い場合と注釈が 0 件の場合は、ここで区別がついている。
		// ListStates が引き当てられなければエラーを返すため、ボードを引き直す
		// 必要が無くなった。
		h.fail(c, err)
		return
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
	}
	// 中身は最新 run ではなく畳み込みから出す（ADR 0026）。0 件なら省く。
	if len(s.Items) > 0 {
		res.Items = toSyncItems(s.Items)
	}

	return res
}

// fail はユースケース層のエラーを契約の code と HTTP ステータスに写す。
//
// 写し替えの表は errors.go に 1 つだけ置いてある。ここが決めるのは、表に無かった
// ときにどこへ落とすかだけ。
func (h *handlers) fail(c *gin.Context, err error) {
	if respondMapped(c, err) {
		return
	}

	// レスポンスには内部情報を載せないが、原因が分からないままだと
	// 手元で調べようがない。サーバー側には必ず残す。
	h.logger.ErrorContext(c.Request.Context(), "unhandled error",
		slog.String("path", c.Request.URL.Path),
		slog.Any("error", err),
	)
	errorJSON(c, http.StatusInternalServerError, apitypes.ErrorCodeInternal, "internal error")
}

// badRequest はリクエストの読み取りに失敗したことを返す。
//
// ボディのバインドはユースケース層に届く前に落ちるので、sentinel を持たない。
// 表を引かずにここで code を決めるのはこの経路だけ。
func (h *handlers) badRequest(c *gin.Context, err error) {
	errorJSON(c, http.StatusBadRequest, apitypes.ErrorCodeInvalidInput, err.Error())
}
