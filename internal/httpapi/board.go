package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/domain"
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

// toDetail は共通部分を toSummary から受け取り、詳細だけを足す。
//
// allOf は生成後に平坦な struct になり Go の埋め込みにはならないので、
// **型としては共有できない。** それでも port.BoardAccess から DTO への
// 詰め替えを 2 度書くのはやめる。契約にフィールドが 1 つ増えたとき、
// 詰め替えが 2 箇所にあると片方だけ足し忘れることができ、しかも生成型なので
// コンパイルは通る。落ちるのは実行時に BoardDetail だけがゼロ値を返す形。
//
// **フィールドの写し漏れは TestToDetail_CarriesEverySummaryField が落とす。**
// 名前で突き合わせているので、契約に足したフィールドを toSummary にだけ
// 足しても、toDetail にだけ足しても落ちる。
//
// targetLocked は board だけでは決まらない。run の有無で決まるので引数で受ける。
// overLimit も同じで、貼った画像の大きさはシーンの外にある（ADR 0074）。
func toDetail(a port.BoardAccess, targetLocked, overLimit bool) apitypes.BoardDetail {
	s := toSummary(a)

	return apitypes.BoardDetail{
		ID:              s.ID,
		Name:            s.Name,
		Role:            s.Role,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
		RepositoryOwner: s.RepositoryOwner,
		RepositoryName:  s.RepositoryName,
		ProjectID:       s.ProjectID,
		ProjectNumber:   s.ProjectNumber,
		ProjectTitle:    s.ProjectTitle,
		ProjectURL:      s.ProjectURL,

		// ここから下が BoardSummary に無いぶん。
		Scene:          a.Board.Scene,
		TargetLocked:   targetLocked,
		SceneOverLimit: overLimit,
	}
}

// toBoardWithFiles は開いたボードの応答を組み立てる。共通部分は toDetail から
// 受け取る（toDetail が toSummary から受け取るのと同じ理由）。
//
// **写し漏れは TestGetBoard_CarriesEveryDetailField が落とす。** 改名の応答
// （BoardDetail）と名前で突き合わせている。
func toBoardWithFiles(d apitypes.BoardDetail, files []port.BoardFile) apitypes.BoardWithFiles {
	// 画像が無くても空のオブジェクトで返す。null にすると契約の「ID → 画像」が
	// 読めない。
	out := make(map[string]string, len(files))
	for _, f := range files {
		out[f.ID] = f.Data
	}

	return apitypes.BoardWithFiles{
		ID:              d.ID,
		Name:            d.Name,
		Role:            d.Role,
		CreatedAt:       d.CreatedAt,
		UpdatedAt:       d.UpdatedAt,
		RepositoryOwner: d.RepositoryOwner,
		RepositoryName:  d.RepositoryName,
		ProjectID:       d.ProjectID,
		ProjectNumber:   d.ProjectNumber,
		ProjectTitle:    d.ProjectTitle,
		ProjectURL:      d.ProjectURL,
		Scene:           d.Scene,
		TargetLocked:    d.TargetLocked,
		SceneOverLimit:  d.SceneOverLimit,

		// ここから下が BoardDetail に無いぶん。
		Files: out,
	}
}

// toListEntry は一覧の 1 件を詰め替える。共通部分は toSummary から受け取る
// （toDetail と同じ理由。写し漏れは TestToDetail_CarriesEverySummaryField が
// 一覧と詳細を突き合わせて落とす）。
//
// **件数が nil なら null のまま返す。** 0 件に丸めると、シーンを読めなかった
// ボードが「注釈なし」に見える。
func toListEntry(e usecase.BoardListEntry) apitypes.BoardListEntry {
	s := toSummary(e.BoardAccess)

	out := apitypes.BoardListEntry{
		ID:              s.ID,
		Name:            s.Name,
		Role:            s.Role,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
		RepositoryOwner: s.RepositoryOwner,
		RepositoryName:  s.RepositoryName,
		ProjectID:       s.ProjectID,
		ProjectNumber:   s.ProjectNumber,
		ProjectTitle:    s.ProjectTitle,
		ProjectURL:      s.ProjectURL,
	}
	if e.Counts != nil {
		out.AnnotationCounts = &apitypes.AnnotationCounts{
			Uncreated: e.Counts.Uncreated,
			Created:   e.Counts.Created,
			Changed:   e.Counts.Changed,
		}
	}
	return out
}

