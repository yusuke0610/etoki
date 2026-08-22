package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// getBoardAccess はそのボードで何ができるかを返す。
//
// **これで作成を止めるのではなく、できない理由を先に見せるために使う**
// （ADR 0017）。GitHub 側の可否は「いまの状態」であって判定ではない。
func (h *handlers) getBoardAccess(c *gin.Context) {
	if h.access == nil {
		// 組み立て口（etoki.New）は Auth の有無によらず必ず渡すので、
		// production では起きない。ここに来るのは Deps を手で組んで足りて
		// いないときだけで、それは**設定の不足ではなく配線の不具合**。
		//
		// **sharing_not_configured は使わない。** 共有が使えるかは
		// getCapabilities が h.members だけで答える（この口は材料に入って
		// いない）ので、ここで共有未設定を名乗ると「共有は使える」と案内した
		// 直後に「共有は未設定」と返る組み合わせが作れる。
		h.logger.ErrorContext(c.Request.Context(), "board access service is not configured",
			slog.String("path", c.Request.URL.Path),
		)
		errorJSON(c, http.StatusInternalServerError, apitypes.ErrorCodeInternal, "internal error")
		return
	}

	state, err := h.access.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, apitypes.BoardAccess{
		Role:          apitypes.BoardRole(state.Role),
		ProjectAccess: apitypes.ProjectAccess(state.ProjectAccess),
	})
}

func (h *handlers) listBoardMembers(c *gin.Context) {
	if h.members == nil {
		sharingNotConfigured(c)
		return
	}

	members, err := h.members.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}

	// nil を返すと JSON が null になる。一覧は常に配列にする。
	out := make([]apitypes.BoardMember, 0, len(members))
	for _, m := range members {
		out = append(out, toMember(m))
	}

	c.JSON(http.StatusOK, out)
}

func (h *handlers) inviteBoardMember(c *gin.Context) {
	if h.members == nil {
		sharingNotConfigured(c)
		return
	}

	var req apitypes.InviteMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.badRequest(c, err)
		return
	}

	m, err := h.members.Invite(
		c.Request.Context(), c.Param("id"), req.Login, port.BoardRole(req.Role))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusCreated, toMember(m))
}

func (h *handlers) setBoardMemberRole(c *gin.Context) {
	if h.members == nil {
		sharingNotConfigured(c)
		return
	}

	var req apitypes.SetMemberRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.badRequest(c, err)
		return
	}

	m, err := h.members.SetRole(
		c.Request.Context(), c.Param("id"), c.Param("userId"), port.BoardRole(req.Role))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, toMember(m))
}

func (h *handlers) removeBoardMember(c *gin.Context) {
	if h.members == nil {
		sharingNotConfigured(c)
		return
	}

	if err := h.members.Remove(
		c.Request.Context(), c.Param("id"), c.Param("userId")); err != nil {
		h.fail(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// toMember は表示用のメンバーを境界の DTO に詰め替える。
func toMember(m usecase.Member) apitypes.BoardMember {
	return apitypes.BoardMember{
		UserID:      m.Membership.UserID,
		Login:       m.Login,
		DisplayName: m.DisplayName,
		Role:        apitypes.BoardRole(m.Membership.Role),
		CreatedAt:   m.Membership.CreatedAt,
	}
}
