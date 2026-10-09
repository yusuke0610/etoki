package sqlite_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/port"
)

func newGrants(t *testing.T) (*sqlite.OAuthGrantRepository, *sql.DB, port.User) {
	t.Helper()

	db := newDB(t)
	user := seedUser(t, newSessions(t, db))
	return sqlite.NewOAuthGrantRepository(db), db, user
}

func seedCode(t *testing.T, repo *sqlite.OAuthGrantRepository, hash, userID string) port.OAuthCode {
	t.Helper()

	c := port.OAuthCode{
		CodeHash:      hash,
		UserID:        userID,
		ClientID:      "client-1",
		ClientName:    "Claude Code",
		Scope:         "read",
		Resource:      "http://127.0.0.1:8080/mcp",
		RedirectURI:   "http://127.0.0.1:33418/callback",
		CodeChallenge: "challenge",
		CreatedAt:     baseTime,
		ExpiresAt:     baseTime.Add(5 * time.Minute),
	}
	if err := repo.SaveCode(t.Context(), c); err != nil {
		t.Fatalf("SaveCode: %v", err)
	}
	return c
}

func grantOf(id, userID string, at time.Time) port.OAuthGrant {
	return port.OAuthGrant{
		ID: id, UserID: userID, ClientID: "client-1", ClientName: "Claude Code",
		Scope: "read", Resource: "http://127.0.0.1:8080/mcp",
		CreatedAt: at, LastUsedAt: at,
	}
}

func tokenPair(prefix string, at time.Time) []port.OAuthToken {
	return []port.OAuthToken{
		{TokenHash: prefix + "-access", Kind: port.OAuthAccessToken,
			CreatedAt: at, ExpiresAt: at.Add(time.Hour)},
		{TokenHash: prefix + "-refresh", Kind: port.OAuthRefreshToken,
			CreatedAt: at, ExpiresAt: at.Add(30 * 24 * time.Hour)},
	}
}

func seedGrant(t *testing.T, repo *sqlite.OAuthGrantRepository, id, userID string, at time.Time) {
	t.Helper()
	if err := repo.CreateGrant(t.Context(), grantOf(id, userID, at), tokenPair(id, at)); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
}

func TestOAuthClient_RoundTrip(t *testing.T) {
	t.Parallel()

	repo, _, _ := newGrants(t)
	want := port.OAuthClient{
		ID: "client-1", Name: "Claude Code",
		RedirectURIs: []string{"http://127.0.0.1/callback", "https://example.com/cb"},
		CreatedAt:    baseTime,
	}
	if err := repo.CreateClient(t.Context(), want, baseTime.Add(-24*time.Hour)); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}

	got, err := repo.FindClient(t.Context(), "client-1")
	if err != nil {
		t.Fatalf("FindClient: %v", err)
	}
	if got == nil {
		t.Fatal("FindClient = nil")
	}
	if got.Name != want.Name || len(got.RedirectURIs) != 2 ||
		got.RedirectURIs[0] != want.RedirectURIs[0] || got.RedirectURIs[1] != want.RedirectURIs[1] ||
		!got.CreatedAt.Equal(baseTime) {
		t.Errorf("FindClient = %+v, want %+v", *got, want)
	}

	missing, err := repo.FindClient(t.Context(), "nope")
	if err != nil || missing != nil {
		t.Errorf("FindClient(nope) = %v, %v; want nil, nil", missing, err)
	}
}

// 登録は誰でもできるので、許可を得ないまま残った登録は消す。**許可を得た
// クライアントは、許可を取り消したあとも残す。** 消すと、client_id を控えている
// クライアントが次の認可で断られる。
func TestCreateClient_PrunesOnlyNeverGrantedClients(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	ctx := t.Context()
	cutoff := baseTime.Add(-24 * time.Hour)

	old := baseTime.Add(-25 * time.Hour)
	for _, id := range []string{"stale", "granted"} {
		if err := repo.CreateClient(ctx, port.OAuthClient{
			ID: id, Name: id, RedirectURIs: []string{"http://127.0.0.1/cb"}, CreatedAt: old,
		}, old.Add(-24*time.Hour)); err != nil {
			t.Fatalf("CreateClient(%s): %v", id, err)
		}
	}
	g := grantOf("g1", user.ID, old)
	g.ClientID = "granted"
	if err := repo.CreateGrant(ctx, g, tokenPair("g1", old)); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
	if err := repo.DeleteGrant(ctx, "g1"); err != nil {
		t.Fatalf("DeleteGrant: %v", err)
	}
	// 境界ちょうど（cutoff に登録）は残す。
	if err := repo.CreateClient(ctx, port.OAuthClient{
		ID: "fresh", Name: "fresh", RedirectURIs: []string{"http://127.0.0.1/cb"}, CreatedAt: cutoff,
	}, cutoff); err != nil {
		t.Fatalf("CreateClient(fresh): %v", err)
	}

	for id, wantKept := range map[string]bool{"stale": false, "granted": true, "fresh": true} {
		got, err := repo.FindClient(ctx, id)
		if err != nil {
			t.Fatalf("FindClient(%s): %v", id, err)
		}
		if (got != nil) != wantKept {
			t.Errorf("%s が残っているか = %v, want %v", id, got != nil, wantKept)
		}
	}
}

