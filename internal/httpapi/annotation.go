package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

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
