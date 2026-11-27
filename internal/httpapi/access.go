package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
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
		// **`*_not_configured` は名乗らない。** getCapabilities の材料に
		// この口は入っていないので、名乗ると「使える」と案内した機能が
		// 「未設定」と返る組み合わせが作れる。
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
		ProjectAccess: apitypes.ProjectAccess(state.ProjectAccess),
	})
}