func TestConsumeCode_FirstUseThenReplay(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	ctx := t.Context()
	saved := seedCode(t, repo, "code-hash", user.ID)

	first, err := repo.ConsumeCode(ctx, "code-hash", "grant-1", baseTime.Add(time.Minute))
	if err != nil {
		t.Fatalf("ConsumeCode: %v", err)
	}
	if first == nil {
		t.Fatal("ConsumeCode = nil")
	}
	if first.Replayed || first.GrantID != "grant-1" {
		t.Errorf("1 回目 = Replayed %v, GrantID %q; want false, grant-1", first.Replayed, first.GrantID)
	}
	if first.UserID != user.ID || first.RedirectURI != saved.RedirectURI ||
		first.CodeChallenge != saved.CodeChallenge || first.Resource != saved.Resource ||
		first.Scope != saved.Scope || first.ClientID != saved.ClientID ||
		first.ClientName != saved.ClientName {
		t.Errorf("1 回目の中身 = %+v, want %+v", *first, saved)
	}

	// 2 回目は別の grantID を渡しても、1 回目のものを返す。
	second, err := repo.ConsumeCode(ctx, "code-hash", "grant-2", baseTime.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ConsumeCode(2): %v", err)
	}
	if second == nil || !second.Replayed || second.GrantID != "grant-1" {
		t.Errorf("2 回目 = %+v; want Replayed, GrantID grant-1", second)
	}
}

func TestConsumeCode_RejectsExpiredAndUnknown(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	ctx := t.Context()
	seedCode(t, repo, "code-hash", user.ID)

	// 期限ちょうどは通さない。
	got, err := repo.ConsumeCode(ctx, "code-hash", "g", baseTime.Add(5*time.Minute))
	if err != nil || got != nil {
		t.Errorf("期限ちょうど = %v, %v; want nil, nil", got, err)
	}
	got, err = repo.ConsumeCode(ctx, "unknown", "g", baseTime)
	if err != nil || got != nil {
		t.Errorf("未知 = %v, %v; want nil, nil", got, err)
	}
	// 期限前なら通る（期限ちょうどで使用済みにしていないこと）。
	got, err = repo.ConsumeCode(ctx, "code-hash", "g", baseTime.Add(5*time.Minute-time.Nanosecond))
	if err != nil || got == nil || got.Replayed {
		t.Errorf("期限前 = %+v, %v; want 1 回目", got, err)
	}
}

func TestFindToken_ReturnsTokenWithGrant(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	ctx := t.Context()
	seedGrant(t, repo, "g1", user.ID, baseTime)

	got, err := repo.FindToken(ctx, "g1-access", baseTime.Add(time.Minute))
	if err != nil {
		t.Fatalf("FindToken: %v", err)
	}
	if got == nil {
		t.Fatal("FindToken = nil")
	}
	if got.Token.Kind != port.OAuthAccessToken || !got.Token.ExpiresAt.Equal(baseTime.Add(time.Hour)) {
		t.Errorf("Token = %+v", got.Token)
	}
	if got.Grant.ID != "g1" || got.Grant.UserID != user.ID ||
		got.Grant.Resource != "http://127.0.0.1:8080/mcp" || got.Grant.Scope != "read" {
		t.Errorf("Grant = %+v", got.Grant)
	}

	// 期限ちょうどは通さない。
	expired, err := repo.FindToken(ctx, "g1-access", baseTime.Add(time.Hour))
	if err != nil || expired != nil {
		t.Errorf("期限ちょうど = %v, %v; want nil, nil", expired, err)
	}
}

