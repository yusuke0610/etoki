package usecase_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

var authNow = time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

// fakeProvider は決められた利用者を返す IdentityProvider。
type fakeProvider struct {
	identity port.Identity

	mu           sync.Mutex
	exchangeCode string
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) AuthorizeURL(state, redirectURI string) string {
	return "https://example.test/authorize?state=" + state + "&redirect_uri=" + redirectURI
}

func (f *fakeProvider) Exchange(_ context.Context, code, _ string) (port.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exchangeCode = code
	return f.identity, nil
}

// fakeSessions は port.SessionRepository のメモリ実装。
type fakeSessions struct {
	mu       sync.Mutex
	users    map[string]port.User       // id -> user
	sessions map[string]port.Session    // tokenHash -> session
	states   map[string]port.OAuthState // state -> 保存した内容
	seq      int
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{
		users:    map[string]port.User{},
		sessions: map[string]port.Session{},
		states:   map[string]port.OAuthState{},
	}
}

func (f *fakeSessions) UpsertUser(_ context.Context, u port.User) (port.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.users {
		if existing.Provider == u.Provider && existing.Subject == u.Subject {
			existing.Login, existing.DisplayName = u.Login, u.DisplayName
			existing.UpdatedAt = u.UpdatedAt
			f.users[existing.ID] = existing
			return existing, nil
		}
	}

	f.seq++
	u.ID = "user-" + strconv.Itoa(f.seq)
	f.users[u.ID] = u
	return u, nil
}

func (f *fakeSessions) FindUser(_ context.Context, id string) (*port.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	u, ok := f.users[id]
	if !ok {
		return nil, nil
	}
	return &u, nil
}

func (f *fakeSessions) FindUserByLogin(
	_ context.Context, provider, login string,
) (*port.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, u := range f.users {
		// 実装と同じく大文字小文字を区別せず、外した login（空文字）では引かない。
		if u.Provider == provider && login != "" && strings.EqualFold(u.Login, login) {
			return &u, nil
		}
	}
	return nil, nil
}

// FindUsers は見つかった ID だけを返す。欠けを誤りにしない実装と同じ形にする。
func (f *fakeSessions) FindUsers(_ context.Context, ids []string) ([]port.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []port.User
	for _, id := range ids {
		for _, u := range f.users {
			if u.ID == id {
				out = append(out, u)
			}
		}
	}
	return out, nil
}

func (f *fakeSessions) CreateSession(_ context.Context, s port.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.TokenHash] = s
	return nil
}

func (f *fakeSessions) FindSession(
	_ context.Context, tokenHash string, now time.Time,
) (*port.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	s, ok := f.sessions[tokenHash]
	if !ok || !s.ExpiresAt.After(now) {
		return nil, nil
	}
	return &s, nil
}

func (f *fakeSessions) DeleteSession(_ context.Context, tokenHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, tokenHash)
	return nil
}

func (f *fakeSessions) SaveState(_ context.Context, st port.OAuthState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[st.State] = st
	return nil
}

func (f *fakeSessions) ConsumeState(
	_ context.Context, state string, now time.Time,
) (*port.OAuthState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	saved, ok := f.states[state]
	delete(f.states, state)
	if !ok || !saved.ExpiresAt.After(now) {
		return nil, nil
	}
	return &saved, nil
}

// allowedSubject はログインを通す相手。defaultProvider の利用者と同じ。
const allowedSubject = "42"

func newAuthService(t *testing.T, p *fakeProvider, s *fakeSessions) *usecase.AuthService {
	t.Helper()
	return usecase.NewAuthService(p, s, []string{allowedSubject},
		usecase.WithAuthClock(func() time.Time { return authNow }))
}

func defaultProvider() *fakeProvider {
	return &fakeProvider{
		identity: port.Identity{
			Provider: "fake", Subject: "42", Login: "octocat", DisplayName: "Octo Cat",
		},
	}
}

func TestStartAndComplete(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	if _, err := svc.Start(t.Context(), "http://127.0.0.1:5173/api/auth/callback", ""); err != nil {
		t.Fatalf("Start() = %v", err)
	}

	var state string
	for s := range sessions.states {
		state = s
	}
	if state == "" {
		t.Fatal("state が保存されていない")
	}

	result, err := svc.Complete(t.Context(), "code-1", state, "http://127.0.0.1:5173/api/auth/callback")
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	token := result.Token
	if token == "" {
		t.Fatal("セッション token が空")
	}
	if result.User.Login != "octocat" {
		t.Errorf("user.Login = %q, want octocat", result.User.Login)
	}

	// cookie の値そのものは保存しない。DB が漏れても生きたセッションに
	// ならないようにするため。
	if _, ok := sessions.sessions[token]; ok {
		t.Error("セッション token が平文で保存されている")
	}
	if _, ok := sessions.sessions[usecase.HashSessionToken(token)]; !ok {
		t.Error("ハッシュで保存されていない")
	}
}

