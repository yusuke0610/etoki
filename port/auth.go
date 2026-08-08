package port

import (
	"context"
	"errors"
	"time"
)

// ErrNotAuthenticated は利用者を特定できないことを表す。
//
// 認証を設定していない構成では起きない。設定してある場合に、セッションが無い
// か失効しているときに返る。
var ErrNotAuthenticated = errors.New("etoki: not authenticated")

// Identity は認証基盤が返す利用者。
//
// 認証基盤ごとに持てる情報は違う。ここに置くのは、どの基盤でも決まる最小限
// だけにしてある。GitHub 固有の値を足したくなったら、それは etoki が特定の
// 基盤を前提にし始めた合図（ADR 0015）。
type Identity struct {
	// Provider は認証基盤の識別子。"github" など。Subject と組で一意になる。
	Provider string
	// Subject は基盤内で不変な利用者 ID。
	//
	// 表示名やログイン名は変わりうるので、同定にはこちらを使う。
	Subject string
	// Login は基盤上のログイン名。表示と、GitHub の owner 照合に使う。
	Login string
	// DisplayName は画面に出す名前。空なら Login を使う。
	DisplayName string
}

// Credentials は認証の結果として得た、下流サービスを叩くための資格情報。
//
// RefreshToken と 2 つの期限は、失効しない基盤ではゼロ値でよい。
// ゼロ値なら「更新しない」を意味する。
type Credentials struct {
	// AccessToken は下流サービスに送るトークン。
	AccessToken string
	// RefreshToken は AccessToken を更新するためのトークン。
	RefreshToken string
	// ExpiresAt は AccessToken の失効時刻。ゼロ値なら失効しない。
	ExpiresAt time.Time
	// RefreshExpiresAt は RefreshToken の失効時刻。ゼロ値なら失効しない。
	RefreshExpiresAt time.Time
}

// Expiring は失効する資格情報かどうかを返す。
func (c Credentials) Expiring() bool {
	return !c.ExpiresAt.IsZero() && c.RefreshToken != ""
}

// IdentityProvider は利用者を外部の認証基盤に問い合わせる。
//
// OAuth2 / OIDC のような「送り出して引き換える」2 段を想定している。
// state は etoki が発行して照合するので、実装は受け取って渡すだけでよい。
// 実装側で CSRF 対策を持つ必要はない。
type IdentityProvider interface {
	// Name は基盤の識別子。Identity.Provider に入る値と揃える。
	Name() string

	// AuthorizeURL は利用者を送り出す先を返す。
	AuthorizeURL(state, redirectURI string) string

	// Exchange はコールバックで受け取った code を利用者と資格情報に引き換える。
	//
	// redirectURI は AuthorizeURL に渡したものと同じ値。基盤によっては
	// 引き換え時にも一致を要求される。
	Exchange(ctx context.Context, code, redirectURI string) (Identity, Credentials, error)

	// Refresh は資格情報を更新する。
	//
	// Credentials.Expiring() が false の基盤では呼ばれない。呼ばれたくない
	// 実装は ErrNotAuthenticated を返してよい。再ログインに落ちる。
	Refresh(ctx context.Context, refreshToken string) (Credentials, error)
}

// GitHubTokenSource は GitHub を叩くトークンを返す。
//
// ctx から利用者を引く。IdentityProvider とは別の継ぎ目にしてあるので、
// 「別の基盤で認証し、GitHub へは固定のトークンで書く」構成が作れる（ADR 0015）。
type GitHubTokenSource interface {
	// Token は現在の利用者のトークンを返す。
	//
	// 失効間際なら更新してから返してよい。特定できなければ
	// ErrNotAuthenticated を返す。
	Token(ctx context.Context) (string, error)
}

// Session は 1 つのログイン。
type Session struct {
	// TokenHash はセッション token のハッシュ。
	//
	// cookie に載せた値そのものは保存しない。DB が漏れても生きたセッションに
	// ならないようにするため。ハッシュ化は永続化層より手前で行う。
	TokenHash string
	// UserID は Identity に対応づけた利用者の ID。
	UserID string
	// CreatedAt は発行時刻。
	CreatedAt time.Time
	// ExpiresAt は失効時刻。
	ExpiresAt time.Time
}

// User は etoki 側に記録した利用者。
type User struct {
	// ID は etoki が発番する ID。
	ID string
	// Provider と Subject は Identity のものと同じ。組で一意。
	Provider string
	Subject  string
	// Login と DisplayName は変わりうる。ログインのたびに上書きする。
	Login       string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SessionRepository は認証まわりの状態を永続化する。
//
// 時刻は呼び出し側が与える。実装が time.Now を握ると挙動が時計に依存し、
// テストが書きづらくなる（BoardRepository と同じ方針）。
type SessionRepository interface {
	// UpsertUser は Provider と Subject で利用者を引き当て、無ければ作る。
	//
	// Login と DisplayName は変わりうるので、既存でも上書きする。
	UpsertUser(ctx context.Context, u User) (User, error)
	// FindUser は ID で利用者を引く。存在しなければ (nil, nil) を返す。
	FindUser(ctx context.Context, id string) (*User, error)

	// CreateSession はセッションを保存する。
	CreateSession(ctx context.Context, s Session) error
	// FindSession は token のハッシュでセッションを引く。
	//
	// 期限切れは存在しないものとして (nil, nil) を返す。
	FindSession(ctx context.Context, tokenHash string, now time.Time) (*Session, error)
	// DeleteSession はセッションを消す。存在しなくても誤りとしない。
	DeleteSession(ctx context.Context, tokenHash string) error

	// SaveCredentials は利用者の資格情報を保存する。既存なら置き換える。
	//
	// 暗号化は実装の責務。平文で置いてはならない（ADR 0015）。
	SaveCredentials(ctx context.Context, userID string, c Credentials, now time.Time) error
	// FindCredentials は利用者の資格情報を返す。無ければ (nil, nil)。
	FindCredentials(ctx context.Context, userID string) (*Credentials, error)

	// ConsumeState は state を照合して削除する。
	//
	// 単回使用。一度使った state で 2 回目は false を返す。期限切れも false。
	ConsumeState(ctx context.Context, state string, now time.Time) (bool, error)
	// SaveState は発行した state を保存する。
	SaveState(ctx context.Context, state string, createdAt, expiresAt time.Time) error
}

// identityKey は Identity を context に載せるための鍵。
//
// 独自型にして衝突を避ける。文字列の鍵は他のパッケージと衝突しうる。
type identityKey struct{}

// ContextWithIdentity は利用者を載せた context を返す。
//
// port に置くのは、外部リポジトリが GitHubTokenSource を自前実装するときに
// 読む必要があるため。internal/ は他モジュールから import できない（ADR 0001）。
func ContextWithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFromContext は context から利用者を取り出す。
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}

// userIDKey は etoki 側の利用者 ID を context に載せるための鍵。
//
// Identity.Subject は認証基盤の ID であって etoki の users.id ではない。
// トークンを引くのに後者が要るので別に持つ。
type userIDKey struct{}

// ContextWithUserID は etoki 側の利用者 ID を載せた context を返す。
func ContextWithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey{}, id)
}

// UserIDFromContext は context から etoki 側の利用者 ID を取り出す。
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey{}).(string)
	return id, ok
}
