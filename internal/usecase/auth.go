package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// 認証に固有の設定。
const (
	// SessionTTL はセッションの有効期間。
	//
	// GitHub の user-to-server トークンは 8 時間で失効するが、refresh token が
	// あるうちは更新できる。セッションをそれに合わせて短くすると、作業中に
	// 何度もログインさせることになる。
	SessionTTL = 14 * 24 * time.Hour

	// StateTTL は OAuth の state を受け付ける期間。
	//
	// 認可画面での操作にかかる時間。長くすると、盗まれた state が使える窓が
	// 広がるだけで得るものが無い。
	StateTTL = 10 * time.Minute

	// refreshMargin は失効の何分前から更新するか。
	//
	// ちょうどで切ると、更新せずに投げたリクエストが往復のあいだに失効する。
	refreshMargin = 5 * time.Minute
)

// AuthService はログインとセッションを担う。
//
// port.GitHubTokenSource も兼ねる。「誰であるか」と「GitHub をどのトークンで
// 叩くか」は別の継ぎ目だが（ADR 0015）、GitHub App で認証する既定の構成では
// 同じ資格情報から両方が決まる。別の基盤に載せ替えるときは、この実装ごと
// 差し替わる。
type AuthService struct {
	provider port.IdentityProvider
	sessions port.SessionRepository
	now      func() time.Time
	newToken func() (string, error)

	// refreshing は利用者ごとの更新の直列化。
	//
	// GitHub は使った refresh token を無効にするので、並行する 2 本が同時に
	// 更新すると片方が無効なトークンを掴む（ADR 0015）。
	refreshing sync.Map // userID -> *sync.Mutex
}

var _ port.GitHubTokenSource = (*AuthService)(nil)

// AuthServiceOption は AuthService の依存を差し替える。
type AuthServiceOption func(*AuthService)

// WithAuthClock は時刻の取得方法を差し替える。
func WithAuthClock(f func() time.Time) AuthServiceOption {
	return func(s *AuthService) { s.now = f }
}

// WithTokenGenerator はセッション token の採番方法を差し替える。
func WithTokenGenerator(f func() (string, error)) AuthServiceOption {
	return func(s *AuthService) { s.newToken = f }
}