// ログインの開始には回数の上限がある（issue #147）。
//
// **ログインしていなくても書き込みが起きる口の 1 つ**（もう 1 つは MCP の
// クライアントの登録、ADR 0076）。1 回ごとに state が
// 1 つ保存され、掃除は期限切れしか消さないので、上限が無いと窓のあいだ叩かれた
// 回数だけ表が育つ。
//
// **断ったぶんを保存していないことまで見る。** 上限を state の保存より後ろで
// 見る実装は、エラーの検査だけでは素通りする。守ろうとしている当のものが
// 増えている。
func TestStart_LimitsLoginStarts(t *testing.T) {
	t.Parallel()

	sessions := newFakeSessions()
	now := authNow
	svc := usecase.NewAuthService(defaultProvider(), sessions, []string{allowedSubject},
		usecase.WithAuthClock(func() time.Time { return now }))

	for i := range usecase.MaxLoginStarts {
		if _, err := svc.Start(t.Context(), "", ""); err != nil {
			t.Fatalf("%d 回目の Start() = %v", i+1, err)
		}
	}

	saved := len(sessions.states)
	if _, err := svc.Start(t.Context(), "", ""); !errors.Is(err, usecase.ErrRateLimited) {
		t.Fatalf("上限を超えた Start() = %v, want ErrRateLimited", err)
	}
	if len(sessions.states) != saved {
		t.Errorf("断ったのに state が増えている: %d -> %d", saved, len(sessions.states))
	}

	// 窓（StateTTL）を出れば通る。**上限は永久に閉じるものではない。**
	now = authNow.Add(usecase.StateTTL + time.Second)
	if _, err := svc.Start(t.Context(), "", ""); err != nil {
		t.Errorf("窓を出たあとの Start() = %v, want nil", err)
	}
}

// state は単回使用。使い回せると CSRF 対策にならない。
func TestComplete_RejectsReusedState(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	if _, err := svc.Start(t.Context(), "http://127.0.0.1/api/auth/callback", ""); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	var state string
	for s := range sessions.states {
		state = s
	}

	if _, err := svc.Complete(t.Context(), "code-1", state, ""); err != nil {
		t.Fatalf("1 回目の Complete() = %v", err)
	}
	if _, err := svc.Complete(t.Context(), "code-1", state, ""); !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("2 回目の Complete() = %v, want ErrInvalidInput", err)
	}
}

func TestComplete_RejectsUnknownState(t *testing.T) {
	t.Parallel()

	svc := newAuthService(t, defaultProvider(), newFakeSessions())

	_, err := svc.Complete(t.Context(), "code-1", "never-issued", "")
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("Complete() = %v, want ErrInvalidInput", err)
	}
}

