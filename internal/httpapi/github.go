package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
)

// githubNotConfigured は GitHub 未設定のときの案内。
//
// 作成先はボードごとに選ぶので、必要なのはトークンだけ（ADR 0014）。
const githubNotConfigured = "github is not configured: set ETOKI_GITHUB_TOKEN"

// listRepositories は作成先に選べるリポジトリを返す。
func (h *handlers) listRepositories(c *gin.Context) {
	if h.catalog == nil {
		errorJSON(c, http.StatusServiceUnavailable, githubNotConfigured)
		return
	}

	repos, err := h.catalog.ListRepositories(c.Request.Context())
	if err != nil {
		h.failCatalog(c, err)
		return
	}

	// nil を返すと JSON が null になる。一覧は常に配列にする。
	out := make([]apitypes.Repository, 0, len(repos))
	for _, r := range repos {
		out = append(out, apitypes.Repository{
			Owner:       r.Owner,
			Name:        r.Name,
			Description: r.Description,
		})
	}

	c.JSON(http.StatusOK, out)
}

// listRepositoryProjects はリポジトリに紐づく Projects v2 を返す。
func (h *handlers) listRepositoryProjects(c *gin.Context) {
	if h.catalog == nil {
		errorJSON(c, http.StatusServiceUnavailable, githubNotConfigured)
		return
	}

	projects, err := h.catalog.ListProjects(c.Request.Context(), c.Param("owner"), c.Param("repo"))
	if err != nil {
		h.failCatalog(c, err)
		return
	}

	out := make([]apitypes.Project, 0, len(projects))
	for _, p := range projects {
		out = append(out, apitypes.Project{ID: p.ID, Number: p.Number, Title: p.Title})
	}

	c.JSON(http.StatusOK, out)
}

// failCatalog は一覧取得のエラーを HTTP ステータスに写す。
//
// GitHub 側の失敗は 502 にする。500 にすると etoki の不具合と読めてしまい、
// トークンの権限不足やレート制限であることが伝わらない。
// 404 には写さない。存在しないリポジトリを指しても GitHub 側のエラーとして
// 返るので、etoki が「無い」と断定できるのは owner / repo が空のときだけ。
// 契約（api/openapi.yaml）にも 404 は無い。
func (h *handlers) failCatalog(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usecase.ErrInvalidInput):
		h.badRequest(c, err)

	default:
		h.logger.ErrorContext(c.Request.Context(), "github listing failed",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))
		errorJSON(c, http.StatusBadGateway, err.Error())
	}
}
