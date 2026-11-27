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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// 認証に固有の設定。
const (
	// SessionTTL はセッションの有効期間。
	//
	// ログインは誰であるかを確かめるだけで、認証基盤のトークンの寿命とは
	// 結びつけない（ADR 0106）。短くすると、作業中に何度もログインさせることに
	// なる。
	SessionTTL = 14 * 24 * time.Hour

	// StateTTL は OAuth の state を受け付ける期間。
	//
	// 認可画面での操作にかかる時間。長くすると、盗まれた state が使える窓が
	// 広がるだけで得るものが無い。
	StateTTL = 10 * time.Minute

	// MaxLoginStarts は StateTTL のあいだに始められるログインの回数
	// （issue #147）。
	//
	// **ログインの開始は、「ログインしていなくても書き込みが起きる」口の 1 つ。**
	// もう 1 つは MCP のクライアントの登録（MaxClientRegistrations、ADR 0076）。
	// 1 回ごとに oauth_states へ 1 行入り、掃除は期限切れしか消さないので、
	// 上限が無いと StateTTL のあいだ叩かれた回数だけ表が育つ。
	//
	// **数えるのは開始の回数で、いま残っている行数ではない。** 行数を数えると
	// 口を増やす（port.SessionRepository にメソッドを足す）ことになり、外部
	// リポジトリの実装が壊れる。開始の回数は残っている行数以上になるので、
	// 抑えたい方向には安全側に外れる。
	//
	// 60 は「認可画面まで行って戻らなかった」を含めても、1 つのインスタンスで
	// 10 分に起きる現実的な回数の上。**根拠のある値ではない。** 実際に詰まる
	// なら動かす。
	MaxLoginStarts = 60
)

// ErrLoginNotAllowed はログインを通さない相手だったことを表す（#492）。
//
// 通すのは 1 人だけ（etoki.NewAuthenticator の allowedSubject）。人に出すのを
// やめ、自分だけが使う形にしたため。
var ErrLoginNotAllowed = errors.New("etoki: this account is not allowed to sign in")

// AuthService はログインとセッションを担う。
//
// 決めるのは誰であるかだけ。GitHub を叩く資格情報は持たない。GitHub は
// 配線が渡す PAT 1 本で叩く（ADR 0106）。
type AuthService struct {
	provider port.IdentityProvider
	sessions port.SessionRepository
	now      func() time.Time

	// allowedSubjects はログインを通す利用者の、認証基盤での ID（Identity.Subject）。
	//
	// **照らすのはログインのときだけではない。** すでにあるセッション（Resolve）と
	// MCP のトークン（FindAllowedUser）も同じ相手で照らす。通す相手を変えた日に、
	// 外した人の入口が残らない。
	allowedSubjects []string

	// mu と loginStarts は StateTTL の窓の中で始まったログインの時刻。
	//
	// **状態はメモリだけ。再起動で消える。** 依る前提は BoardLocks / LLMLimiter と
	// 同じで（SQLite を複数プロセスで共有しない）、表は作らない。
	//
	// **利用者ごとではなくプロセス全体で数える。** この口は requireAuth の外に
	// あり、絞る軸にできる「誰であるか」がまだ無い（ADR 0044 の軸は使えない）。
	mu          sync.Mutex
	loginStarts []time.Time
}

// AuthServiceOption は AuthService の依存を差し替える。
type AuthServiceOption func(*AuthService)

// WithAuthClock は時刻の取得方法を差し替える。
func WithAuthClock(f func() time.Time) AuthServiceOption {
	return func(s *AuthService) { s.now = f }
}