// toSyncItem は保存済みの draft issue 1 件を境界の DTO に詰め替える。
// 注釈の状態と作成結果の両方で返すので 1 箇所に置く。
func toSyncItem(it port.SyncItem) apitypes.SyncItem {
	out := apitypes.SyncItem{
		ItemID:         it.ItemID,
		ItemDatabaseID: it.ItemDatabaseID,
		Kind:           apitypes.ItemKind(it.Kind),
		Title:          it.Title,
		Body:           it.Body,
		LocalID:        it.LocalID,
		Action:         apitypes.SyncAction(it.Action),
		// **写し忘れると「確かに作った」ではなく「分からない」に倒れる**
		// （ADR 0056）。ゼロ値の向きをそちらに取ってあるのはこのため。
		Confirmed: it.Confirmed,
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
	// diagrams はプロンプトからの図のドラフト生成。nil でもよい。
	//
	// interpretations と同じ LLM の設定で決まるが、**別々に持つ。** 使えるかを
	// 見せる口（capabilities）が答えているのは「解釈できるか」と「ドラフトを
	// 作れるか」という別の問いで、1 つに畳むと画面がどちらのボタンを止めれば
	// よいのかを決められなくなる。
	diagrams *usecase.DiagramService
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

// maxSceneBody はシーンを載せたリクエストボディを読む上限。
//
// シーンそのものの上限はユースケース層が持つ（usecase.MaxSceneBytes）。ここで
// 重ねて持つのは、上限を超えたボディを全部メモリに載せてからでないと判定できない
// のを避けるため（interpret.go の maxInterpretBody と同じ理由）。判定の正本は
// あくまでユースケース層側で、ここは読み込みの歯止めである。
//
// **ユースケース層が弾くより先にここで切れないようにする。** 先に切れると、
// 上限に収まっているシーンが「大きすぎる」で落ち、減らす必要のないものを
// 減らせと言うことになる。シーンは JSON の文字列として運ばれるので、倍率は
// エスケープの最悪値で取る。`"` と `\` は 2 バイトだが、制御文字や
// `<` のように `\u00XX` で書かれる文字は 1 バイトが 6 バイトになる
// （どこをエスケープするかは送り手しだいで、こちらからは決められない）。
const maxSceneBody = usecase.MaxSceneBytes*6 + 4<<10

// maxSaveBody はシーンの保存のリクエストボディを読む上限。シーンに加えて、
// 貼った画像を載せてくる（ADR 0074）。
//
// 1 回の保存で送られてくる画像は、保存したあとに残る画像に含まれるので、
// 合計は usecase.MaxBoardFileBytes を超えない（超えたら正本が 413 で弾く）。
// 倍率は maxSceneBody と同じ理由でエスケープの最悪値で取る。
const maxSaveBody = (usecase.MaxSceneBytes+usecase.MaxBoardFileBytes)*6 + 4<<10

// bindSceneBody はシーンを載せたリクエストを読む。
//
// 歯止めに引っかかったボディは 400 ではなく 413 に写す。**同じ「大きすぎる」が
// 経路によって違うステータスで返らないようにする。** 写し替えの表は errors.go に
// あるので、ここは sentinel を選ぶだけ。
func (h *handlers) bindSceneBody(c *gin.Context, req any, limit int64) bool {
	widenBody(c, limit)

	err := c.ShouldBindJSON(req)
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		h.fail(c, fmt.Errorf("%w: request body exceeds %d bytes",
			usecase.ErrSceneTooLarge, limit))
		return false
	}

	h.badRequest(c, err)
	return false
}

func (h *handlers) createBoard(c *gin.Context) {
	var req apitypes.CreateBoardRequest
	// 作成は画像を受け取らない（ADR 0074）。歯止めもシーンのぶんだけでよい。
	if !h.bindSceneBody(c, &req, maxSceneBody) {
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
	// 上限も同じで、シーンは上限内であることを確かめてから作り、画像はまだ
	// 1 枚も無い。作った本人は必ず owner（BoardService.Create）。
	c.JSON(http.StatusCreated,
		toDetail(port.BoardAccess{Board: b, Role: port.RoleOwner}, false, false))
}

func (h *handlers) listBoards(c *gin.Context) {
	out, err := h.boardList(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, out)
}

// boardList は一覧の応答を組み立てる。
//
// **`/api` と `/mcp` の両方がここを通る**（ADR 0071）。詰め替えを口ごとに書くと、
// 契約に足したフィールドが片方にだけ載る。生成型は同じなので、載せ忘れた側も
// ゼロ値のままコンパイルが通る。
func (h *handlers) boardList(ctx context.Context) ([]apitypes.BoardListEntry, error) {
	boards, err := h.boards.List(ctx)
	if err != nil {
		return nil, err
	}

	// nil を返すと JSON が null になる。一覧は常に配列にする。
	out := make([]apitypes.BoardListEntry, 0, len(boards))
	for _, e := range boards {
		out = append(out, toListEntry(e))
	}
	return out, nil
}

// getBoard はボードを貼った画像ごと返す。**画像を返すのはこの口だけ**
// （ADR 0074）。
func (h *handlers) getBoard(c *gin.Context) {
	opened, err := h.boards.Open(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}

	locked, err := h.boards.TargetLocked(c.Request.Context(), opened.Board.ID)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, toBoardWithFiles(
		toDetail(opened.BoardAccess, locked, opened.OverLimit), opened.Files))
}

// renameBoard はボードの名前を変える。
//
// 名前だけを受け取る。作成先やシーンを一緒に受けると、この経路でも作成先を
// 書けることになり、固定（ADR 0014）が意味を失う。
func (h *handlers) renameBoard(c *gin.Context) {
	var req apitypes.RenameBoardRequest
	if !h.bindJSON(c, &req) {
		return
	}

	id := c.Param("id")
	if err := h.boards.Rename(c.Request.Context(), id, req.Name); err != nil {
		h.fail(c, err)
		return
	}

	// 改名後の姿を返す。フロントが手元の値を組み立て直さずに済む
	// （作成先の設定と同じ形）。
	b, err := h.boards.Find(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}

	h.respondBoard(c, *b)
}

// getBoardDeletion は削除で何が失われるかを返す。
//
// **削除とは別の呼び出しに保つ。** 見せてから確認させるための口なので、
// 削除の応答に混ぜると押す前に読めない（ADR 0042）。
func (h *handlers) getBoardDeletion(c *gin.Context) {
	d, err := h.boards.Deletion(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, apitypes.BoardDeletion{RecordedItemCount: d.RecordedItemCount})
}

// deleteBoard はボードを削除する。
//
// owner だけかの判断はユースケース層が持つ（ADR 0017）。ここは 204 を返すだけ。
func (h *handlers) deleteBoard(c *gin.Context) {
	if err := h.boards.Delete(c.Request.Context(), c.Param("id")); err != nil {
		h.fail(c, err)
		return
	}

	// 消したものを本文に載せない。メンバーを外す口（removeBoardMember）と
	// 揃える。
	c.Status(http.StatusNoContent)
}

// setBoardTarget は draft issue の作成先をボードに設定する。
//
// 固定済みかどうかの判断はユースケース層が持つ。ここは 409 に写すだけ。
func (h *handlers) setBoardTarget(c *gin.Context) {
	var req apitypes.BoardTarget
	if !h.bindJSON(c, &req) {
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

	h.respondBoard(c, *b)
}

// refreshBoardTargetDisplay は作成先の表示用スナップショットだけを取り直す。
//
// **固定済みでも通る。** 固定するのは作成先そのものであって、表示用の値では
// ない（ADR 0037）。同じかどうかの判断はユースケース層が持つ。
func (h *handlers) refreshBoardTargetDisplay(c *gin.Context) {
	var req apitypes.BoardTargetDisplay
	if !h.bindJSON(c, &req) {
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

	h.respondBoard(c, *b)
}

// respondBoard はボードを固定状態と上限の判定つきで 200 で返す。画像は
// 運ばない（ADR 0074）。
//
// ステータスを引数で受けない。**この形で返すのは既存のボードだけ**で、
// 作成（201）は run を照会せずに返せるので通らない（createBoard）。受けられる
// ようにすると、呼び分ける理由が無いのに呼び分けられる口が残る。
func (h *handlers) respondBoard(c *gin.Context, a port.BoardAccess) {
	locked, err := h.boards.TargetLocked(c.Request.Context(), a.Board.ID)
	if err != nil {
		h.fail(c, err)
		return
	}
	overLimit, err := h.boards.OverLimit(c.Request.Context(), a)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, toDetail(a, locked, overLimit))
}

func (h *handlers) saveScene(c *gin.Context) {
	var req apitypes.SaveSceneRequest
	if !h.bindSceneBody(c, &req, maxSaveBody) {
		return
	}

	saved, err := h.boards.SaveScene(
		c.Request.Context(), c.Param("id"), req.Scene, req.Files, req.BaseUpdatedAt)
	if err != nil {
		h.fail(c, err)
		return
	}

	// 保存後の版を返す。返さないと、クライアントは次の保存の基準を得るために
	// 毎回ボードを取り直すことになり、シーンまで運ぶ（ADR 0020）。持っている
	// 画像も返す。次の保存で何を送らなくてよいかは、サーバーが決めた結果で
	// 知らせる（ADR 0074）。
	//
	// nil は空の配列にする。null にすると契約の「持っている画像」が読めない。
	// port は ID の並びとしか約束していないので、ここで揃える。
	held := saved.FileIDs
	if held == nil {
		held = []string{}
	}
	c.JSON(http.StatusOK, apitypes.SaveSceneResponse{
		UpdatedAt: saved.UpdatedAt,
		FileIds:   held,
	})
}

func (h *handlers) listAnnotations(c *gin.Context) {
	out, err := h.boardAnnotations(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, out)
}

// boardAnnotations は注釈の 3 状態の応答を組み立てる。`/api` と `/mcp` の両方が
// 通る（boardList と同じ理由）。
func (h *handlers) boardAnnotations(ctx context.Context, boardID string) (apitypes.BoardAnnotations, error) {
	states, detached, err := h.annotations.ListStates(ctx, boardID)
	if err != nil {
		// ボードが無い場合と注釈が 0 件の場合は、ここで区別がついている。
		// ListStates が引き当てられなければエラーを返すため、ボードを引き直す
		// 必要が無くなった。
		return apitypes.BoardAnnotations{}, err
	}

	out := apitypes.BoardAnnotations{
		// nil を返すと JSON が null になる。一覧は常に配列にする。
		Annotations: make([]apitypes.AnnotationStatus, 0, len(states)),
		Detached:    make([]apitypes.DetachedAnnotation, 0, len(detached)),
	}
	for _, s := range states {
		out.Annotations = append(out.Annotations, toAnnotationStatus(s))
	}
	for _, d := range detached {
		out.Detached = append(out.Detached, toDetachedAnnotation(d))
	}
	return out, nil
}

// toDetachedAnnotation はシーンから消えた注釈を境界の形にする。
//
// **名前も粒度も 3 状態も載せない。** シーンに frame が無いので取りようが
// 無く、既定で埋めると「名前の無い注釈」（ADR 0022）と見分けが付かなくなる。
func toDetachedAnnotation(d usecase.DetachedAnnotation) apitypes.DetachedAnnotation {
	res := apitypes.DetachedAnnotation{
		ID:    d.ID,
		Items: toSyncItems(d.Items),
	}
	// 届いたか分からない書き込みは別のリストで返す（ADR 0056）。0 件なら省く。
	if len(d.Unconfirmed) > 0 {
		res.UnconfirmedItems = toSyncItems(d.Unconfirmed)
	}
	if d.LatestRun != nil {
		t := d.LatestRun.CreatedAt
		res.LastSyncedAt = &t
	}
	return res
}

// listAnnotationRuns はその注釈の実行履歴を返す。
//
// **畳み込みではなく 1 回ずつの記録を返す**（ADR 0007 / 0026）。いま GitHub に
// 在るものは listAnnotations の items が持つ。
func (h *handlers) listAnnotationRuns(c *gin.Context) {
	out, err := h.annotationRuns(c.Request.Context(), c.Param("id"), c.Param("annotationId"))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, out)
}

// annotationRuns は実行の履歴の応答を組み立てる。`/api` と `/mcp` の両方が
// 通る（boardList と同じ理由）。
func (h *handlers) annotationRuns(
	ctx context.Context, boardID, annotationID string,
) ([]apitypes.SyncRun, error) {
	runs, err := h.annotations.ListRuns(ctx, boardID, annotationID)
	if err != nil {
		return nil, err
	}

	// nil を返すと JSON が null になる。一覧は常に配列にする。
	out := make([]apitypes.SyncRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, toSyncRun(r))
	}
	return out, nil
}