// 時刻の書式が桁数で揃っていないと、文字列の比較が時刻の順と食い違う。
// 小数部の末尾の 0 を落とす書式だと、秒ちょうどの期限 "…00Z" が、それより
// 後の "…00.5Z" より大きく並び、**切れたトークンが通る。**
func TestFindToken_ComparesSubsecondExpiryInTimeOrder(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	ctx := t.Context()
	seedGrant(t, repo, "g1", user.ID, baseTime)

	got, err := repo.FindToken(ctx, "g1-access", baseTime.Add(time.Hour+500*time.Millisecond))
	if err != nil || got != nil {
		t.Errorf("期限の 0.5 秒後 = %v, %v; want nil, nil", got, err)
	}
}

func TestUseRefreshToken_RotatesOnceThenReportsReplay(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	ctx := t.Context()
	seedGrant(t, repo, "g1", user.ID, baseTime)

	first, err := repo.UseRefreshToken(ctx, "g1-refresh", baseTime.Add(time.Hour))
	if err != nil {
		t.Fatalf("UseRefreshToken: %v", err)
	}
	if first == nil || first.Replayed || first.Grant.ID != "g1" {
		t.Fatalf("1 回目 = %+v; want g1, 未使用", first)
	}

	// 使用済みは FindToken からも見えない。
	if found, err := repo.FindToken(ctx, "g1-refresh", baseTime.Add(time.Hour)); err != nil || found != nil {
		t.Errorf("使用済みの FindToken = %v, %v; want nil, nil", found, err)
	}

	second, err := repo.UseRefreshToken(ctx, "g1-refresh", baseTime.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("UseRefreshToken(2): %v", err)
	}
	if second == nil || !second.Replayed || second.Grant.ID != "g1" {
		t.Errorf("2 回目 = %+v; want Replayed, g1", second)
	}

	// アクセストークンを refresh として使わせない。
	access, err := repo.UseRefreshToken(ctx, "g1-access", baseTime.Add(time.Minute))
	if err != nil || access != nil {
		t.Errorf("アクセストークン = %v, %v; want nil, nil", access, err)
	}
}

func TestUseRefreshToken_RejectsExpired(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	seedGrant(t, repo, "g1", user.ID, baseTime)

	got, err := repo.UseRefreshToken(t.Context(), "g1-refresh", baseTime.Add(30*24*time.Hour))
	if err != nil || got != nil {
		t.Errorf("期限ちょうど = %v, %v; want nil, nil", got, err)
	}
}

func TestAddTokens(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	ctx := t.Context()
	seedGrant(t, repo, "g1", user.ID, baseTime)

	later := baseTime.Add(time.Hour)
	if err := repo.AddTokens(ctx, "g1", tokenPair("next", later), later); err != nil {
		t.Fatalf("AddTokens: %v", err)
	}
	got, err := repo.FindToken(ctx, "next-access", later)
	if err != nil || got == nil {
		t.Fatalf("FindToken(next) = %v, %v", got, err)
	}
	if !got.Grant.LastUsedAt.Equal(later) {
		t.Errorf("LastUsedAt = %v, want %v", got.Grant.LastUsedAt, later)
	}

	// 取り消した許可には足さない。書かれていないことも見る。
	if err := repo.DeleteGrant(ctx, "g1"); err != nil {
		t.Fatalf("DeleteGrant: %v", err)
	}
	err = repo.AddTokens(ctx, "g1", tokenPair("ghost", later), later)
	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("取り消した許可への AddTokens = %v, want ErrNotFound", err)
	}
	if got, _ := repo.FindToken(ctx, "ghost-access", later); got != nil {
		t.Error("取り消した許可にトークンが書かれている")
	}
}

func TestDeleteGrant_RemovesTokens(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	ctx := t.Context()
	seedGrant(t, repo, "g1", user.ID, baseTime)

	if err := repo.DeleteGrant(ctx, "g1"); err != nil {
		t.Fatalf("DeleteGrant: %v", err)
	}
	for _, hash := range []string{"g1-access", "g1-refresh"} {
		if got, err := repo.FindToken(ctx, hash, baseTime); err != nil || got != nil {
			t.Errorf("FindToken(%s) = %v, %v; want nil, nil", hash, got, err)
		}
	}
	if got, err := repo.UseRefreshToken(ctx, "g1-refresh", baseTime); err != nil || got != nil {
		t.Errorf("UseRefreshToken = %v, %v; want nil, nil", got, err)
	}
	// 無いものを消しても誤りにしない。
	if err := repo.DeleteGrant(ctx, "g1"); err != nil {
		t.Errorf("DeleteGrant(2) = %v", err)
	}
}

