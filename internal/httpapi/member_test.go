package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/port"
)

// doJSON は cookie と JSON ボディを付けてリクエストする。
//
// 既存の do はボディを取るが cookie を取らず、withCookie は逆。共有のテストは
// 両方要る。
func doJSON(
	t *testing.T, r *gin.Engine, method, path string, cookie *http.Cookie, body any,
) *httptest.ResponseRecorder {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	// httptest の既定の Host は example.com。指定を忘れると origin.go が 403 で
	// 弾く。
	req.Host = loopbackHost
	if cookie != nil {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// 共有の HTTP 面をひととおり通す。ここで見るのはステータスコードの分け方で、
// ロールの判定そのものはユースケース層のテストが持つ。

// signInAs は identity を差し替えてログインし、cookie を返す。
func signInAs(
	t *testing.T, r *gin.Engine, p *stubProvider, subject, login string,
) *http.Cookie {
	t.Helper()

	p.identity = &port.Identity{
		Provider: "github", Subject: subject, Login: login, DisplayName: login,
	}
	return signIn(t, r)
}

// inviteBody は、画面と同じく先に引き当ててから招待の本文を組み立てる
// （ADR 0053）。引き当てが 200 でなければ落とす。
func inviteBody(
	t *testing.T, r *gin.Engine, cookie *http.Cookie, boardID, login, role string,
) map[string]string {
	t.Helper()

	rec := withCookie(t, r, http.MethodGet, "/api/boards/"+boardID+"/invitee?login="+login, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("lookupInvitee(%s) = %d %s", login, rec.Code, rec.Body)
	}
	got := decode[apitypes.Invitee](t, rec)
	return map[string]string{"login": login, "userId": got.UserID, "role": role}
}

// createSharedBoard はログイン済みの利用者としてボードを 1 枚作り、ID を返す。
//
// cookie を取らない createBoard（router_test.go）とは別に持つ。共有のテストは
// 誰が作ったのかが要る。
func createSharedBoard(t *testing.T, r *gin.Engine, cookie *http.Cookie, name string) string {
	t.Helper()

	rec := doJSON(t, r, http.MethodPost, "/api/boards", cookie, newBoardBody(name))
	if rec.Code != http.StatusCreated {
		t.Fatalf("createBoard: %d %s", rec.Code, rec.Body)
	}

	body := decode[map[string]any](t, rec)
	if body["role"] != string(port.RoleOwner) {
		t.Errorf("作った本人の role = %v, want owner", body["role"])
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("id が返っていない: %s", rec.Body)
	}
	return id
}

// 招待された editor はブレストに参加できる。リポジトリのアクセス権は要らない
// （ADR 0017）。GitHub を設定していないこの構成でも通ることがその証拠になる。
func TestInvitedEditorCanUseBoard(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{}
	r, _ := newAuthRouter(t, provider)

	// 招待できるのは一度ログインした相手だけ。先に bob を作る。
	bob := signInAs(t, r, provider, "2", "bob")
	alice := signInAs(t, r, provider, "1", "alice")

	boardID := createSharedBoard(t, r, alice, "共有するボード")

	// 招待前は「無い」ものとして見える。403 にしない。
	if rec := withCookie(t, r, http.MethodGet, "/api/boards/"+boardID, bob); rec.Code != http.StatusNotFound {
		t.Fatalf("招待前の GET = %d, want 404", rec.Code)
	}

	rec := doJSON(t, r, http.MethodPost, "/api/boards/"+boardID+"/members", alice,
		inviteBody(t, r, alice, boardID, "bob", "editor"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", rec.Code, rec.Body)
	}

	// 招待後は開けて、保存もできる。
	opened := withCookie(t, r, http.MethodGet, "/api/boards/"+boardID, bob)
	if opened.Code != http.StatusOK {
		t.Fatalf("招待後の GET = %d %s", opened.Code, opened.Body)
	}

	// 開いたときの版をそのまま送り返す。実際のクライアントと同じ経路（ADR 0020）。
	base := decode[map[string]any](t, opened)["updatedAt"]
	rec = doJSON(t, r, http.MethodPut, "/api/boards/"+boardID+"/scene", bob,
		saveSceneBody(`{"type":"excalidraw","elements":[]}`, base))
	if rec.Code != http.StatusOK {
		t.Fatalf("editor のシーン保存 = %d %s", rec.Code, rec.Body)
	}

	// 作成先の変更は owner だけ。ボードの存在は知っているので 403。
	rec = doJSON(t, r, http.MethodPut, "/api/boards/"+boardID+"/target", bob,
		map[string]string{"repositoryOwner": "evil", "repositoryName": "repo", "projectId": "PVT_x"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor の作成先変更 = %d %s, want 403", rec.Code, rec.Body)
	}
}

// viewer は読めるが書けない。解釈も許さない（LLM を叩く外部呼び出しのため）。
func TestInvitedViewerCannotWrite(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{}
	r, _ := newAuthRouter(t, provider)

	bob := signInAs(t, r, provider, "2", "bob")
	alice := signInAs(t, r, provider, "1", "alice")
	boardID := createSharedBoard(t, r, alice, "読むだけのボード")

	rec := doJSON(t, r, http.MethodPost, "/api/boards/"+boardID+"/members", alice,
		inviteBody(t, r, alice, boardID, "bob", "viewer"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", rec.Code, rec.Body)
	}

	if rec := withCookie(t, r, http.MethodGet, "/api/boards/"+boardID, bob); rec.Code != http.StatusOK {
		t.Fatalf("viewer の GET = %d %s", rec.Code, rec.Body)
	}
	if rec := withCookie(t, r, http.MethodGet,
		"/api/boards/"+boardID+"/annotations", bob); rec.Code != http.StatusOK {
		t.Fatalf("viewer の注釈一覧 = %d %s", rec.Code, rec.Body)
	}

	// 版は正しい形で送る。基準の欠落で 400 になると、ロールの判定を通っていない
	// まま緑になる。
	rec = doJSON(t, r, http.MethodPut, "/api/boards/"+boardID+"/scene", bob,
		saveSceneBody(`{"type":"excalidraw","elements":[]}`, fixedTime))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer のシーン保存 = %d %s, want 403", rec.Code, rec.Body)
	}
}

// 一度もログインしていない login 宛には招待を積まない。改名で空いた login を
// 取った別人に権限が渡るため（ADR 0017）。
func TestInvite_UnknownLoginIsBadRequest(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{}
	r, _ := newAuthRouter(t, provider)

	alice := signInAs(t, r, provider, "1", "alice")
	boardID := createSharedBoard(t, r, alice, "ボード")

	rec := doJSON(t, r, http.MethodPost, "/api/boards/"+boardID+"/members", alice,
		map[string]string{"login": "carol", "userId": "user-carol", "role": "editor"})
	// 404 にしない。ボードが無いのか相手が居ないのか区別できなくなる。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知の login = %d %s, want 400", rec.Code, rec.Body)
	}
}

// 招待の前に、login が誰に当たるのかを見せる（ADR 0053）。大文字小文字は
// 区別しない。最終ログインは確認で見せる材料なので、値そのものを見る。
func TestLookupInvitee_ReturnsTheLastSignedInUser(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{}
	r, _ := newAuthRouter(t, provider)

	signInAs(t, r, provider, "2", "Bob")
	alice := signInAs(t, r, provider, "1", "alice")
	boardID := createSharedBoard(t, r, alice, "ボード")

	rec := withCookie(t, r, http.MethodGet, "/api/boards/"+boardID+"/invitee?login=bob", alice)
	if rec.Code != http.StatusOK {
		t.Fatalf("lookupInvitee = %d %s", rec.Code, rec.Body)
	}
	got := decode[apitypes.Invitee](t, rec)
	if got.Login != "Bob" || got.DisplayName != "Bob" || got.UserID == "" {
		t.Errorf("Invitee = %+v", got)
	}
	if got.LastSignedInAt.IsZero() {
		t.Errorf("LastSignedInAt が空: %+v", got)
	}

	if rec := withCookie(t, r, http.MethodGet,
		"/api/boards/"+boardID+"/invitee?login=carol", alice); rec.Code != http.StatusBadRequest {
		t.Errorf("未知の login = %d %s, want 400", rec.Code, rec.Body)
	}
}

// 引き当ては owner だけ。招待できない人に、誰がログインしたことがあるかを
// 見せる理由が無い。
func TestLookupInvitee_RequiresOwner(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{}
	r, _ := newAuthRouter(t, provider)

	bob := signInAs(t, r, provider, "2", "bob")
	alice := signInAs(t, r, provider, "1", "alice")
	boardID := createSharedBoard(t, r, alice, "ボード")

	if rec := doJSON(t, r, http.MethodPost, "/api/boards/"+boardID+"/members", alice,
		inviteBody(t, r, alice, boardID, "bob", "editor")); rec.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", rec.Code, rec.Body)
	}

	rec := withCookie(t, r, http.MethodGet, "/api/boards/"+boardID+"/invitee?login=alice", bob)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor の引き当て = %d %s, want 403", rec.Code, rec.Body)
	}
}

// 確認してから押すまでのあいだに、改名で空いた login を別人が取った（#142）。
// 確認で見せた相手と違う人に権限を渡さない。実物の DB で、ログインの時点で
// 引き当てが新しい持ち主に移ることまで通しで見る。
func TestInvite_ConflictsWhenLoginChangedHands(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{}
	r, _ := newAuthRouter(t, provider)

	signInAs(t, r, provider, "2", "bob")
	alice := signInAs(t, r, provider, "1", "alice")
	boardID := createSharedBoard(t, r, alice, "ボード")

	checked := inviteBody(t, r, alice, boardID, "bob", "editor")

	// 以前の bob は改名し、空いた bob を別人がログインして取った。
	newcomer := signInAs(t, r, provider, "3", "bob")

	rec := doJSON(t, r, http.MethodPost, "/api/boards/"+boardID+"/members", alice, checked)
	if rec.Code != http.StatusConflict {
		t.Fatalf("持ち主の変わった招待 = %d %s, want 409", rec.Code, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeInviteeChanged {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeInviteeChanged)
	}

	// 招待は積まれていない。新しい bob も以前の bob も開けない。
	if rec := withCookie(t, r, http.MethodGet, "/api/boards/"+boardID, newcomer); rec.Code != http.StatusNotFound {
		t.Errorf("新しい bob の GET = %d, want 404", rec.Code)
	}
	members := withCookie(t, r, http.MethodGet, "/api/boards/"+boardID+"/members", alice)
	if n := len(decode[[]apitypes.BoardMember](t, members)); n != 1 {
		t.Errorf("メンバー = %d 人, want 1（owner だけ）", n)
	}
}

// 2 回目の招待は 409。黙ってロールを書き換えない。
func TestInvite_DuplicateIsConflict(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{}
	r, _ := newAuthRouter(t, provider)

	signInAs(t, r, provider, "2", "bob")
	alice := signInAs(t, r, provider, "1", "alice")
	boardID := createSharedBoard(t, r, alice, "ボード")

	body := inviteBody(t, r, alice, boardID, "bob", "editor")
	if rec := doJSON(t, r, http.MethodPost, "/api/boards/"+boardID+"/members", alice, body); rec.Code != http.StatusCreated {
		t.Fatalf("1 回目: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, r, http.MethodPost, "/api/boards/"+boardID+"/members", alice, body); rec.Code != http.StatusConflict {
		t.Fatalf("2 回目 = %d %s, want 409", rec.Code, rec.Body)
	}
}

// 最後の owner は外せない。誰も招待できず作成先も変えられないボードが残る。
func TestRemoveMember_LastOwnerIsConflict(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{}
	r, _ := newAuthRouter(t, provider)

	alice := signInAs(t, r, provider, "1", "alice")
	boardID := createSharedBoard(t, r, alice, "ボード")

	members := decode[[]map[string]any](t,
		withCookie(t, r, http.MethodGet, "/api/boards/"+boardID+"/members", alice))
	if len(members) != 1 {
		t.Fatalf("メンバー = %+v, want 1 人", members)
	}
	userID, _ := members[0]["userId"].(string)

	rec := withCookie(t, r, http.MethodDelete,
		"/api/boards/"+boardID+"/members/"+userID, alice)
	if rec.Code != http.StatusConflict {
		t.Fatalf("最後の owner の削除 = %d %s, want 409", rec.Code, rec.Body)
	}
}

// 認証を設定していない構成では共有そのものが無い。404 ではなく 503 を返し、
// 「URL が違う」のか「設定していない」のかを区別できるようにする。
func TestMembers_WithoutAuthConfigured(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, nil)

	rec := withCookie(t, r, http.MethodGet, "/api/boards/board-1/members", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("認証なしの members = %d %s, want 503", rec.Code, rec.Body)
	}
	// 設定するものが違うので、LLM / GitHub の未設定と同じ code に畳まない。
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeSharingNotConfigured {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeSharingNotConfigured)
	}
}

// 作成できるかどうかは、etoki 側のロールとは別に返る。「開けるが書けない」が
// 普通に起きるので、1 つに畳まない（ADR 0017）。
func TestBoardAccess_ReportsRoleAndProjectAccess(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{}
	r, _ := newAuthRouter(t, provider)

	alice := signInAs(t, r, provider, "1", "alice")
	boardID := createSharedBoard(t, r, alice, "ボード")

	rec := withCookie(t, r, http.MethodGet, "/api/boards/"+boardID+"/access", alice)
	if rec.Code != http.StatusOK {
		t.Fatalf("access = %d %s", rec.Code, rec.Body)
	}

	body := decode[map[string]any](t, rec)
	if body["role"] != "owner" {
		t.Errorf("role = %v, want owner", body["role"])
	}
	// この構成は GitHub を設定していない。確かめていないことを allowed にも
	// denied にも倒さない。
	if body["projectAccess"] != "unknown" {
		t.Errorf("projectAccess = %v, want unknown", body["projectAccess"])
	}
}
