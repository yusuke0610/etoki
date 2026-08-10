package sqlite_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/secret"
	"github.com/yusuke0610/etoki/port"
)

func newBox(t *testing.T, seed byte) secret.Box {
	t.Helper()

	key := make([]byte, secret.KeySize)
	for i := range key {
		key[i] = seed + byte(i)
	}

	b, err := secret.New(key)
	if err != nil {
		t.Fatalf("secret.New: %v", err)
	}
	return b
}

func newSessions(t *testing.T, db *sql.DB) *sqlite.SessionRepository {
	t.Helper()
	return sqlite.NewSessionRepository(db, newBox(t, 7))
}

func seedUser(t *testing.T, repo *sqlite.SessionRepository) port.User {
	t.Helper()

	u, err := repo.UpsertUser(t.Context(), port.User{
		Provider: "github", Subject: "42", Login: "octocat", DisplayName: "Octo Cat",
		CreatedAt: baseTime, UpdatedAt: baseTime,
	})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	return u
}

// login は改名で変わる。同定は (provider, subject) で行い、id は保たれる。
// ここで id を振り直すと、ボードの所有者（PR-C）を見失う。
func TestUpsertUser_KeepsIDAcrossRename(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))
	first := seedUser(t, repo)

	later := baseTime.Add(time.Hour)
	second, err := repo.UpsertUser(t.Context(), port.User{
		Provider: "github", Subject: "42", Login: "renamed", DisplayName: "Renamed",
		CreatedAt: later, UpdatedAt: later,
	})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("ID = %q, want %q（改名で振り直してはいけない）", second.ID, first.ID)
	}
	if second.Login != "renamed" || second.DisplayName != "Renamed" {
		t.Errorf("表示が更新されていない: %+v", second)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", second.CreatedAt, first.CreatedAt)
	}
}

func TestFindUser(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))
	u := seedUser(t, repo)

	got, err := repo.FindUser(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("FindUser: %v", err)
	}
	if got == nil || got.Login != "octocat" {
		t.Fatalf("FindUser() = %+v", got)
	}

	if got, err = repo.FindUser(t.Context(), "missing"); err != nil || got != nil {
		t.Fatalf("FindUser(missing) = (%+v, %v), want (nil, nil)", got, err)
	}
}

func TestSession_RoundTrip(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))
	u := seedUser(t, repo)

	s := port.Session{
		TokenHash: "hash-1", UserID: u.ID,
		CreatedAt: baseTime, ExpiresAt: baseTime.Add(time.Hour),
	}
	if err := repo.CreateSession(t.Context(), s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := repo.FindSession(t.Context(), "hash-1", baseTime)
	if err != nil {
		t.Fatalf("FindSession: %v", err)
	}
	if got == nil || got.UserID != u.ID {
		t.Fatalf("FindSession() = %+v", got)
	}

	if err := repo.DeleteSession(t.Context(), "hash-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got, err = repo.FindSession(t.Context(), "hash-1", baseTime); err != nil || got != nil {
		t.Fatalf("削除後の FindSession() = (%+v, %v), want (nil, nil)", got, err)
	}
}

// 期限切れは存在しないものとして扱う。
func TestFindSession_IgnoresExpired(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))
	u := seedUser(t, repo)

	if err := repo.CreateSession(t.Context(), port.Session{
		TokenHash: "hash-1", UserID: u.ID,
		CreatedAt: baseTime, ExpiresAt: baseTime.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := repo.FindSession(t.Context(), "hash-1", baseTime.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("FindSession: %v", err)
	}
	if got != nil {
		t.Errorf("期限切れが返っている: %+v", got)
	}
}

// 存在しないセッションの削除は誤りにしない。ログアウトを冪等にするため。
func TestDeleteSession_IsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))

	if err := repo.DeleteSession(t.Context(), "never-existed"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
}

