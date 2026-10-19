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
		h.githubNotConfigured(c)
		return
	}

	list, err := h.catalog.ListRepositories(c.Request.Context())
	if err != nil {
		h.failCatalog(c, err)
		return
	}

	// nil を返すと JSON が null になる。一覧は常に配列にする。
	out := make([]apitypes.Repository, 0, len(list.Repositories))
	for _, r := range list.Repositories {
		out = append(out, apitypes.Repository{
			Owner:       r.Owner,
			Name:        r.Name,
			Description: r.Description,
		})
	}

	// **配列ではなくオブジェクトで返す**（ADR 0054）。打ち切りを載せる場所が
	// 要る。ヘッダにしなかったのは、契約に現れないものを画面が読むことになり、
	// 生成した型から辿れなくなるため（ADR 0011）。
	c.JSON(http.StatusOK, apitypes.RepositoryList{
		Repositories: out,
		Truncated:    list.Truncated,
	})
}

// listRepositoryProjects はリポジトリに紐づく Projects v2 を返す。
func (h *handlers) listRepositoryProjects(c *gin.Context) {
	if h.catalog == nil {
		h.githubNotConfigured(c)
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

// checkProjectFields は作成先の Project に、作成に要るフィールドが揃っているかを
// 返す（ADR 0086）。
//
// **GitHub に問い合わせられなかったことは 200 の unknown で返る。** 失敗に写すのは
// 入力の誤りだけで、それはユースケースが決める。表示のための材料なので、GitHub が
// 落ちているだけで選択の画面に失敗を出さない。
func (h *handlers) checkProjectFields(c *gin.Context) {
	if h.catalog == nil {
		h.githubNotConfigured(c)
		return
	}

	projectID := c.Param("projectId")
	check, err := h.catalog.CheckProjectFields(c.Request.Context(), projectID)
	if err != nil {
		h.failCatalog(c, err)
		return
	}

	// nil を返すと JSON が null になる。足りないものが無くても配列にする。
	missing := make([]apitypes.ProjectFieldRequirement, 0, len(check.Missing))
	for _, m := range check.Missing {
		missing = append(missing, apitypes.ProjectFieldRequirement(m))
	}

	c.JSON(http.StatusOK, apitypes.ProjectFieldsCheck{
		ProjectID:     projectID,
		Status:        apitypes.ProjectFieldsStatus(check.Status),
		MissingFields: missing,
	})
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
