package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
)

// listRepositories は作成先に選べるリポジトリを返す。
func (h *handlers) listRepositories(c *gin.Context) {
	if h.catalog == nil {
		githubNotConfigured(c)
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
		githubNotConfigured(c)
		return
	}

	projects, err := h.catalog.ListProjects(c.Request.Context(), c.Param("owner"), c.Param("repo"))
	if err != nil {
		h.failCatalog(c, err)
		return
	}

	out := make([]apitypes.Project, 0, len(projects))
	for _, p := range projects {
		out = append(out, apitypes.Project{ID: p.ID, Number: p.Number, Title: p.Title, URL: p.URL})
	}

	c.JSON(http.StatusOK, out)
}

// failCatalog は一覧取得のエラーを応答にする。
//
// **既定が 502 なのがここだけ違う。** 表に無いものを 500 にすると etoki の
// 不具合と読めてしまい、トークンの権限不足やレート制限であることが伝わらない。
// 404 には写さない。存在しないリポジトリを指しても GitHub 側のエラーとして
// 返るので、etoki が「無い」と断定できるのは owner / repo が空のときだけ。
// 契約（api/openapi.yaml）にも 404 は無い。
func (h *handlers) failCatalog(c *gin.Context, err error) {
	if respondMapped(c, err) {
		return
	}

	h.logger.ErrorContext(c.Request.Context(), "github listing failed",
		slog.String("path", c.Request.URL.Path), slog.Any("error", err))
	// GitHub が返した本文をそのまま載せる。画面は既定で畳むが、開けば
	// レート制限や権限不足の手掛かりになる。
	errorJSON(c, http.StatusBadGateway, apitypes.ErrorCodeGithubUnavailable, err.Error())
}
