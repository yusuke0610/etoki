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

// ErrForbidden は下流サービスが権限を理由に拒んだことを表す。
//
// etoki は設定した PAT で GitHub を叩くので（ADR 0106）、その PAT にリポジトリ
// へのアクセス権が無ければここに来る。**etoki 側にその権限を複製しない**と決めた
// 以上、拒否は実行時にしか分からない（ADR 0017）。500 に丸めると、権限の
// 問題だと分からない。
var ErrForbidden = errors.New("etoki: forbidden by the upstream service")

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

	// Exchange はコールバックで受け取った code を利用者に引き換える。
	//
	// redirectURI は AuthorizeURL に渡したものと同じ値。基盤によっては
	// 引き換え時にも一致を要求される。
	//
	// **返すのは誰であるかだけ。** 引き換えで基盤のトークンを得ても、etoki は
	// それを下流を叩く資格情報にしない（ADR 0106）。誰であるかを確かめたら捨てる。
	Exchange(ctx context.Context, code, redirectURI string) (Identity, error)
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

// OAuthState は発行した OAuth の state 1 つ。
//
// **戻り先を一緒に持つ。** 認可のコールバックの URL に載せると、戻ってきた
// URL の中身が戻り先を決めることになり、オープンリダイレクトを塞ぐ責任が
// 毎回の照合に移る。state はサーバー発行・単回使用・期限つきなので、同じ
// 寿命のものを 1 つ増やすだけで済む（ADR 0059）。
type OAuthState struct {
	// State は照合に使う値そのもの。
	State string
	// ReturnTo はログイン後に戻す先。空文字は「知らない」。
	//
	// **自オリジンの相対パスだけが入る。** 検証はユースケース層が行い、
	// 永続化層は受け取った値をそのまま書く（検証を 2 箇所に置かない）。
	ReturnTo string
	// CreatedAt は発行時刻。**SaveState への入力。**
	CreatedAt time.Time
	// ExpiresAt は受け付けを打ち切る時刻。**SaveState への入力。**
	//
	// ConsumeState は照合を済ませて返すので、この 2 つを埋めない（下記）。
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
	// FindUserByLogin は login で利用者を引く。存在しなければ (nil, nil)。
	//
	// 人が打つ識別子は login しかないので、CLI から利用者を指すのに要る。
	// login は改名で変わるため、同定には使わない（それは Subject の仕事）。
	FindUserByLogin(ctx context.Context, provider, login string) (*User, error)
	// FindUsers は ID をまとめて引く。見つからなかった ID は結果に現れない。
	//
	// メンバー一覧に表示名を添えるのに要る。1 人ずつ引くと、メンバーの数だけ
	// 問い合わせが増える。**欠けを誤りにしない。** 利用者を消してもボードは
	// 残る（ADR 0016）ので、指し先の無いメンバーは普通に起こる。
	FindUsers(ctx context.Context, ids []string) ([]User, error)

	// CreateSession はセッションを保存する。
	CreateSession(ctx context.Context, s Session) error
	// FindSession は token のハッシュでセッションを引く。
	//
	// 期限切れは存在しないものとして (nil, nil) を返す。
	FindSession(ctx context.Context, tokenHash string, now time.Time) (*Session, error)
	// DeleteSession はセッションを消す。存在しなくても誤りとしない。
	DeleteSession(ctx context.Context, tokenHash string) error

	// ConsumeState は state を照合して削除し、保存してあった内容を返す。
	//
	// 単回使用。一度使った state で 2 回目は (nil, nil) を返す。期限切れも同じ。
	//
	// **埋めるのは State と ReturnTo だけでよい。** 時刻を読む呼び出し側が無い。
	// 読まれない値のために解析を増やすと、その失敗が通った state を誤りに変える。
	ConsumeState(ctx context.Context, state string, now time.Time) (*OAuthState, error)
	// SaveState は発行した state を保存する。
	SaveState(ctx context.Context, s OAuthState) error
}

// userIDKey は etoki 側の利用者 ID を context に載せるための鍵。
//
// 独自型にして衝突を避ける。文字列の鍵は他のパッケージと衝突しうる。
//
// 運ぶのは利用者 ID だけ。Identity ごと載せる案もあったが、読む側が
// 現れないまま公開面が増える。**どこからも読まれない出入口は、実装しても
// 何も起きない。** 必要になってから足す（HeaderIdentityProvider を落とした
// のと同じ理由）。表示名が要る HTTP 層は gin の context で持ち回っている。
type userIDKey struct{}

// ContextWithUserID は etoki 側の利用者 ID を載せた context を返す。
//
// port に置いたのは、外部リポジトリが GitHubTokenSource を自前実装するときに
// 読むためだった。その口は外したので（ADR 0106）、いまの読み手は internal/ の
// 中（ボードの認可と LLM の上限）だけ。置き場所は公開 API 面の約束を外すとき
// （#493）に見直す。
//
// Identity.Subject は認証基盤の ID であって etoki の users.id ではない。
// ボードの持ち主やメンバーが指すのは後者なので、載せるのはこちら。
func ContextWithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey{}, id)
}

// UserIDFromContext は context から etoki 側の利用者 ID を取り出す。
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey{}).(string)
	return id, ok
}