// NewAuthService は AuthService を作る。
//
// allowedSubjects はログインを通す利用者の、認証基盤での ID（Identity.Subject）。
// **空なら誰も通らない。** 空を設定の誤りとして弾くのは組み立て口
// （etoki.NewAuthenticator）で、ここでは閉じる側に倒すだけにする。
//
// **組み立て口が渡すのは 1 人だけ**（#492）。ここで複数を受けるのは、共有
// （ADR 0017）が 2 人目を前提にしていて、そのテストが 2 人目のログインを要る
// ため。共有を外すとき（#495）に 1 人へ戻す。
func NewAuthService(
	provider port.IdentityProvider, sessions port.SessionRepository, allowedSubjects []string,
	opts ...AuthServiceOption,
) *AuthService {
	s := &AuthService{
		provider:        provider,
		sessions:        sessions,
		now:             time.Now,
		allowedSubjects: slices.Clone(allowedSubjects),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Provider は認証基盤の識別子を返す。
func (s *AuthService) Provider() string { return s.provider.Name() }

// allows はその利用者をログインさせてよいかを返す。
//
// **認証基盤の名前も見る。** Subject は基盤の中でだけ一意なので、別の基盤の
// 同じ番号を通さない。
func (s *AuthService) allows(provider, subject string) bool {
	return subject != "" && provider == s.provider.Name() && slices.Contains(s.allowedSubjects, subject)
}

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

	state, err := randomToken()
	if err != nil {
		return "", err
	}

	now := s.now()
	// **上限は state を書く前に見る。** 後ろに置くと、断ったリクエストも 1 行
	// 書いてから断ることになり、守ろうとしている当のものが増える。
	if err := s.admitLoginStart(now); err != nil {
		return "", err
	}

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

// admitLoginStart は窓の中の開始回数を見て、1 回ぶんを数える。
//
// 上限に当たったら ErrRateLimited（429）で断る。**待たせない。** ブラウザ側で
// 「止まった」のか「並んでいる」のかが見えなくなる（LLMLimiter と同じ判断）。
//
// **窓は StateTTL と同じにする。** 数えているのは「この瞬間 oauth_states に
// 残りうる行」なので、窓を別に持つと 2 つの値がずれる。
func (s *AuthService) admitLoginStart(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.loginStarts = withinWindow(s.loginStarts, now.Add(-StateTTL))
	if len(s.loginStarts) >= MaxLoginStarts {
		return fmt.Errorf("%w: %d logins started within %s, limit is %d",
			ErrRateLimited, len(s.loginStarts), StateTTL, MaxLoginStarts)
	}
	s.loginStarts = append(s.loginStarts, now)

	return nil
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

	identity, err := s.provider.Exchange(ctx, code, redirectURI)
	if err != nil {
		return LoginResult{}, err
	}

	// **照らすのは利用者を作る前。** 断った相手を users に残すと、招待の引き当て
	// （FindUserByLogin）に現れる。
	if !s.allows(identity.Provider, identity.Subject) {
		return LoginResult{}, fmt.Errorf("%w: %s user %q (id %s)",
			ErrLoginNotAllowed, identity.Provider, identity.Login, identity.Subject)
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

	token, err := randomToken()
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

	return s.FindAllowedUser(ctx, session.UserID)
}

// FindAllowedUser は ID で利用者を引き、ログインを通す相手なら返す。
//
// 見つからない・通さない相手は (nil, nil)。呼び出し側が未ログインとして扱う。
// セッション（Resolve）と MCP のトークンの持ち主を確かめるのに使う。どちらも
// 発行した時点のログインで決まっているので、通す相手を変えたあとも残りうる。
func (s *AuthService) FindAllowedUser(ctx context.Context, userID string) (*port.User, error) {
	// セッションはあるのに利用者が消えている場合も未ログインとして扱う。
	user, err := s.sessions.FindUser(ctx, userID)
	if err != nil || user == nil {
		return nil, err
	}
	if !s.allows(user.Provider, user.Subject) {
		return nil, nil
	}
	return user, nil
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
// 消すのはこのセッションだけ。同じ利用者が別の端末からも使っている可能性が
// あり、片方のログアウトで全部を切るのは意図に合わない。
func (s *AuthService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.DeleteSession(ctx, HashSessionToken(token))
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
