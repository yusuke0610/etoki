package httpapi

import (
	"context"
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
		Premise:        a.Board.Premise,
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
		Premise:         d.Premise,
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

	if err := c.ShouldBindJSON(req); err != nil {
		h.failBind(c, err, usecase.ErrSceneTooLarge)
		return false
	}
	return true
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

// maxPremiseBody は背景の書き換えのリクエストボディを読む上限（ADR 0093）。
//
// 正本の上限は usecase.MaxPremiseRunes（文字数）。本文には背景が 2 つ（新しい
// 背景と、照合のための編集前の背景）載り、JSON のエスケープの最悪では 1 文字が
// 12 バイトになる（BMP の外の文字をサロゲートペアで `\uXXXX\uXXXX` と書いた
// とき）。その最悪値で割り増して置く。小さく取ると、上限ちょうどの背景が
// 歯止め側で落ち、減らす必要のないものを減らせと言うことになる。
const maxPremiseBody = usecase.MaxPremiseRunes*12*2 + 4<<10

// setBoardPremise はボードの背景を書き換える（ADR 0093）。
//
// 背景だけを受け取る。シーンや作成先を一緒に受けると、この経路でもそれらを
// 書けることになる（改名と同じ理由）。
func (h *handlers) setBoardPremise(c *gin.Context) {
	widenBody(c, maxPremiseBody)

	var req apitypes.SetPremiseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 歯止めに当たった失敗も、長すぎる背景と同じ code に写す。写さないと、
		// 同じ「長すぎる」が本文の大きさしだいで 2 通りの code に割れる。
		h.failBind(c, err, usecase.ErrPremiseTooLarge)
		return
	}

	id := c.Param("id")
	if err := h.boards.SetPremise(c.Request.Context(), id, req.BasePremise, req.Premise); err != nil {
		h.fail(c, err)
		return
	}

	// 書き換えたあとの姿を返す（改名と同じ）。正規化した背景を、フロントが
	// 次の照合の基準にできるようにする。
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

	c.JSON(http.StatusOK, apitypes.BoardDeletion{
		RecordedItemCount:     d.RecordedItemCount,
		UnconfirmedWriteCount: d.UnconfirmedWriteCount,
	})
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
