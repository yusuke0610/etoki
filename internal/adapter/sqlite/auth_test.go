package sqlite_test

import (
	"database/sql"
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
	if got.Expiring() {
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
