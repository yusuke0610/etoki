package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
)

// accountDeletionNotConfigured は利用者を消す口を組み立てていないときの案内。
//
// 案内は認証のものを使う。利用者の行を消す口は cmd/etoki が必ず渡すので、利用者が
// 設定で足せるのは認証の側だけ（MCP の接続と同じ）。
func (h *handlers) accountDeletionNotConfigured(c *gin.Context) {
	notConfigured(c, apitypes.ErrorCodeAccountDeletionNotConfigured,
		"account deletion requires authentication and an account repository", h.hints.Auth)
}

// getAccountDeletion は自分を消すと失われるものを返す（ADR 0101）。何も書かない。
func (h *handlers) getAccountDeletion(c *gin.Context) {
	if h.accounts == nil {
		h.accountDeletionNotConfigured(c)
		return
	}

	d, err := h.accounts.Deletion(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}

	boards := make([]apitypes.AccountDeletionBoard, 0, len(d.Boards))
	for _, b := range d.Boards {
		boards = append(boards, apitypes.AccountDeletionBoard{
			ID:                    b.ID,
			Name:                  b.Name,
			OtherMemberCount:      b.OtherMemberCount,
			RecordedItemCount:     b.Losing.RecordedItemCount,
			UnconfirmedWriteCount: b.Losing.UnconfirmedWriteCount,
		})
	}
	c.JSON(http.StatusOK, apitypes.AccountDeletion{
		Boards:         boards,
		LeftBoardCount: d.LeftBoardCount,
	})
}

// deleteAccount は自分を消す（ADR 0101）。
//
// **消したらセッションの cookie も消す。** セッションの行は利用者と一緒に消えて
// いるが、cookie を残すと、画面が次に送る要求まで消えたセッションを名乗り続ける。
// 途中で失敗したら cookie は残す。まだ消えていない利用者として、もう一度消せる。
func (h *handlers) deleteAccount(c *gin.Context) {
	if h.accounts == nil {
		h.accountDeletionNotConfigured(c)
		return
	}

	if err := h.accounts.Delete(c.Request.Context()); err != nil {
		h.fail(c, err)
		return
	}

	h.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}