// state が通らないリクエストで認証基盤を叩かせない。
func TestComplete_DoesNotExchangeWithBadState(t *testing.T) {
	t.Parallel()

	provider := defaultProvider()
	svc := newAuthService(t, provider, newFakeSessions())

	if _, err := svc.Complete(t.Context(), "code-1", "bogus", ""); err == nil {
		t.Fatal("Complete() = nil, want error")
	}
	if provider.exchangeCode != "" {
		t.Errorf("state を照合する前に Exchange している: %q", provider.exchangeCode)
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	if _, err := svc.Start(t.Context(), "", ""); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	var state string
	for s := range sessions.states {
		state = s
	}
	result, err := svc.Complete(t.Context(), "code-1", state, "")
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	token := result.Token

	user, err := svc.Resolve(t.Context(), token)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if user == nil || user.Login != "octocat" {
		t.Fatalf("Resolve() = %+v, want octocat", user)
	}

	// ログアウトしたら解決できない。
	if err := svc.Logout(t.Context(), token); err != nil {
		t.Fatalf("Logout() = %v", err)
	}
	if user, err = svc.Resolve(t.Context(), token); err != nil || user != nil {
		t.Fatalf("Resolve() = (%+v, %v), want (nil, nil)", user, err)
	}
}

func TestResolve_IgnoresUnknownToken(t *testing.T) {
	t.Parallel()

	svc := newAuthService(t, defaultProvider(), newFakeSessions())

	for _, token := range []string{"", "never-issued"} {
		user, err := svc.Resolve(t.Context(), token)
		if err != nil || user != nil {
			t.Errorf("Resolve(%q) = (%+v, %v), want (nil, nil)", token, user, err)
		}
	}
}

// ---------------------------------------------------------------------------
// ログインを通す相手（#492）
// ---------------------------------------------------------------------------

// startedState はログインを始め、保存された state を返す。
func startedState(t *testing.T, svc *usecase.AuthService, sessions *fakeSessions) string {
	t.Helper()

	if _, err := svc.Start(t.Context(), "", ""); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	for s := range sessions.states {
		return s
	}
	t.Fatal("state が保存されていない")
	return ""
}

// 通す相手でなければ断る。**断った相手について何も書かないことまで見る。**
// 利用者を作ってから断る実装は、エラーの検査だけでは素通りする。残った行は
// 招待の引き当てに現れ、保存したトークンは他人の GitHub の権限そのもの。
func TestComplete_RejectsUserNotAllowed(t *testing.T) {
	t.Parallel()

	for name, identity := range map[string]port.Identity{
		"別の利用者": {Provider: "fake", Subject: "99", Login: "hubot"},
		// Subject は基盤の中でだけ一意。別の基盤の同じ番号を通さない。
		"別の基盤の同じ番号": {Provider: "other", Subject: allowedSubject, Login: "octocat"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			provider, sessions := defaultProvider(), newFakeSessions()
			provider.identity = identity
			svc := newAuthService(t, provider, sessions)

			_, err := svc.Complete(t.Context(), "code-1", startedState(t, svc, sessions), "")
			if !errors.Is(err, usecase.ErrLoginNotAllowed) {
				t.Fatalf("Complete() = %v, want ErrLoginNotAllowed", err)
			}
			// 断った相手が誰だったかはログで追えるようにする。
			if !strings.Contains(err.Error(), identity.Login) {
				t.Errorf("断った相手の login がエラーに無い: %v", err)
			}

			if len(sessions.users) != 0 {
				t.Errorf("断った相手を利用者にしている: %+v", sessions.users)
			}
			if len(sessions.sessions) != 0 {
				t.Errorf("断った相手にセッションを渡している: %+v", sessions.sessions)
			}
		})
	}
}

// 通す相手を渡さなかったら誰も通さない。開く側に倒すと、設定を書き忘れた
// だけで誰でもログインできた頃に戻る。
func TestComplete_RejectsEveryoneWithoutAllowedSubject(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		allowed []string
		subject string
	}{
		"一覧が空": {allowed: nil, subject: allowedSubject},
		// 空の ID は「通す相手」ではない。認証基盤が空の Subject を返したときに、
		// 空を書いた一覧と一致して通らないようにする。
		"空の ID": {allowed: []string{""}, subject: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			provider, sessions := defaultProvider(), newFakeSessions()
			provider.identity.Subject = tc.subject
			svc := usecase.NewAuthService(provider, sessions, tc.allowed,
				usecase.WithAuthClock(func() time.Time { return authNow }))

			_, err := svc.Complete(t.Context(), "code-1", startedState(t, svc, sessions), "")
			if !errors.Is(err, usecase.ErrLoginNotAllowed) {
				t.Fatalf("Complete() = %v, want ErrLoginNotAllowed", err)
			}
			if len(sessions.users) != 0 || len(sessions.sessions) != 0 {
				t.Errorf("通す相手が無いのに書いている: users=%d sessions=%d",
					len(sessions.users), len(sessions.sessions))
			}
		})
	}
}

// 通す相手を変えた日に、外した人のセッションを生かさない。同じ DB を、通す
// 相手だけを変えた別の組み立てから見る（再起動して設定を変えたのと同じ）。
func TestResolve_IgnoresSessionOfUserNoLongerAllowed(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	before := newAuthService(t, provider, sessions)

	result, err := before.Complete(t.Context(), "code-1", startedState(t, before, sessions), "")
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	// 前提。変える前は解決できる。
	if user, err := before.Resolve(t.Context(), result.Token); err != nil || user == nil {
		t.Fatalf("変える前の Resolve() = (%+v, %v), want 利用者", user, err)
	}

	after := usecase.NewAuthService(provider, sessions, []string{"99"},
		usecase.WithAuthClock(func() time.Time { return authNow }))
	user, err := after.Resolve(t.Context(), result.Token)
	if err != nil || user != nil {
		t.Fatalf("変えたあとの Resolve() = (%+v, %v), want (nil, nil)", user, err)
	}
}