func TestListGrants_ScopedToUserNewestFirst(t *testing.T) {
	t.Parallel()

	repo, db, user := newGrants(t)
	ctx := t.Context()
	other, err := newSessions(t, db).UpsertUser(ctx, port.User{
		Provider: "github", Subject: "99", Login: "other", DisplayName: "Other",
		CreatedAt: baseTime, UpdatedAt: baseTime,
	})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	seedGrant(t, repo, "old", user.ID, baseTime)
	seedGrant(t, repo, "new", user.ID, baseTime.Add(time.Minute))
	seedGrant(t, repo, "theirs", other.ID, baseTime.Add(2*time.Minute))

	mine, err := repo.ListGrants(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if ids := grantIDs(mine); len(ids) != 2 || ids[0] != "new" || ids[1] != "old" {
		t.Errorf("ListGrants = %v, want [new old]", ids)
	}

	all, err := repo.ListAllGrants(ctx)
	if err != nil {
		t.Fatalf("ListAllGrants: %v", err)
	}
	if ids := grantIDs(all); len(ids) != 3 || ids[0] != "theirs" || ids[1] != "new" || ids[2] != "old" {
		t.Errorf("ListAllGrants = %v, want [theirs new old]", ids)
	}

	none, err := repo.ListGrants(ctx, "")
	if err != nil || len(none) != 0 {
		t.Errorf("ListGrants(\"\") = %v, %v; want 空（全員ぶんではない）", grantIDs(none), err)
	}
}

// 期限が切れてトークンが 1 本も残っていない許可は、一覧に残しても使えない。
func TestCreateGrant_PrunesDeadGrants(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	ctx := t.Context()
	seedGrant(t, repo, "dead", user.ID, baseTime)
	seedGrant(t, repo, "alive", user.ID, baseTime.Add(29*24*time.Hour))

	// dead の refresh token が切れたあとに、別の許可を作る。
	seedGrant(t, repo, "new", user.ID, baseTime.Add(30*24*time.Hour))

	got, err := repo.ListGrants(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if ids := grantIDs(got); len(ids) != 2 || ids[0] != "new" || ids[1] != "alive" {
		t.Errorf("ListGrants = %v, want [new alive]", ids)
	}
}

func TestFindGrant(t *testing.T) {
	t.Parallel()

	repo, _, user := newGrants(t)
	seedGrant(t, repo, "g1", user.ID, baseTime)

	got, err := repo.FindGrant(t.Context(), "g1")
	if err != nil || got == nil {
		t.Fatalf("FindGrant = %v, %v", got, err)
	}
	if got.UserID != user.ID || got.ClientName != "Claude Code" || !got.CreatedAt.Equal(baseTime) {
		t.Errorf("FindGrant = %+v", *got)
	}
	missing, err := repo.FindGrant(t.Context(), "nope")
	if err != nil || missing != nil {
		t.Errorf("FindGrant(nope) = %v, %v; want nil, nil", missing, err)
	}
}

// 利用者を消したら、その人の許可とトークンも消える。残ると、誰のものでもない
// トークンが `/mcp` を通る。
func TestOAuthGrants_CascadeOnUserDelete(t *testing.T) {
	t.Parallel()

	repo, db, user := newGrants(t)
	ctx := t.Context()
	seedGrant(t, repo, "g1", user.ID, baseTime)
	seedCode(t, repo, "code", user.ID)

	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if got, err := repo.FindToken(ctx, "g1-access", baseTime); err != nil || got != nil {
		t.Errorf("FindToken = %v, %v; want nil, nil", got, err)
	}
	if got, err := repo.ConsumeCode(ctx, "code", "g", baseTime); err != nil || got != nil {
		t.Errorf("ConsumeCode = %v, %v; want nil, nil", got, err)
	}
}

func grantIDs(gs []port.OAuthGrant) []string {
	ids := make([]string, 0, len(gs))
	for _, g := range gs {
		ids = append(ids, g.ID)
	}
	return ids
}
