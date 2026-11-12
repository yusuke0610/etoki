package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/port"
)

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