func TestCredentials_RoundTrip(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := newSessions(t, db)
	u := seedUser(t, repo)

	want := port.Credentials{
		AccessToken:      "ghu_secret_access",
		RefreshToken:     "ghr_secret_refresh",
		ExpiresAt:        baseTime.Add(8 * time.Hour),
		RefreshExpiresAt: baseTime.Add(180 * 24 * time.Hour),
	}
	if err := repo.SaveCredentials(t.Context(), u.ID, want, baseTime); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	got, err := repo.FindCredentials(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("FindCredentials: %v", err)
	}
	if got == nil {
		t.Fatal("FindCredentials() = nil")
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Errorf("トークンが復元できていない: %+v", got)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) || !got.RefreshExpiresAt.Equal(want.RefreshExpiresAt) {
		t.Errorf("期限が復元できていない: %+v", got)
	}
}

// 共有サーバーになる前提なので平文で置かない（ADR 0015）。
func TestSaveCredentials_StoresCiphertext(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := newSessions(t, db)
	u := seedUser(t, repo)

	const token = "ghu_must_not_be_readable"
	if err := repo.SaveCredentials(t.Context(), u.ID,
		port.Credentials{AccessToken: token, RefreshToken: "ghr_x"}, baseTime); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	var stored, storedRefresh string
	if err := db.QueryRowContext(t.Context(),
		`SELECT access_token, refresh_token FROM github_tokens WHERE user_id = ?`,
		u.ID).Scan(&stored, &storedRefresh); err != nil {
		t.Fatalf("select: %v", err)
	}

	if strings.Contains(stored, token) {
		t.Error("access token が平文で保存されている")
	}
	if strings.Contains(storedRefresh, "ghr_x") {
		t.Error("refresh token が平文で保存されている")
	}
}

// 鍵を変えたら開けない。中身を推測して復旧しようとせず、エラーにする。
func TestFindCredentials_FailsWithDifferentKey(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	u := seedUser(t, newSessions(t, db))

	if err := newSessions(t, db).SaveCredentials(t.Context(), u.ID,
		port.Credentials{AccessToken: "ghu_1"}, baseTime); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	other := sqlite.NewSessionRepository(db, newBox(t, 200))
	if _, err := other.FindCredentials(t.Context(), u.ID); err == nil {
		t.Fatal("FindCredentials() = nil, want error")
	}
}

// 失効しない構成では refresh token と期限が空になる。
func TestCredentials_HandlesNonExpiring(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))
	u := seedUser(t, repo)

	if err := repo.SaveCredentials(t.Context(), u.ID,
		port.Credentials{AccessToken: "ghu_forever"}, baseTime); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	got, err := repo.FindCredentials(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("FindCredentials: %v", err)
	}
	if got.AccessToken != "ghu_forever" || got.RefreshToken != "" {
		t.Errorf("Credentials = %+v", got)
	}
	if !got.ExpiresAt.IsZero() || !got.RefreshExpiresAt.IsZero() {
		t.Errorf("期限が入っている: %+v", got)
	}
	if got.Refreshable() {
		t.Error("失効しない資格情報が Expiring() を返している")
	}
}

func TestSaveCredentials_Replaces(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))
	u := seedUser(t, repo)

	for _, token := range []string{"ghu_1", "ghu_2"} {
		if err := repo.SaveCredentials(t.Context(), u.ID,
			port.Credentials{AccessToken: token}, baseTime); err != nil {
			t.Fatalf("SaveCredentials: %v", err)
		}
	}

	got, err := repo.FindCredentials(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("FindCredentials: %v", err)
	}
	if got.AccessToken != "ghu_2" {
		t.Errorf("AccessToken = %q, want ghu_2", got.AccessToken)
	}
}

func TestFindCredentials_MissingIsNotAnError(t *testing.T) {
	t.Parallel()

	got, err := newSessions(t, newDB(t)).FindCredentials(t.Context(), "nobody")
	if err != nil || got != nil {
		t.Fatalf("FindCredentials() = (%+v, %v), want (nil, nil)", got, err)
	}
}

