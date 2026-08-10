package usecase_test

import (
	"context"
	"errors"
	"strconv"
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
	creds    port.Credentials
	// refreshed は Refresh が返す資格情報。
	refreshed port.Credentials
	// refreshErr が非 nil なら Refresh が失敗する。
	refreshErr error
	// refreshDelay は Refresh にかける時間。並行呼び出しの窓を広げるために使う。
	refreshDelay time.Duration

	mu           sync.Mutex
	refreshCalls int
	exchangeCode string
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) AuthorizeURL(state, redirectURI string) string {
	return "https://example.test/authorize?state=" + state + "&redirect_uri=" + redirectURI
}

func (f *fakeProvider) Exchange(
	_ context.Context, code, _ string,
) (port.Identity, port.Credentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exchangeCode = code
	return f.identity, f.creds, nil
}

func (f *fakeProvider) Refresh(context.Context, string) (port.Credentials, error) {
	f.mu.Lock()
	f.refreshCalls++
	err, out, delay := f.refreshErr, f.refreshed, f.refreshDelay
	f.mu.Unlock()

	// 更新には往復がかかる。直列化されていなければ、この間に他の呼び出しが
	// 同じ古い資格情報を読んで一緒に更新に走る。
	time.Sleep(delay)

	if err != nil {
		return port.Credentials{}, err
	}
	return out, nil
}

func (f *fakeProvider) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshCalls
}

// fakeSessions は port.SessionRepository のメモリ実装。
type fakeSessions struct {
	mu       sync.Mutex
	users    map[string]port.User        // id -> user
	sessions map[string]port.Session     // tokenHash -> session
	creds    map[string]port.Credentials // userID -> credentials
	states   map[string]time.Time        // state -> expiresAt
	saves    int
	seq      int
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{
		users:    map[string]port.User{},
		sessions: map[string]port.Session{},
		creds:    map[string]port.Credentials{},
		states:   map[string]time.Time{},
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
		if u.Provider == provider && u.Login == login {
			return &u, nil
		}
	}
	return nil, nil
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

func (f *fakeSessions) SaveCredentials(
	_ context.Context, userID string, c port.Credentials, _ time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creds[userID] = c
	f.saves++
	return nil
}

func (f *fakeSessions) FindCredentials(
	_ context.Context, userID string,
) (*port.Credentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	c, ok := f.creds[userID]
	if !ok {
		return nil, nil
	}
	return &c, nil
}

func (f *fakeSessions) SaveState(_ context.Context, state string, _, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[state] = expiresAt
	return nil
}

func (f *fakeSessions) ConsumeState(
	_ context.Context, state string, now time.Time,
) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	expiresAt, ok := f.states[state]
	delete(f.states, state)
	return ok && expiresAt.After(now), nil
}

func (f *fakeSessions) savedCredentials(userID string) port.Credentials {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creds[userID]
}

func newAuthService(t *testing.T, p *fakeProvider, s *fakeSessions) *usecase.AuthService {
	t.Helper()
	return usecase.NewAuthService(p, s, usecase.WithAuthClock(func() time.Time { return authNow }))
}

func defaultProvider() *fakeProvider {
	return &fakeProvider{
		identity: port.Identity{
			Provider: "fake", Subject: "42", Login: "octocat", DisplayName: "Octo Cat",
		},
		creds: port.Credentials{
			AccessToken:      "access-1",
			RefreshToken:     "refresh-1",
			ExpiresAt:        authNow.Add(8 * time.Hour),
			RefreshExpiresAt: authNow.Add(180 * 24 * time.Hour),
		},
		refreshed: port.Credentials{
			AccessToken:      "access-2",
			RefreshToken:     "refresh-2",
			ExpiresAt:        authNow.Add(8 * time.Hour),
			RefreshExpiresAt: authNow.Add(180 * 24 * time.Hour),
		},
	}
}

