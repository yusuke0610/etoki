package sqlite_test

import (
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/port"
)

// 利用者を消すと、セッション・資格情報・MCP の許可（コードとトークンを含む）が
// 一緒に消える（ADR 0101）。残ると、消えた利用者のセッションやトークンが生きた
// まま残る。**ほかの利用者のものは残る**ことと、**メンバーの行は消さない**こと
// （片付けるのはユースケース層、ADR 0016）も見る。
func TestDeleteUser_RemovesSessionsCredentialsAndGrants(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := newSessions(t, db)
	grants := sqlite.NewOAuthGrantRepository(db)
	gone := seedUser(t, repo)
	kept, err := repo.UpsertUser(t.Context(), port.User{
		Provider: "github", Subject: "43", Login: "hubot", DisplayName: "Hubot",
		CreatedAt: baseTime, UpdatedAt: baseTime,
	})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	for _, u := range []port.User{gone, kept} {
		if err := repo.CreateSession(t.Context(), port.Session{
			TokenHash: "session-" + u.ID, UserID: u.ID,
			CreatedAt: baseTime, ExpiresAt: baseTime.Add(time.Hour),
		}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if err := repo.SaveCredentials(t.Context(), u.ID, port.Credentials{
			AccessToken: "ghu_" + u.ID,
		}, baseTime); err != nil {
			t.Fatalf("SaveCredentials: %v", err)
		}
		seedCode(t, grants, "code-"+u.ID, u.ID)
		seedGrant(t, grants, "grant-"+u.ID, u.ID, baseTime)
	}
	if err := sqlite.NewBoardRepository(db).Create(t.Context(), port.Board{
		ID: "b1", Name: "共有のボード", Scene: `{"elements":[]}`,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, gone.ID); err != nil {
		t.Fatalf("Create board: %v", err)
	}

	accounts := sqlite.NewAccountRepository(db)
	if err := accounts.DeleteUser(t.Context(), gone.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// 消えたかどうかは、口の返り値ではなく表の中身で見る。期限切れを隠す口
	// （FindSession など）だと、残っていても空に見える。
	byUser := map[string]string{
		"users":         `SELECT COUNT(*) FROM users WHERE id = ?`,
		"sessions":      `SELECT COUNT(*) FROM sessions WHERE user_id = ?`,
		"github_tokens": `SELECT COUNT(*) FROM github_tokens WHERE user_id = ?`,
		"oauth_codes":   `SELECT COUNT(*) FROM oauth_codes WHERE user_id = ?`,
		"oauth_grants":  `SELECT COUNT(*) FROM oauth_grants WHERE user_id = ?`,
	}
	const tokens = `SELECT COUNT(*) FROM oauth_tokens WHERE grant_id = ?`
	for name, q := range byUser {
		if n := countRows(t, db, q, gone.ID); n != 0 {
			t.Errorf("消した利用者の %s が %d 行残っている", name, n)
		}
		// ほかの利用者のものは残る。
		if n := countRows(t, db, q, kept.ID); n != 1 {
			t.Errorf("ほかの利用者の %s = %d 行, want 1", name, n)
		}
	}
	if n := countRows(t, db, tokens, "grant-"+gone.ID); n != 0 {
		t.Errorf("消した利用者のトークンが %d 行残っている", n)
	}
	if n := countRows(t, db, tokens, "grant-"+kept.ID); n != 2 {
		t.Errorf("ほかの利用者のトークン = %d 行, want 2", n)
	}
	// メンバーの行は残す。消すかどうかはユースケース層が決める。
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM board_members WHERE user_id = ?`, gone.ID); n != 1 {
		t.Errorf("メンバーの行 = %d 行, want 1（DeleteUser は触らない）", n)
	}

	// 2 回目も誤りにしない。途中で失敗した削除を、もう一度消して続けられる。
	if err := accounts.DeleteUser(t.Context(), gone.ID); err != nil {
		t.Errorf("2 回目の DeleteUser = %v", err)
	}
}