// state は単回使用。SELECT してから DELETE すると、同じ state で 2 本同時に
// 来たときに両方通る。
func TestConsumeState_IsSingleUse(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))

	if err := repo.SaveState(t.Context(), "state-1", baseTime, baseTime.Add(time.Minute)); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	ok, err := repo.ConsumeState(t.Context(), "state-1", baseTime)
	if err != nil || !ok {
		t.Fatalf("1 回目の ConsumeState() = (%v, %v), want (true, nil)", ok, err)
	}

	if ok, err = repo.ConsumeState(t.Context(), "state-1", baseTime); err != nil || ok {
		t.Fatalf("2 回目の ConsumeState() = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestConsumeState_RejectsExpiredAndUnknown(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))

	if err := repo.SaveState(t.Context(), "old", baseTime, baseTime.Add(time.Minute)); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	ok, err := repo.ConsumeState(t.Context(), "old", baseTime.Add(time.Hour))
	if err != nil || ok {
		t.Errorf("期限切れの ConsumeState() = (%v, %v), want (false, nil)", ok, err)
	}

	if ok, err = repo.ConsumeState(t.Context(), "never-issued", baseTime); err != nil || ok {
		t.Errorf("未知の ConsumeState() = (%v, %v), want (false, nil)", ok, err)
	}
}

// 利用者を消したら、そのセッションと資格情報も消える。取り残すと、
// 消したはずの利用者のトークンが DB に残り続ける。
func TestDeletingUserCascades(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := newSessions(t, db)
	u := seedUser(t, repo)

	if err := repo.CreateSession(t.Context(), port.Session{
		TokenHash: "hash-1", UserID: u.ID,
		CreatedAt: baseTime, ExpiresAt: baseTime.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := repo.SaveCredentials(t.Context(), u.ID,
		port.Credentials{AccessToken: "ghu_1"}, baseTime); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	if _, err := db.ExecContext(t.Context(), `DELETE FROM users WHERE id = ?`, u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	session, err := repo.FindSession(t.Context(), "hash-1", baseTime)
	if err != nil {
		t.Fatalf("FindSession: %v", err)
	}
	if session != nil {
		t.Error("利用者を消してもセッションが残っている")
	}

	creds, err := repo.FindCredentials(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("FindCredentials: %v", err)
	}
	if creds != nil {
		t.Error("利用者を消しても資格情報が残っている")
	}
}

// ---------------------------------------------------------------------------
// メンバーシップによる絞り込み（ADR 0016 / 0017）
// ---------------------------------------------------------------------------

func seedOwnedBoard(t *testing.T, db *sql.DB, id, owner string) {
	t.Helper()

	err := sqlite.NewBoardRepository(db).Create(t.Context(), port.Board{
		ID: id, Name: "board " + id, Scene: `{"elements":[]}`,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, owner)
	if err != nil {
		t.Fatalf("seed board: %v", err)
	}
}

// メンバーでないボードは「存在しない」ものとして扱う。権限エラーと区別すると、
// ID を総当たりして他人のボードの存在を確かめられる。
func TestBoards_AreInvisibleToOtherOwners(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-a", "user-a")

	got, err := repo.Find(t.Context(), "user-b", "board-a")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != nil {
		t.Errorf("他人のボードが見えている: %+v", got)
	}

	// 所有者本人には見える。作った人は owner のメンバーになる。
	got, err = repo.Find(t.Context(), "user-a", "board-a")
	if err != nil || got == nil {
		t.Fatalf("Find(所有者) = (%+v, %v), want ボード", got, err)
	}
	if got.Role != port.RoleOwner {
		t.Errorf("Role = %q, want %q", got.Role, port.RoleOwner)
	}
}

func TestList_ReturnsOnlyOwnBoards(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-a", "user-a")
	seedOwnedBoard(t, db, "board-b", "user-b")
	seedOwnedBoard(t, db, "board-legacy", "")

	got, err := repo.List(t.Context(), "user-a")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Board.ID != "board-a" {
		t.Fatalf("List(user-a) = %+v, want board-a だけ", got)
	}
}

// UpdateScene は Find を通らずに直接 UPDATE する経路。絞り忘れると他人の
// ボードを書き換えられる。
func TestUpdateScene_RejectsOtherOwners(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-a", "user-a")

	err := repo.UpdateScene(t.Context(), "user-b", "board-a", `{"elements":["tampered"]}`, baseTime)
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UpdateScene = %v, want ErrNotFound", err)
	}

	got, err := repo.Find(t.Context(), "user-a", "board-a")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Scene != `{"elements":[]}` {
		t.Errorf("他人に書き換えられている: %s", got.Board.Scene)
	}
}

func TestUpdateTarget_RejectsOtherOwners(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-a", "user-a")

	target := port.BoardTarget{RepositoryOwner: "evil", RepositoryName: "repo", ProjectID: "PVT_x"}
	if err := repo.UpdateTarget(t.Context(), "user-b", "board-a", target, baseTime); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UpdateTarget = %v, want ErrNotFound", err)
	}

	got, err := repo.Find(t.Context(), "user-a", "board-a")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Target.Selected() {
		t.Errorf("他人に作成先を設定されている: %+v", got.Board.Target)
	}
}

// 認証を設定していない構成は、空文字の所有者 1 人として動く。
func TestUnauthenticatedOwnerSeesLegacyBoards(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-legacy", "")

	got, err := repo.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List(\"\") = %d 件, want 1", len(got))
	}
}

func TestClaimUnowned(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-legacy-1", "")
	seedOwnedBoard(t, db, "board-legacy-2", "")
	seedOwnedBoard(t, db, "board-owned", "user-b")

	// 認証なしの利用者が誰かのボードに招かれている行。数えないものは
	// 引き受けもしない。数と述語がずれると、owner のつもりで viewer の行を
	// 受け取ることになる。
	if err := repo.AddMember(t.Context(), port.BoardMember{
		BoardID: "board-owned", UserID: "", Role: port.RoleViewer, CreatedAt: baseTime,
	}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	n, err := repo.CountUnowned(t.Context())
	if err != nil {
		t.Fatalf("CountUnowned: %v", err)
	}
	if n != 2 {
		t.Fatalf("CountUnowned = %d, want 2", n)
	}

	claimed, err := repo.ClaimUnowned(t.Context(), "user-a")
	if err != nil {
		t.Fatalf("ClaimUnowned: %v", err)
	}
	if claimed != 2 {
		t.Errorf("ClaimUnowned = %d, want 2", claimed)
	}

	got, err := repo.List(t.Context(), "user-a")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("引き受け後の List = %d 件, want 2", len(got))
	}

	// 他人のボードは巻き込まない。
	other, err := repo.List(t.Context(), "user-b")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(other) != 1 {
		t.Errorf("他人のボードを巻き込んでいる: %+v", other)
	}

	if n, err = repo.CountUnowned(t.Context()); err != nil || n != 0 {
		t.Errorf("CountUnowned = (%d, %v), want (0, nil)", n, err)
	}
}

// 空文字は「所有者が無い」そのもの。引き受けたことにならない。
func TestClaimUnowned_RejectsEmptyOwner(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedOwnedBoard(t, db, "board-legacy", "")

	if _, err := sqlite.NewBoardRepository(db).ClaimUnowned(t.Context(), ""); err == nil {
		t.Fatal("ClaimUnowned(\"\") = nil, want error")
	}
}

func TestFindUserByLogin(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))
	want := seedUser(t, repo)

	got, err := repo.FindUserByLogin(t.Context(), "github", "octocat")
	if err != nil {
		t.Fatalf("FindUserByLogin: %v", err)
	}
	if got == nil || got.ID != want.ID {
		t.Fatalf("FindUserByLogin() = %+v, want %+v", got, want)
	}

	if got, err = repo.FindUserByLogin(t.Context(), "github", "nobody"); err != nil || got != nil {
		t.Errorf("FindUserByLogin(未知) = (%+v, %v), want (nil, nil)", got, err)
	}
}
