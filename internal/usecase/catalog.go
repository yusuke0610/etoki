package usecase

import (
	"context"
	"fmt"

	"github.com/yusuke0610/etoki/port"
)

// GitHubCatalogService は作成先として選べるものの一覧を返す。
//
// ボードに何を設定するかは開発者が選ぶ。この層は候補を並べるだけで、
// 自動でどれかを選んだり既定にしたりしない（中核思想 3）。
type GitHubCatalogService struct {
	github port.GitHubClient
}

// NewGitHubCatalogService は GitHubCatalogService を作る。
func NewGitHubCatalogService(github port.GitHubClient) *GitHubCatalogService {
	return &GitHubCatalogService{github: github}
}

// ListRepositories は選べるリポジトリを返す。
func (s *GitHubCatalogService) ListRepositories(ctx context.Context) ([]port.Repository, error) {
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