func TestStartAndComplete(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	if _, err := svc.Start(t.Context(), "http://127.0.0.1:5173/api/auth/callback"); err != nil {
		t.Fatalf("Start() = %v", err)
	}

	var state string
	for s := range sessions.states {
		state = s
	}
	if state == "" {
		t.Fatal("state が保存されていない")
	}

	token, user, err := svc.Complete(t.Context(), "code-1", state, "http://127.0.0.1:5173/api/auth/callback")
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	if token == "" {
		t.Fatal("セッション token が空")
	}
	if user.Login != "octocat" {
		t.Errorf("user.Login = %q, want octocat", user.Login)
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

// state は単回使用。使い回せると CSRF 対策にならない。
func TestComplete_RejectsReusedState(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	if _, err := svc.Start(t.Context(), "http://127.0.0.1/api/auth/callback"); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	var state string
	for s := range sessions.states {
		state = s
	}

	if _, _, err := svc.Complete(t.Context(), "code-1", state, ""); err != nil {
		t.Fatalf("1 回目の Complete() = %v", err)
	}
	if _, _, err := svc.Complete(t.Context(), "code-1", state, ""); !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("2 回目の Complete() = %v, want ErrInvalidInput", err)
	}
}

func TestComplete_RejectsUnknownState(t *testing.T) {
	t.Parallel()

	svc := newAuthService(t, defaultProvider(), newFakeSessions())

	_, _, err := svc.Complete(t.Context(), "code-1", "never-issued", "")
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("Complete() = %v, want ErrInvalidInput", err)
	}
}

// state が通らないリクエストで認証基盤を叩かせない。
func TestComplete_DoesNotExchangeWithBadState(t *testing.T) {
	t.Parallel()

	provider := defaultProvider()
	svc := newAuthService(t, provider, newFakeSessions())

	if _, _, err := svc.Complete(t.Context(), "code-1", "bogus", ""); err == nil {
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

	if _, err := svc.Start(t.Context(), ""); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	var state string
	for s := range sessions.states {
		state = s
	}
	token, _, err := svc.Complete(t.Context(), "code-1", state, "")
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}

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
// port.GitHubTokenSource としての振る舞い
// ---------------------------------------------------------------------------

func TestToken_RequiresIdentity(t *testing.T) {
	t.Parallel()

	svc := newAuthService(t, defaultProvider(), newFakeSessions())

	if _, err := svc.Token(t.Context()); !errors.Is(err, port.ErrNotAuthenticated) {
		t.Fatalf("Token() = %v, want ErrNotAuthenticated", err)
	}
}

func TestToken_ReturnsStoredTokenWhenFresh(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	if err := sessions.SaveCredentials(t.Context(), "user-1", provider.creds, authNow); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	ctx := port.ContextWithUserID(t.Context(), "user-1")
	got, err := svc.Token(ctx)
	if err != nil {
		t.Fatalf("Token() = %v", err)
	}
	if got != "access-1" {
		t.Errorf("Token() = %q, want access-1", got)
	}
	if provider.calls() != 0 {
		t.Errorf("まだ有効なのに更新している: %d 回", provider.calls())
	}
}

// ちょうどで切ると、更新せずに投げたリクエストが往復のあいだに失効する。
func TestToken_RefreshesBeforeExpiry(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	expiring := provider.creds
	expiring.ExpiresAt = authNow.Add(1 * time.Minute) // 余裕（5 分）の内側
	if err := sessions.SaveCredentials(t.Context(), "user-1", expiring, authNow); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	ctx := port.ContextWithUserID(t.Context(), "user-1")
	got, err := svc.Token(ctx)
	if err != nil {
		t.Fatalf("Token() = %v", err)
	}
	if got != "access-2" {
		t.Errorf("Token() = %q, want access-2", got)
	}

	// 更新後の資格情報を保存し直す。次回また更新すると、GitHub が使い捨てに
	// した refresh token を再送することになる。
	if saved := sessions.savedCredentials("user-1"); saved.AccessToken != "access-2" ||
		saved.RefreshToken != "refresh-2" {
		t.Errorf("保存された資格情報 = %+v, want access-2 / refresh-2", saved)
	}
}

// 失効情報を持たない資格情報は更新しない。App 側で失効を切っている構成。
func TestToken_DoesNotRefreshNonExpiringCredentials(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	if err := sessions.SaveCredentials(t.Context(), "user-1",
		port.Credentials{AccessToken: "forever"}, authNow); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	ctx := port.ContextWithUserID(t.Context(), "user-1")
	got, err := svc.Token(ctx)
	if err != nil {
		t.Fatalf("Token() = %v", err)
	}
	if got != "forever" {
		t.Errorf("Token() = %q, want forever", got)
	}
	if provider.calls() != 0 {
		t.Errorf("失効しない資格情報を更新している: %d 回", provider.calls())
	}
}

// GitHub は使った refresh token を無効にする。並行して更新すると、片方が
// 無効なトークンを掴む（ADR 0015）。
func TestToken_SerializesConcurrentRefresh(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	provider.refreshDelay = 20 * time.Millisecond

	expiring := provider.creds
	expiring.ExpiresAt = authNow.Add(1 * time.Minute)
	if err := sessions.SaveCredentials(t.Context(), "user-1", expiring, authNow); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	ctx := port.ContextWithUserID(t.Context(), "user-1")

	const n = 8
	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
	)
	errs := make([]error, n)
	got := make([]string, n)

	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			// 全部を同時に走らせる。順に呼ぶと、直列化が無くても 1 回で済んで
			// しまい、テストが何も確かめないものになる。
			<-start
			got[i], errs[i] = svc.Token(ctx)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Token()[%d] = %v", i, err)
		}
		if got[i] != "access-2" {
			t.Errorf("Token()[%d] = %q, want access-2", i, got[i])
		}
	}

	if calls := provider.calls(); calls != 1 {
		t.Errorf("Refresh を %d 回呼んでいる, want 1", calls)
	}
}

