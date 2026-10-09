package port

import (
	"context"
	"time"
)

// MCP のクライアントに etoki が発行する許可（ADR 0076）。
//
// 画面のログイン（SessionRepository）とは別の型にしてある。寿命も失効の
// 仕方も違い、同じ型に混ぜると片方の都合でもう片方を壊す。SessionRepository に
// メソッドを足すと、それを自前実装している外部リポジトリが壊れる理由もある。

// OAuthClient は動的登録（RFC 7591）で登録したクライアント。
//
// Client ID Metadata Document で名乗るクライアントはここに入らない。
// メタデータの正本は相手の URL にあり、写すと古くなる。
type OAuthClient struct {
	// ID は etoki が発番する client_id。
	ID string
	// Name はクライアントが名乗った名前。同意の画面に出す。
	Name string
	// RedirectURIs は登録された戻り先。
	RedirectURIs []string
	// CreatedAt は登録した時刻。
	CreatedAt time.Time
}

// OAuthCode は発行した認可コード 1 つ。
//
// 許可（OAuthGrant）はトークンと引き換えたときに作る。同意しただけで引き換え
// られなかったものを、許可の一覧に並べないため。
type OAuthCode struct {
	// CodeHash はコードの SHA-256。値そのものは保存しない。
	CodeHash string
	// UserID は同意した利用者。
	UserID string
	// ClientID と ClientName は同意したときのクライアント。
	ClientID   string
	ClientName string
	// Scope と Resource は許した範囲（RFC 8707 の resource）。
	Scope    string
	Resource string
	// RedirectURI は認可の要求に載っていた戻り先。引き換えで一致を見る。
	RedirectURI string
	// CodeChallenge は PKCE の S256 の値。
	CodeChallenge string
	// CreatedAt と ExpiresAt は発行時刻と期限。
	CreatedAt time.Time
	ExpiresAt time.Time

	// GrantID は引き換えのときに作る許可の ID。**ConsumeCode の出力。**
	//
	// 2 回目の引き換えでは 1 回目に作った許可を指す。使い回されたコードは
	// 漏れたと見なし、そこから出したトークンごと失効させる（RFC 6749 4.1.2）。
	GrantID string
	// Replayed は 2 回目以降の引き換えなら真。**ConsumeCode の出力。**
	Replayed bool
}

// OAuthGrant は利用者がクライアントに許した 1 件。
//
// トークンは許可にぶら下がる。**失効は許可ごとに行う。** トークン 1 本ずつを
// 人に選ばせても見分けがつかない。
type OAuthGrant struct {
	ID         string
	UserID     string
	ClientID   string
	ClientName string
	Scope      string
	Resource   string
	CreatedAt  time.Time
	// LastUsedAt は最後にトークンを発行した時刻。
	//
	// アクセストークンを使うたびには書かない。読み取りの道具を呼ぶたびに
	// 書き込みが起きる。更新の間隔（アクセストークンの寿命）の粒度で足りる。
	LastUsedAt time.Time
}

// OAuthTokenKind はトークンの種類。
type OAuthTokenKind string

const (
	// OAuthAccessToken は `/mcp` に送るトークン。
	OAuthAccessToken OAuthTokenKind = "access"
	// OAuthRefreshToken はアクセストークンを取り直すトークン。使い捨て。
	OAuthRefreshToken OAuthTokenKind = "refresh"
)

// OAuthToken は発行したトークン 1 本。
type OAuthToken struct {
	// TokenHash はトークンの SHA-256。値そのものは保存しない（ADR 0015 と同じ）。
	TokenHash string
	Kind      OAuthTokenKind
	CreatedAt time.Time
	ExpiresAt time.Time
}

// OAuthTokenGrant はトークンを引いた結果。トークンとその許可。
type OAuthTokenGrant struct {
	Token OAuthToken
	Grant OAuthGrant
}

// OAuthRefresh は refresh token を使った結果。
type OAuthRefresh struct {
	Grant OAuthGrant
	// Replayed は使用済みの refresh token がもう一度来たなら真。
	//
	// 漏れた値が使われたと見なし、呼び出し側が許可ごと失効させる。
	Replayed bool
}

// OAuthGrantRepository は MCP の認可（ADR 0076）の状態を永続化する。
//
// 時刻は呼び出し側が与える（SessionRepository と同じ方針）。期限切れの掃除は
// 書き込みのついでに行い、別の口を持たない。
type OAuthGrantRepository interface {
	// CreateClient は登録したクライアントを保存する。
	//
	// ついでに、unusedBefore より前に登録されて一度も許可を得なかった
	// クライアントを消す。登録は誰でもできるので、消さないと表が育つ。
	CreateClient(ctx context.Context, c OAuthClient, unusedBefore time.Time) error
	// FindClient は ID でクライアントを引く。無ければ (nil, nil)。
	FindClient(ctx context.Context, id string) (*OAuthClient, error)

	// SaveCode は認可コードを保存する。ついでに期限切れのコードを消す。
	SaveCode(ctx context.Context, c OAuthCode) error
	// ConsumeCode は期限内のコードを使用済みにして返す。
	//
	// 1 回目は grantID を控えて返す（Replayed は偽）。2 回目以降は 1 回目に
	// 控えた GrantID を載せて Replayed を真にして返す。未知と期限切れは
	// (nil, nil)。**使用済みにする照合は 1 文で行う。** 同じコードで 2 本
	// 同時に来ても、1 回目になれるのは 1 本だけ。
	ConsumeCode(ctx context.Context, codeHash, grantID string, now time.Time) (*OAuthCode, error)

	// CreateGrant は許可と最初のトークンを 1 つのトランザクションで保存する。
	//
	// ついでに期限切れのトークンと、トークンが 1 本も残っていない許可を消す。
	CreateGrant(ctx context.Context, g OAuthGrant, tokens []OAuthToken) error
	// FindToken は期限内のトークンをその許可ごと引く。無ければ (nil, nil)。
	//
	// 使用済みの refresh token も (nil, nil)。
	FindToken(ctx context.Context, tokenHash string, now time.Time) (*OAuthTokenGrant, error)
	// UseRefreshToken は refresh token を使用済みにして、許可を返す。
	//
	// 期限内で未使用なら使用済みにして返す。期限内で使用済みなら Replayed を
	// 真にして返す。未知と期限切れは (nil, nil)。照合は 1 文で行う。
	UseRefreshToken(ctx context.Context, tokenHash string, now time.Time) (*OAuthRefresh, error)
	// AddTokens は許可にトークンを足し、LastUsedAt を now にする。
	//
	// 許可がもう無ければ ErrNotFound。更新のあいだに取り消された場合に、
	// 取り消した許可へトークンを足さないため。
	AddTokens(ctx context.Context, grantID string, tokens []OAuthToken, now time.Time) error

	// FindGrant は ID で許可を引く。無ければ (nil, nil)。
	FindGrant(ctx context.Context, id string) (*OAuthGrant, error)
	// ListGrants は利用者の許可を新しい順に返す。
	ListGrants(ctx context.Context, userID string) ([]OAuthGrant, error)
	// ListAllGrants は全員の許可を新しい順に返す。CLI（`etoki grants`）が使う。
	//
	// ListGrants の空文字で表さない。空文字は「認証なしの操作者」1 人を指す
	// 値で（ADR 0016）、「全員」と読ませると意味が 2 つになる。
	ListAllGrants(ctx context.Context) ([]OAuthGrant, error)
	// DeleteGrant は許可をトークンごと消す。無くても誤りとしない。
	DeleteGrant(ctx context.Context, id string) error
}
