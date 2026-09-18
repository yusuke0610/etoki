package sqlite_test

import (
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/secret"
	"github.com/yusuke0610/etoki/migrations"
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

	// 版は合っている。落ちる理由がメンバーでないことだけになるようにする。
	err := repo.UpdateScene(t.Context(), "user-b", "board-a", `{"elements":["tampered"]}`,
		baseTime, baseTime.Add(time.Hour))
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UpdateScene = %v, want ErrNotFound", err)
	}
	// 非メンバーには「無い」としか返さない。版の食い違いとして返すと、
	// 書けなかった理由からボードの存在を確かめられる（ADR 0016 / 0017）。
	if errors.Is(err, port.ErrConflict) {
		t.Error("非メンバーに版の食い違いを返している")
	}

	got, err := repo.Find(t.Context(), "user-a", "board-a")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Scene != `{"elements":[]}` {
		t.Errorf("他人に書き換えられている: %s", got.Board.Scene)
	}
}

// 表示名の取り直しも同じ絞りを通る。固定後に通る経路なので、ここが
// 抜けていると他人のボードの見え方を書き換えられる（ADR 0037）。
func TestUpdateTargetDisplay_RejectsOtherOwners(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-a", "user-a")

	display := port.BoardTargetDisplay{ProjectTitle: "書き換えた名前"}
	if err := repo.UpdateTargetDisplay(
		t.Context(), "user-b", "board-a", display, baseTime,
	); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UpdateTargetDisplay = %v, want ErrNotFound", err)
	}

	got, err := repo.Find(t.Context(), "user-a", "board-a")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Target.ProjectTitle != "" {
		t.Errorf("他人に表示名を書き換えられている: %+v", got.Board.Target)
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

// upsertAs は subject を固定して login だけを変えながらログインさせる。
func upsertAs(
	t *testing.T, repo *sqlite.SessionRepository, subject, login string, at time.Time,
) port.User {
	t.Helper()

	u, err := repo.UpsertUser(t.Context(), port.User{
		Provider: "github", Subject: subject, Login: login, DisplayName: login,
		CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatalf("UpsertUser(%s, %s): %v", subject, login, err)
	}
	return u
}

// 改名で空いた login を別人が取った（#142）。A は bob から改名したが、etoki には
// 入り直していないので、手元の行は bob のまま残る。**B がログインした時点で
// bob を引いたら B が返らなければならない。** A が返ると、B のつもりの招待や
// claim で A に権限が渡る。
func TestFindUserByLogin_PrefersTheLatestHolderAfterRename(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))
	old := upsertAs(t, repo, "1", "bob", baseTime)
	fresh := upsertAs(t, repo, "2", "bob", baseTime.Add(time.Hour))

	got, err := repo.FindUserByLogin(t.Context(), "github", "bob")
	if err != nil {
		t.Fatalf("FindUserByLogin: %v", err)
	}
	if got == nil || got.ID != fresh.ID {
		t.Fatalf("FindUserByLogin(bob) = %+v, want %s（最後にログインした bob）", got, fresh.ID)
	}

	// 古い行は消さない。ボードの所有者やメンバーが指している（ID は保つ）。
	// login だけを外すので、A が入り直せば新しい login で引けるようになる。
	prev, err := repo.FindUser(t.Context(), old.ID)
	if err != nil {
		t.Fatalf("FindUser: %v", err)
	}
	if prev == nil || prev.Login != "" {
		t.Errorf("以前の持ち主 = %+v, want login を外した行", prev)
	}
	if !prev.UpdatedAt.Equal(baseTime) {
		t.Errorf("以前の持ち主の UpdatedAt = %v, want %v（最後にログインした時刻は動かさない）",
			prev.UpdatedAt, baseTime)
	}

	renamed := upsertAs(t, repo, "1", "bob2", baseTime.Add(2*time.Hour))
	if got, _ := repo.FindUserByLogin(t.Context(), "github", "bob2"); got == nil || got.ID != renamed.ID {
		t.Errorf("入り直した A を新しい login で引けない: %+v", got)
	}
}