func TestToken_FailsWhenRefreshTokenExpired(t *testing.T) {
	t.Parallel()

	provider, sessions := defaultProvider(), newFakeSessions()
	svc := newAuthService(t, provider, sessions)

	dead := provider.creds
	dead.ExpiresAt = authNow.Add(-time.Hour)
	dead.RefreshExpiresAt = authNow.Add(-time.Minute)
	if err := sessions.SaveCredentials(t.Context(), "user-1", dead, authNow); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	ctx := port.ContextWithUserID(t.Context(), "user-1")
	if _, err := svc.Token(ctx); !errors.Is(err, port.ErrNotAuthenticated) {
		t.Fatalf("Token() = %v, want ErrNotAuthenticated", err)
	}
	if provider.calls() != 0 {
		t.Error("refresh token が失効しているのに更新を試みている")
	}
}

func TestToken_FailsWithoutStoredCredentials(t *testing.T) {
	t.Parallel()

	svc := newAuthService(t, defaultProvider(), newFakeSessions())

	ctx := port.ContextWithUserID(t.Context(), "user-1")
	if _, err := svc.Token(ctx); !errors.Is(err, port.ErrNotAuthenticated) {
		t.Fatalf("Token() = %v, want ErrNotAuthenticated", err)
	}
}

func TestStaticTokenSource(t *testing.T) {
	t.Parallel()

	got, err := usecase.StaticTokenSource("ghp_x").Token(t.Context())
	if err != nil || got != "ghp_x" {
		t.Fatalf("Token() = (%q, %v), want (ghp_x, nil)", got, err)
	}

	if _, err := usecase.StaticTokenSource("").Token(t.Context()); !errors.Is(err, port.ErrNotAuthenticated) {
		t.Fatalf("Token() = %v, want ErrNotAuthenticated", err)
	}
}