// NewAuthService は AuthService を作る。
func NewAuthService(
	provider port.IdentityProvider, sessions port.SessionRepository, opts ...AuthServiceOption,
) *AuthService {
	s := &AuthService{
		provider: provider,
		sessions: sessions,
		now:      time.Now,
		newToken: randomToken,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Provider は認証基盤の識別子を返す。
func (s *AuthService) Provider() string { return s.provider.Name() }

// Start は state を発行し、送り出す先の URL を返す。
//
// state はサーバーが作って保存し、コールバックで照合してから捨てる。OAuth の
// CSRF 対策であり、Origin ガード（ADR 0013）とは別物。
//
// returnTo はログイン後に戻す先。空文字なら「指定なし」で、コールバックは "/"
// に戻す。**戻り先を state と一緒に保存する**ので、コールバックの URL には
// 載らない（ADR 0059）。
func (s *AuthService) Start(ctx context.Context, redirectURI, returnTo string) (string, error) {
	returnTo, err := SanitizeReturnTo(returnTo)
	if err != nil {
		return "", err
	}

	state, err := s.newToken()
	if err != nil {
		return "", err
	}

	now := s.now()
	if err := s.sessions.SaveState(ctx, port.OAuthState{
		State:     state,
		ReturnTo:  returnTo,
		CreatedAt: now,
		ExpiresAt: now.Add(StateTTL),
	}); err != nil {
		return "", err
	}

	return s.provider.AuthorizeURL(state, redirectURI), nil
}

// SanitizeReturnTo はログイン後の戻り先を検証して整える。
//
// **通すのは自オリジンの相対パスだけ。** 戻り先はブラウザのトップレベル遷移に
// なるので、絶対 URL や `//host` を通すとオープンリダイレクトになる。空文字は
// 「指定なし」としてそのまま通し、コールバックが "/" に落とす。
//
// **不正値を黙って空に落とさない。** 落とすと、渡した値が消えたことに呼び出し
// 側が気づけないまま、思っていない場所に着地する（.claude/rules/
// validation-boundaries.md）。
//
// **検証はここだけに置く。** ハンドラにも永続化層にも同じ判定を書かない。
// 公開しているのは、コールバックの側が「保存済みの値は検証済み」と言い切れる
// 根拠をテストで固定するため。
func SanitizeReturnTo(returnTo string) (string, error) {
	if returnTo == "" {
		return "", nil
	}

	u, err := url.Parse(returnTo)
	if err != nil {
		return "", fmt.Errorf("%w: returnTo is not a valid URL", ErrInvalidInput)
	}
	// スキームとホストを持つものは他所を指しうる。Opaque は "mailto:..." の形。
	if u.Scheme != "" || u.Host != "" || u.Opaque != "" {
		return "", fmt.Errorf("%w: returnTo must be a relative path", ErrInvalidInput)
	}

	// **受け取った文字列のまま見る。** "/\\evil.example" はブラウザが "//" と
	// 同じに読むが、url.Parse を通すとバックスラッシュが %5C に化けるので、
	// 組み直したあとの文字列を見る検査では通り抜ける。**通り抜けたほうは
	// 同一オリジンの安全なパスだが、書き換えて通すことになる。** 送ろうとした
	// 先と着地する先が変わるので、直して通さず弾く。
	if !strings.HasPrefix(returnTo, "/") ||
		strings.HasPrefix(returnTo, "//") ||
		strings.HasPrefix(returnTo, "/\\") {
		return "", fmt.Errorf("%w: returnTo must be a same-origin path", ErrInvalidInput)
	}

	// フラグメントは捨てる。ブラウザは Location のフラグメントを送ってくれない
	// ので持ち回っても届かず、持っているふりだけが残る。
	sanitized := (&url.URL{Path: u.Path, RawQuery: u.RawQuery}).String()

	// 組み直した結果をもう一度見る。url.URL{Path: "//evil.example"} は
	// "//evil.example" を返すので、入力を見た上の検査だけでは足りない。
	if !strings.HasPrefix(sanitized, "/") || strings.HasPrefix(sanitized, "//") {
		return "", fmt.Errorf("%w: returnTo must be a same-origin path", ErrInvalidInput)
	}

	return sanitized, nil
}

// LoginResult は 1 回のログインで決まったもの。
//
// **戻り値を並べずに束ねる。** token / 利用者 / 戻り先の 3 つを返す形にすると、
// 呼び出し側が位置で受けることになり、足したときに取り違えても型が合う。
type LoginResult struct {
	// Token は cookie に載せる値そのもの。保存されるのはそのハッシュだけ。
	Token string
	// User はログインした利用者。
	User port.User
	// ReturnTo は Start に渡された戻り先。空文字は「指定なし」。
	//
	// **検証済みの値しか入らない。** 保存したのは Start（SanitizeReturnTo を
	// 通した後）だけなので、ここで検証し直さない。
	ReturnTo string
}

// Complete はコールバックを処理し、セッション token と戻り先を返す。
func (s *AuthService) Complete(
	ctx context.Context, code, state, redirectURI string,
) (LoginResult, error) {
	now := s.now()

	// 照合は引き換えより先。state が通らないリクエストで GitHub を叩かせない。
	saved, err := s.sessions.ConsumeState(ctx, state, now)
	if err != nil {
		return LoginResult{}, err
	}
	if saved == nil {
		return LoginResult{}, fmt.Errorf("%w: state is unknown or expired", ErrInvalidInput)
	}

	identity, creds, err := s.provider.Exchange(ctx, code, redirectURI)
	if err != nil {
		return LoginResult{}, err
	}

	user, err := s.sessions.UpsertUser(ctx, port.User{
		Provider:    identity.Provider,
		Subject:     identity.Subject,
		Login:       identity.Login,
		DisplayName: identity.DisplayName,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return LoginResult{}, err
	}

	if err := s.sessions.SaveCredentials(ctx, user.ID, creds, now); err != nil {
		return LoginResult{}, err
	}

	token, err := s.newToken()
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.sessions.CreateSession(ctx, port.Session{
		TokenHash: HashSessionToken(token),
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
	}); err != nil {
		return LoginResult{}, err
	}

	return LoginResult{Token: token, User: user, ReturnTo: saved.ReturnTo}, nil
}

// Resolve はセッション token から利用者を引く。
//
// 見つからない・失効しているは (nil, nil)。呼び出し側が未ログインとして扱う。
func (s *AuthService) Resolve(ctx context.Context, token string) (*port.User, error) {
	if token == "" {
		return nil, nil
	}

	session, err := s.sessions.FindSession(ctx, HashSessionToken(token), s.now())
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, nil
	}

	// セッションはあるのに利用者が消えている場合も未ログインとして扱う。
	return s.sessions.FindUser(ctx, session.UserID)
}

// FindUserByLogin は login で利用者を引く。存在しなければ (nil, nil)。
//
// 招待の相手を指すのに使う。認証基盤の識別子はここが知っているので、
// 呼ぶ側が provider を持ち回らずに済む。
func (s *AuthService) FindUserByLogin(ctx context.Context, login string) (*port.User, error) {
	return s.sessions.FindUserByLogin(ctx, s.provider.Name(), login)
}

// FindUsers は ID をまとめて引く。メンバー一覧に表示名を添えるのに使う。
func (s *AuthService) FindUsers(ctx context.Context, ids []string) ([]port.User, error) {
	return s.sessions.FindUsers(ctx, ids)
}

// Logout はセッションを破棄する。
//
// GitHub 側のトークンは取り消さない。同じ利用者が別の端末からも使っている
// 可能性があり、片方のログアウトで全部を切るのは意図に合わない。
func (s *AuthService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.DeleteSession(ctx, HashSessionToken(token))
}

// Token は現在の利用者の GitHub トークンを返す。port.GitHubTokenSource の実装。
//
// 失効間際なら更新して保存し直す。
func (s *AuthService) Token(ctx context.Context) (string, error) {
	userID, ok := port.UserIDFromContext(ctx)
	if !ok || userID == "" {
		return "", port.ErrNotAuthenticated
	}

	// 更新は利用者ごとに 1 本に絞る。並行して更新すると、GitHub が使い捨てに
	// した refresh token を掴んだ側が失敗する。
	value, _ := s.refreshing.LoadOrStore(userID, &sync.Mutex{})
	mu, ok := value.(*sync.Mutex)
	if !ok {
		return "", errors.New("etoki: refresh lock has unexpected type")
	}
	mu.Lock()
	defer mu.Unlock()

	creds, err := s.sessions.FindCredentials(ctx, userID)
	if err != nil {
		return "", err
	}
	if creds == nil {
		return "", fmt.Errorf("%w: no stored credentials", port.ErrNotAuthenticated)
	}

	if !s.needsRefresh(*creds) {
		return creds.AccessToken, nil
	}

	now := s.now()
	if !creds.RefreshExpiresAt.IsZero() && !creds.RefreshExpiresAt.After(now) {
		return "", fmt.Errorf("%w: refresh token expired", port.ErrNotAuthenticated)
	}

	next, err := s.provider.Refresh(ctx, creds.RefreshToken)
	if err != nil {
		return "", err
	}
	if err := s.sessions.SaveCredentials(ctx, userID, next, now); err != nil {
		return "", err
	}

	return next.AccessToken, nil
}

// needsRefresh は更新が要るかを返す。
//
// 失効情報を持たない資格情報は更新しない。App 側で「Expire user authorization
// tokens」を切っている構成が該当する。
func (s *AuthService) needsRefresh(c port.Credentials) bool {
	if !c.Refreshable() {
		return false
	}
	return !c.ExpiresAt.After(s.now().Add(refreshMargin))
}

// HashSessionToken はセッション token を保存できる形にする。
//
// cookie の値そのものは保存しない。DB が漏れても生きたセッションにならない
// ようにするため（ADR 0015）。鍵は要らないので単純なハッシュでよい。
func HashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// randomToken はセッション token と state に使う乱数を作る。
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("etoki: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// StaticTokenSource は常に同じトークンを返す port.GitHubTokenSource。
//
// 認証を設定していない構成（ETOKI_GITHUB_TOKEN だけ）で使う。これがあることで、
// github.Client は「トークンをどこから得るか」を 1 通りだけ知っていればよくなる。
type StaticTokenSource string

var _ port.GitHubTokenSource = StaticTokenSource("")

// Token は設定されたトークンをそのまま返す。
func (t StaticTokenSource) Token(context.Context) (string, error) {
	if t == "" {
		return "", fmt.Errorf("%w: no token configured", port.ErrNotAuthenticated)
	}
	return string(t), nil
}