// GitHub の login は大文字小文字を区別しない。区別すると、Alice でログインした
// 人を alice で招待したときに「まだログインしていない」と断られ、owner は相手に
// 頼みに行く（#142）。
func TestFindUserByLogin_IgnoresCase(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))
	want := upsertAs(t, repo, "1", "Alice", baseTime)

	got, err := repo.FindUserByLogin(t.Context(), "github", "alice")
	if err != nil {
		t.Fatalf("FindUserByLogin: %v", err)
	}
	if got == nil || got.ID != want.ID {
		t.Fatalf("FindUserByLogin(alice) = %+v, want %s", got, want.ID)
	}

	// 大文字小文字だけ違う login を別人が取っても、同じ規則で最後の 1 人に絞る。
	other := upsertAs(t, repo, "2", "ALICE", baseTime.Add(time.Hour))
	if got, _ := repo.FindUserByLogin(t.Context(), "github", "Alice"); got == nil || got.ID != other.ID {
		t.Errorf("FindUserByLogin(Alice) = %+v, want %s", got, other.ID)
	}
}

// 外した login（空文字）で引けてはいけない。空の入力が、login を外した行の
// どれかに当たる。
func TestFindUserByLogin_EmptyLoginFindsNobody(t *testing.T) {
	t.Parallel()

	repo := newSessions(t, newDB(t))
	upsertAs(t, repo, "1", "bob", baseTime)
	upsertAs(t, repo, "2", "bob", baseTime.Add(time.Hour))

	got, err := repo.FindUserByLogin(t.Context(), "github", "")
	if err != nil || got != nil {
		t.Errorf("FindUserByLogin(\"\") = (%+v, %v), want (nil, nil)", got, err)
	}
}

