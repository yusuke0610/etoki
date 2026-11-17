package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 利用者が自分を消す口（ADR 0101）。ここで見るのは HTTP の面で、どれを消すかの
// 判定そのものはユースケース層のテスト（account_test.go）が持つ。

// accountRouter は認証つきで、利用者を消す口まで組み立てたルーターを返す。
func accountRouter(t *testing.T) (*gin.Engine, *stubProvider, port.BoardRepository) {
	t.Helper()

	provider := &stubProvider{}
	deps, _, db := newAuthDeps(t, provider)
	boards := sqlite.NewBoardRepository(db)
	// ボードを消すのは deps.Boards に任せる。etoki.New と同じ組み立て。
	deps.Accounts = usecase.NewAccountService(boards, deps.Boards, sqlite.NewAccountRepository(db))
	return httpapi.NewRouter(deps), provider, boards
}

// userIDOf はボードのただ 1 人のメンバー（作った本人）の ID を返す。メンバーを
// 直に足すのに要る。
func userIDOf(t *testing.T, r *gin.Engine, cookie *http.Cookie, boardID string) string {
	t.Helper()
	members := decodeOK[[]apitypes.BoardMember](t,
		withCookie(t, r, http.MethodGet, "/api/boards/"+boardID+"/members", cookie))
	if len(members) != 1 {
		t.Fatalf("members = %+v, want 1", members)
	}
	return members[0].UserID
}

