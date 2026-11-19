package port

import "context"

// AccountRepository は利用者の行を消す（ADR 0101）。**任意。** 渡さなければ、
// 利用者を消す口は開かない（503）。
//
// SessionRepository とは別の型にしてある。SessionRepository にメソッドを足すと、
// それを自前実装している外部リポジトリが壊れる（OAuthGrantRepository と同じ理由）。
type AccountRepository interface {
	// DeleteUser は利用者の行を消す。存在しなくても誤りとしない。
	//
	// **その利用者のセッション・資格情報・MCP の許可（認可コードとトークンを
	// 含む）も一緒に消す。** 消し残すと、消えた利用者のセッションやトークンが
	// 生きたまま残る。
	//
	// **ボードとメンバーの行は消さない。** どれを消してどれを残すかは
	// ユースケース層が決め、先に片付けてから呼ぶ（ADR 0101）。
	DeleteUser(ctx context.Context, userID string) error
}
