package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// membersNotConfigured は認証未設定のときの案内。
//
// 招待は「誰であるか」が決まって初めて意味を持つ。認証を設定していない構成は
// 利用者 1 人なので、共有する相手がいない（ADR 0016 / 0017）。
const membersNotConfigured = "sharing requires authentication: " +
	"set ETOKI_GITHUB_APP_CLIENT_ID and ETOKI_GITHUB_APP_CLIENT_SECRET"

func (h *handlers) listBoardMembers(c *gin.Context) {
	if h.members == nil {
		errorJSON(c, http.StatusServiceUnavailable, membersNotConfigured)
		return
	}

	members, err := h.members.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.failMember(c, err)
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
		errorJSON(c, http.StatusServiceUnavailable, membersNotConfigured)
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
		h.failMember(c, err)
		return
	}

	c.JSON(http.StatusCreated, toMember(m))
}

func (h *handlers) setBoardMemberRole(c *gin.Context) {
	if h.members == nil {
		errorJSON(c, http.StatusServiceUnavailable, membersNotConfigured)
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
		h.failMember(c, err)
		return
	}

	c.JSON(http.StatusOK, toMember(m))
}

func (h *handlers) removeBoardMember(c *gin.Context) {
	if h.members == nil {
		errorJSON(c, http.StatusServiceUnavailable, membersNotConfigured)
		return
	}

	if err := h.members.Remove(
		c.Request.Context(), c.Param("id"), c.Param("userId")); err != nil {
		h.failMember(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// failMember はメンバー操作のエラーを HTTP ステータスに写す。
func (h *handlers) failMember(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usecase.ErrAlreadyMember), errors.Is(err, usecase.ErrLastOwner):
		// どちらも「いまの状態では通せない」。入力の誤りではないので 400 に
		// しない。やり直せば通る種類でもないので 409 に寄せる。
		errorJSON(c, http.StatusConflict, err.Error())

	default:
		h.fail(c, err)
	}
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