// 0012 より前に、改名で同じ login の行が 2 つ（大文字小文字違いを含む）
// できていた DB。移行で最後にログインした 1 行だけに login を残し、以後の
// 一意索引が張れる状態にする。
func TestMigrate_KeepsLoginOnlyOnTheLatestHolder(t *testing.T) {
	t.Parallel()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(t.Context(),
		`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`,
	); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	sort.Strings(names)
	for _, name := range names {
		if name >= "0012" {
			break
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := db.ExecContext(t.Context(), string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := db.ExecContext(t.Context(),
			`INSERT INTO schema_migrations VALUES (?, '2026-01-01T00:00:00Z')`, name,
		); err != nil {
			t.Fatalf("record %s: %v", name, err)
		}
	}

	for _, row := range [][]string{
		{"old", "1", "bob", "2026-01-01T00:00:00Z"},
		{"new", "2", "Bob", "2026-02-01T00:00:00Z"},
		{"solo", "3", "carol", "2026-01-01T00:00:00Z"},
		// 同じ秒の中の前後。RFC3339Nano は末尾の 0 を落とすので、文字列で比べると
		// 00.5Z が 00Z より前に並ぶ。
		{"sec-old", "4", "dave", "2026-03-01T00:00:00Z"},
		{"sec-new", "5", "dave", "2026-03-01T00:00:00.5Z"},
	} {
		if _, err := db.ExecContext(t.Context(),
			`INSERT INTO users (id, provider, subject, login, display_name, created_at, updated_at)
			 VALUES (?, 'github', ?, ?, '', ?, ?)`, row[0], row[1], row[2], row[3], row[3],
		); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	repo := newSessions(t, db)
	for id, want := range map[string]string{
		"old": "", "new": "Bob", "solo": "carol", "sec-old": "", "sec-new": "dave",
	} {
		u, err := repo.FindUser(t.Context(), id)
		if err != nil || u == nil {
			t.Fatalf("FindUser(%s) = (%+v, %v)", id, u, err)
		}
		if u.Login != want {
			t.Errorf("%s の login = %q, want %q", id, u.Login, want)
		}
	}

	// 以後は同じ login を 2 行に持たせられない。
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO users (id, provider, subject, login, display_name, created_at, updated_at)
		 VALUES ('dup', 'github', '9', 'CAROL', '', '2026-03-01T00:00:00Z', '2026-03-01T00:00:00Z')`,
	); err == nil {
		t.Error("大文字小文字違いの同じ login を直接 INSERT できた")
	}
}

// メンバーの読み書きは SQLite 側にしか無い。ユースケースのフェイクでは
// 主キーの衝突も対象の不在も再現できないので、ここで実体に当てる。
func TestBoardMembers_RoundTrip(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-a", "user-a")

	err := repo.AddMember(t.Context(), port.BoardMember{
		BoardID: "board-a", UserID: "user-b", Role: port.RoleEditor,
		CreatedAt: baseTime.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	// 並びは古い順。作った人が先頭に来る。
	got, err := repo.ListMembers(t.Context(), "board-a")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListMembers = %d 件, want 2", len(got))
	}
	if got[0].UserID != "user-a" || got[0].Role != port.RoleOwner {
		t.Errorf("先頭 = %+v, want user-a の owner", got[0])
	}
	if got[1].UserID != "user-b" || got[1].Role != port.RoleEditor {
		t.Errorf("2 件目 = %+v, want user-b の editor", got[1])
	}
	if !got[1].CreatedAt.Equal(baseTime.Add(time.Minute)) {
		t.Errorf("CreatedAt = %v, want %v", got[1].CreatedAt, baseTime.Add(time.Minute))
	}

	if err := repo.UpdateMemberRole(t.Context(), "board-a", "user-b", port.RoleViewer); err != nil {
		t.Fatalf("UpdateMemberRole: %v", err)
	}
	if got, err = repo.ListMembers(t.Context(), "board-a"); err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if got[1].Role != port.RoleViewer {
		t.Errorf("更新後の Role = %q, want %q", got[1].Role, port.RoleViewer)
	}

	if err := repo.RemoveMember(t.Context(), "board-a", "user-b"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if got, err = repo.ListMembers(t.Context(), "board-a"); err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(got) != 1 || got[0].UserID != "user-a" {
		t.Errorf("外した後の ListMembers = %+v", got)
	}
}

// 黙って上書きすると、呼び出し側が「招待した」と「ロールを変えた」を
// 区別できなくなる。
func TestAddMember_RejectsDuplicate(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-a", "user-a")

	err := repo.AddMember(t.Context(), port.BoardMember{
		BoardID: "board-a", UserID: "user-a", Role: port.RoleViewer, CreatedAt: baseTime,
	})
	if !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("AddMember(重複) = %v, want ErrAlreadyExists", err)
	}

	// ロールは書き換わっていない。
	got, err := repo.ListMembers(t.Context(), "board-a")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(got) != 1 || got[0].Role != port.RoleOwner {
		t.Errorf("ListMembers = %+v, want user-a の owner 1 件", got)
	}
}

// 存在しない行への更新が黙って成功すると、呼び出し側は保存できたと思い込む。
func TestBoardMembers_MissingIsNotFound(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-a", "user-a")

	err := repo.UpdateMemberRole(t.Context(), "board-a", "user-x", port.RoleViewer)
	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("UpdateMemberRole(未知) = %v, want ErrNotFound", err)
	}

	if err := repo.RemoveMember(t.Context(), "board-a", "user-x"); !errors.Is(err, port.ErrNotFound) {
		t.Errorf("RemoveMember(未知) = %v, want ErrNotFound", err)
	}
}

// 無いボードへの招待は外部キーで弾く。通ると、ボードを消したあとに残る
// メンバー行と同じ「指し先の無い行」を自分で作ることになる。
func TestAddMember_RejectsUnknownBoard(t *testing.T) {
	t.Parallel()

	repo := sqlite.NewBoardRepository(newDB(t))

	err := repo.AddMember(t.Context(), port.BoardMember{
		BoardID: "no-such-board", UserID: "user-a",
		Role: port.RoleEditor, CreatedAt: baseTime,
	})
	if err == nil {
		t.Fatal("AddMember: want foreign key error, got nil")
	}
}