// MCP のトークンの持ち主もセッションと同じ相手で照らす（ADR 0076）。
func TestFindAllowedUser(t *testing.T) {
	t.Parallel()

	sessions := newFakeSessions()
	allowed, err := sessions.UpsertUser(t.Context(), port.User{
		Provider: "fake", Subject: allowedSubject, Login: "octocat",
	})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	other, err := sessions.UpsertUser(t.Context(), port.User{
		Provider: "fake", Subject: "99", Login: "hubot",
	})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	svc := newAuthService(t, defaultProvider(), sessions)

	got, err := svc.FindAllowedUser(t.Context(), allowed.ID)
	if err != nil || got == nil || got.ID != allowed.ID {
		t.Errorf("通す相手 = (%+v, %v), want %s", got, err, allowed.ID)
	}
	for name, id := range map[string]string{"通さない相手": other.ID, "居ない利用者": "user-missing"} {
		if got, err := svc.FindAllowedUser(t.Context(), id); err != nil || got != nil {
			t.Errorf("%s = (%+v, %v), want (nil, nil)", name, got, err)
		}
	}
}

// ログイン後の戻り先は自オリジンの相対パスだけ（ADR 0059）。ここが緩むと、
// ログインさせた相手を任意の URL へ送り出せる。
func TestSanitizeReturnTo(t *testing.T) {
	t.Parallel()

	allowed := []struct {
		name string
		in   string
		want string
	}{
		{"指定なし", "", ""},
		{"ボードの URL", "/?board=board-1", "/?board=board-1"},
		{"選び直しの URL", "/?board=board-1&picking=1", "/?board=board-1&picking=1"},
		{"ルート", "/", "/"},
		// フラグメントは捨てる。ブラウザは Location のフラグメントを送って
		// こないので、持ち回っても届かない。
		{"フラグメントは落とす", "/?board=b#frame", "/?board=b"},
	}
	for _, tc := range allowed {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := usecase.SanitizeReturnTo(tc.in)
			if err != nil {
				t.Fatalf("SanitizeReturnTo(%q) = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("SanitizeReturnTo(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	rejected := []struct {
		name string
		in   string
	}{
		{"絶対 URL", "https://evil.example/"},
		{"スキーム相対", "//evil.example/"},
		// ブラウザは "/\" を "//" と同じに読む。文字列の形だけを見た検査を
		// すり抜ける典型。
		{"バックスラッシュ", "/\\evil.example/"},
		{"スキームだけ", "javascript:alert(1)"},
		{"パスで始まらない", "board-1"},
		{"壊れた URL", "/%zz"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := usecase.SanitizeReturnTo(tc.in)
			if !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("SanitizeReturnTo(%q) = (%q, %v), want ErrInvalidInput", tc.in, got, err)
			}
			// **黙って空に落とさない。** 落とすと、渡した値が消えたことに
			// 呼び出し側が気づけない。
			if got != "" {
				t.Errorf("弾いたのに値を返している: %q", got)
			}
		})
	}
}

// 弾いた戻り先では state を発行しない。発行してしまうと、400 を返した分だけ
// 表が育つ。
func TestStart_RejectsForeignReturnTo(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	if _, err := svc.Start(t.Context(), "", "https://evil.example/"); !errors.Is(
		err, usecase.ErrInvalidInput,
	) {
		t.Fatalf("Start() = %v, want ErrInvalidInput", err)
	}
	if len(sessions.states) != 0 {
		t.Errorf("弾いたのに state を保存している: %v", sessions.states)
	}
}

// 戻り先は state と一緒に往復する。Complete がここを落とすと、入り直した人は
// 開いていたボードへ戻れない。
func TestComplete_CarriesReturnTo(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	if _, err := svc.Start(t.Context(), "", "/?board=board-1"); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	var state string
	for s := range sessions.states {
		state = s
	}

	result, err := svc.Complete(t.Context(), "code-1", state, "")
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	if result.ReturnTo != "/?board=board-1" {
		t.Errorf("ReturnTo = %q, want /?board=board-1", result.ReturnTo)
	}
}