func addMember(t *testing.T, boards port.BoardRepository, boardID, userID string, role port.BoardRole) {
	t.Helper()
	if err := boards.AddMember(t.Context(), port.BoardMember{
		BoardID: boardID, UserID: userID, Role: role, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
}

// 消える前に見せたものと、消したあとに残るものを通しで見る。
//
//   - alice だけのボード、alice が owner で bob が参加するボード → 消える
//   - bob が owner で alice が参加するボード → alice が外れるだけ
func TestAccount_DeletesOwnBoardsAndLeavesOthers(t *testing.T) {
	t.Parallel()

	r, provider, boards := accountRouter(t)
	bob := signInAs(t, r, provider, "2", "bob")
	bobsBoard := createSharedBoard(t, r, bob, "bob のボード")
	bobID := userIDOf(t, r, bob, bobsBoard)

	alice := signInAs(t, r, provider, "1", "alice")
	solo := createSharedBoard(t, r, alice, "ひとりのボード")
	shared := createSharedBoard(t, r, alice, "共有のボード")
	aliceID := userIDOf(t, r, alice, solo)
	addMember(t, boards, shared, bobID, port.RoleEditor)
	addMember(t, boards, bobsBoard, aliceID, port.RoleEditor)

	got := decodeOK[apitypes.AccountDeletion](t,
		withCookie(t, r, http.MethodGet, "/api/account/deletion", alice))
	if got.LeftBoardCount != 1 {
		t.Errorf("leftBoardCount = %d, want 1（bob のボード）", got.LeftBoardCount)
	}
	others := map[string]int{}
	for _, b := range got.Boards {
		others[b.Name] = b.OtherMemberCount
	}
	if len(got.Boards) != 2 || others["ひとりのボード"] != 0 || others["共有のボード"] != 1 {
		t.Errorf("boards = %+v, want ひとり（ほか 0 人）と共有（ほか 1 人）", got.Boards)
	}

	rec := doJSON(t, r, http.MethodDelete, "/api/account", alice, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	// **cookie も消す。** 残すと、画面が次に送る要求まで消えたセッションを名乗る。
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "etoki_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("セッション cookie を消していない: %v", rec.Result().Cookies())
	}

	// 消えたセッションでは、もうログインしていない。
	session := decodeOK[apitypes.SessionStatus](t,
		withCookie(t, r, http.MethodGet, "/api/auth/session", alice))
	if session.Authenticated {
		t.Error("消した利用者のセッションがまだ通る")
	}

	// bob から見ると、alice が owner の共有のボードは消え、bob のボードは残って
	// alice だけが外れている。
	bobsBoards := decodeOK[[]apitypes.BoardSummary](t,
		withCookie(t, r, http.MethodGet, "/api/boards", bob))
	if len(bobsBoards) != 1 || bobsBoards[0].ID != bobsBoard {
		t.Errorf("bob のボード = %+v, want bob のボードだけ", bobsBoards)
	}
	members := decodeOK[[]apitypes.BoardMember](t,
		withCookie(t, r, http.MethodGet, "/api/boards/"+bobsBoard+"/members", bob))
	if len(members) != 1 || members[0].UserID != bobID {
		t.Errorf("bob のボードのメンバー = %+v, want bob だけ", members)
	}
}

// 消す口は状態を変えるので、Origin の検証の内側にある（ADR 0013）。外のページから
// 叩かれても消えない。
func TestAccount_RejectsCrossSiteDelete(t *testing.T) {
	t.Parallel()

	r, provider, _ := accountRouter(t)
	alice := signInAs(t, r, provider, "1", "alice")

	req := map[string]string{"Origin": "https://evil.example", "Cookie": alice.Name + "=" + alice.Value}
	if rec := request(t, r, http.MethodDelete, "/api/account", loopbackHost, req, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body)
	}
	if !decodeOK[apitypes.SessionStatus](t,
		withCookie(t, r, http.MethodGet, "/api/auth/session", alice)).Authenticated {
		t.Error("外のページからの要求で消えた")
	}
}

// 認証なしの構成では、組み立てを渡されても開かない。空文字の利用者 1 人
// （ADR 0016）を消すと、全ボードが消える。
func TestAccount_NotOpenedWithoutAuth(t *testing.T) {
	t.Parallel()

	deps, _, db := newAuthDeps(t, nil)
	boards := sqlite.NewBoardRepository(db)
	accounts := &recordingAccounts{}
	deps.Accounts = usecase.NewAccountService(boards, deps.Boards, accounts)
	r := httpapi.NewRouter(deps)
	createBoard(t, r, "設計会")

	rec := do(t, r, http.MethodDelete, "/api/account", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeAccountDeletionNotConfigured {
		t.Errorf("code = %q", code)
	}
	if n := countBoards(t, r); n != 1 {
		t.Errorf("ボード = %d 枚, want 1（消していない）", n)
	}
	if len(accounts.deleted) != 0 {
		t.Errorf("利用者を消した: %v", accounts.deleted)
	}
	caps := decodeOK[apitypes.Capabilities](t, do(t, r, http.MethodGet, "/api/capabilities", nil))
	if caps.AccountDeletion {
		t.Error("capabilities.accountDeletion = true, want false")
	}
}

// recordingAccounts は消した利用者を覚えるだけの AccountRepository。
type recordingAccounts struct{ deleted []string }

func (a *recordingAccounts) DeleteUser(_ context.Context, userID string) error {
	a.deleted = append(a.deleted, userID)
	return nil
}

// 認証ありで組み立てたら、使える機能に出す。
func TestAccount_CapabilityWhenConfigured(t *testing.T) {
	t.Parallel()

	r, provider, _ := accountRouter(t)
	alice := signInAs(t, r, provider, "1", "alice")

	caps := decodeOK[apitypes.Capabilities](t,
		withCookie(t, r, http.MethodGet, "/api/capabilities", alice))
	if !caps.AccountDeletion {
		t.Error("capabilities.accountDeletion = false, want true")
	}
}

// 文書へのリンクは、ログインの前に読める口に載せる（ADR 0101）。設定していない
// なら省く（空文字を載せると「設定されているが空」に読める）。
func TestSession_ReturnsDocumentLinks(t *testing.T) {
	t.Parallel()

	deps, _, _ := newAuthDeps(t, &stubProvider{})
	deps.TermsURL = "https://etoki.example/terms"
	deps.PrivacyURL = "https://etoki.example/privacy"
	r := httpapi.NewRouter(deps)

	// 未ログインのまま引く。
	got := decodeOK[apitypes.SessionStatus](t, do(t, r, http.MethodGet, "/api/auth/session", nil))
	if got.Authenticated {
		t.Fatal("ログインしていないのに authenticated")
	}
	if got.TermsURL == nil || *got.TermsURL != deps.TermsURL {
		t.Errorf("termsUrl = %v, want %q", got.TermsURL, deps.TermsURL)
	}
	if got.PrivacyURL == nil || *got.PrivacyURL != deps.PrivacyURL {
		t.Errorf("privacyUrl = %v, want %q", got.PrivacyURL, deps.PrivacyURL)
	}

	r, _ = newAuthRouter(t, &stubProvider{})
	raw := decodeOK[map[string]any](t, do(t, r, http.MethodGet, "/api/auth/session", nil))
	for _, key := range []string{"termsUrl", "privacyUrl"} {
		if _, ok := raw[key]; ok {
			t.Errorf("設定していないのに %s を載せている: %v", key, raw)
		}
	}
}
