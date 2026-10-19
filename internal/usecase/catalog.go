package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yusuke0610/etoki/port"
)

// GitHubCatalogService は作成先として選べるものの一覧を返す。
//
// ボードに何を設定するかは開発者が選ぶ。この層は候補を並べるだけで、
// 自動でどれかを選んだり既定にしたりしない（中核思想 3）。
type GitHubCatalogService struct {
	github     port.GitHubClient
	fieldNames ProjectFieldNames
	logger     *slog.Logger
}

// NewGitHubCatalogService は GitHubCatalogService を作る。
//
// fieldNames は作成（`CreationService`）と同じものを渡す。**任意の設定ではなく
// 引数にしてある。** 渡し忘れると既定の名前で確かめることになり、名前を変えた
// 構成で、確認と作成の答えが食い違う（#292）。空の名前は既定に落とす。
func NewGitHubCatalogService(
	github port.GitHubClient, fieldNames ProjectFieldNames, logger *slog.Logger,
) *GitHubCatalogService {
	if logger == nil {
		logger = slog.Default()
	}
	return &GitHubCatalogService{
		github:     github,
		fieldNames: fieldNames.withDefaults(),
		logger:     logger,
	}
}

// ListRepositories は選べるリポジトリを返す。
//
// **打ち切りの有無も一緒に返す**（ADR 0054）。ここで落とすと、境界に出る前に
// 「これで全部」と「ここまでしか見ていない」の区別が消える。
func (s *GitHubCatalogService) ListRepositories(
	ctx context.Context,
) (port.RepositoryList, error) {
	return s.github.ListRepositories(ctx)
}

// ListProjects はリポジトリに紐づく Projects v2 を返す。
func (s *GitHubCatalogService) ListProjects(
	ctx context.Context, owner, name string,
) ([]port.Project, error) {
	if owner == "" || name == "" {
		return nil, fmt.Errorf("%w: repository owner and name are required", ErrInvalidInput)
	}
	return s.github.ListRepositoryProjects(ctx, owner, name)
}