func toSyncRun(r port.SyncRun) apitypes.SyncRun {
	out := apitypes.SyncRun{
		ID:        r.ID,
		CreatedAt: r.CreatedAt,
		// 一覧は常に配列にする。nil を返すと JSON が null になる。
		Items: toSyncItems(r.Items),
		Error: r.Error,
	}

	// **記録していなかった頃の run では省く。** complete を埋めると、当時も
	// 起きていた途中失敗を成功として言い切ることになる（ADR 0043）。
	if outcome, ok := toRunOutcome(r.Outcome); ok {
		out.Outcome = &outcome
	}

	return out
}

// toRunOutcome は run の結末を契約の値に写す。記録が無ければ ok が false。
func toRunOutcome(o port.RunOutcome) (apitypes.RunOutcome, bool) {
	if !o.Valid() {
		return "", false
	}

	return apitypes.RunOutcome(o), true
}

func toAnnotationStatus(s usecase.AnnotationState) apitypes.AnnotationStatus {
	res := apitypes.AnnotationStatus{
		ID:          s.Annotation.ID,
		Name:        s.Annotation.Name,
		Granularity: apitypes.Granularity(s.Annotation.Granularity),
		State:       apitypes.SyncState(s.State),
	}

	// **選んでいなければ省略する。** 空文字を載せると DiagramKind の enum に
	// 無い値が契約の外から出ることになる。**Valid() も見る。** 検証前に
	// 保存された既存データに不明な値が残っていた場合の備え。
	if s.Annotation.Kind != domain.DiagramKindUnspecified && s.Annotation.Kind.Valid() {
		kind := apitypes.DiagramKind(s.Annotation.Kind)
		res.Kind = &kind
	}

	if s.LatestRun != nil {
		syncedAt := s.LatestRun.CreatedAt
		res.LastSyncedAt = &syncedAt

		// 途中で失敗したかどうかは一覧にも出す。履歴を開かないと気づけないと、
		// 翌日戻ってきた開発者には「件数が少ない run」にしか見えない
		// （ADR 0043）。**理由は載せない。** 手掛かりの本文は履歴が持つ。
		if outcome, ok := toRunOutcome(s.LatestRun.Outcome); ok {
			res.LastRunOutcome = &outcome
		}
	}
	// 中身は最新 run ではなく畳み込みから出す（ADR 0026）。0 件なら省く。
	if len(s.Items) > 0 {
		res.Items = toSyncItems(s.Items)
	}
	// **畳み込みには入らないものを別に出す**（ADR 0056）。混ぜると「いま
	// GitHub に在る N 件」が嘘になる。0 件なら省く。
	if len(s.Unconfirmed) > 0 {
		res.UnconfirmedItems = toSyncItems(s.Unconfirmed)
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

// bindJSON は掛かっている上限（既定は router.go の defaultMaxBody、広げた口では
// widenBody で置いたもの）のもとで本文を読む。
//
// **歯止めに当たった失敗も契約の code に写す**（`.claude/rules/api-contract.md`）。
// 写さないと、同じ「大きすぎる」がボディの大きさしだいで 400 と 413 に割れ、
// 画面が同じ原因を 2 通りに案内することになる。
//
// シーン・解釈・図のドラフトは自分の上限と自分の sentinel を持つので、この
// ヘルパーは通らない（bindSceneBody / bindInterpretImages / bindDiagramRequest）。
func (h *handlers) bindJSON(c *gin.Context, req any) bool {
	err := c.ShouldBindJSON(req)
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		h.fail(c, fmt.Errorf("%w: request body exceeds %d bytes",
			errRequestTooLarge, tooLarge.Limit))
		return false
	}

	h.badRequest(c, err)
	return false
}
