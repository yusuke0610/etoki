package port

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound は更新対象が存在しないことを表す。
//
// 取得系（Find 系）は「無い」を異常として扱わず nil を返すため、このエラーは
// 更新系でのみ返る。
var ErrNotFound = errors.New("etoki: not found")

// ErrAlreadyExists は作ろうとしたものがすでにあることを表す。
//
// 同じ利用者を 2 回招待した場合に返る。既存の行を黙って書き換えると、
// 「招待した」と「ロールを変えた」を呼び出し側が区別できなくなる。
var ErrAlreadyExists = errors.New("etoki: already exists")

// ErrConflict は更新の基準にした版が、いまの版と食い違うことを表す。
//
// ErrNotFound と分ける。「書けなかった」は同じでも、直し方が違う。無いものは
// 待っても現れないが、食い違いは相手の変更を取り込めば進める（ADR 0020）。
var ErrConflict = errors.New("etoki: stale base version")

// BoardTarget は draft issue の作成先。
//
// ボードごとに持つ。3 つとも空なら未選択（ADR 0014）。
type BoardTarget struct {
	// RepositoryOwner は対象リポジトリの所有者。
	RepositoryOwner string
	// RepositoryName は対象リポジトリの名前。
	RepositoryName string
	// ProjectID は draft issue を作る Projects v2 の node ID。
	ProjectID string
	// ProjectNumber は Project の番号。表示用のスナップショット（ADR 0019）。
	ProjectNumber int
	// ProjectTitle は Project の名前。表示用のスナップショット（ADR 0019）。
	ProjectTitle string
	// ProjectURL は Project の URL。表示用のスナップショット（ADR 0025）。
	//
	// 組み立てずに GitHub が返したものを控える。Projects v2 の URL は owner が
	// user か org かで形が変わるが、etoki はどちらなのかを知らない。空文字は
	// 「URL を知らない」で、そのときフロントはリポジトリの Projects タブへ落とす。
	ProjectURL string
}

// BoardTargetDisplay は作成先の表示用スナップショット（ADR 0019 / 0025）。
//
// **作成先そのもの（owner / name / projectId）を含まない。** 固定後に更新
// できるのはここに挙がっているものだけ、を型で示すため（ADR 0037）。
type BoardTargetDisplay struct {
	// ProjectNumber は Project の番号。取得していなければ 0。
	ProjectNumber int
	// ProjectTitle は Project の名前。取得していなければ空文字。
	ProjectTitle string
	// ProjectURL は Project の URL。組み立てず GitHub が返したものを控える。
	ProjectURL string
}

// Selected は作成先が選ばれているかどうかを返す。
//
// **見るのは 3 つだけ。** ProjectNumber と ProjectTitle は表示用であり、
// 取得できていなくても作成先は決まっている（ADR 0019）。ここに足すと、
// 名前を送らずに設定した正しい作成先が「未選択」に落ちる。
func (t BoardTarget) Selected() bool {
	return t.RepositoryOwner != "" && t.RepositoryName != "" && t.ProjectID != ""
}

// Board は 1 枚の Excalidraw ボード。
//
// スナップショットとバージョニングは行わないため、Scene は常に最新状態のみを
// 保持する。過去の状態が必要な場合、利用者がボードを複製する運用に委ねる。
//
// 所有者は持たない。誰がどう関われるかはボードの属性ではなくメンバーシップ
// なので、BoardMember が持つ（ADR 0017）。
type Board struct {
	// ID はサーバーが発番する UUID。
	ID string
	// Name は表示名。
	Name string
	// Scene は Excalidraw のシーン JSON。**貼った画像の実体は入らない。**
	// 画像は BoardFile として別に持つ（ADR 0074）。
	Scene string
	// Target は draft issue の作成先。未選択ならゼロ値。
	Target BoardTarget
	// CreatedAt は作成時刻。
	CreatedAt time.Time
	// UpdatedAt は最終更新時刻。
	UpdatedAt time.Time
}

// BoardFile はキャンバスに貼った画像 1 枚（ADR 0074）。
//
// シーンとは別に持つ。シーンに入れておくと、図形を 1 つ動かしただけの保存でも
// 画像を丸ごと書き直し、シーンを読むたびに画像まで読むことになる。
type BoardFile struct {
	// ID は Excalidraw が振った画像の ID。ボードの中で一意で、画像の要素の
	// fileId がこれを指す。
	ID string
	// Data は Excalidraw の画像データ（BinaryFileData）を JSON にしたもの。
	// **etoki は中身を解釈しない。** シーンと同じく、受け取ったものを
	// そのまま返す。
	Data string
}

// SceneWrite はシーンの保存 1 回で書くもの（ADR 0074）。
type SceneWrite struct {
	// Scene は画像の実体を抜いたシーン JSON。
	Scene string
	// Added は今回の保存で足す画像。同じ ID がすでにあれば置き換える。
	Added []BoardFile
	// Referenced はシーンが参照している画像の ID。**これに入っていない画像は
	// 消す。** シーンから外れた画像を残すと、使っていない画像のぶんだけ
	// 上限が埋まっていく。
	Referenced []string
}

// BoardRole はボードに対する権限の強さ（ADR 0017）。
type BoardRole string

// BoardRole の取りうる値。
const (
	// RoleOwner は招待とロール変更、作成先の変更ができる。
	RoleOwner BoardRole = "owner"
	// RoleEditor はブレストと解釈と draft issue の作成ができる。
	//
	// 作成できるかを最終的に決めるのは GitHub。etoki は実行者のトークンで
	// 叩くので、リポジトリへのアクセス権が無ければ GitHub 側が拒む。
	RoleEditor BoardRole = "editor"
	// RoleViewer は読むだけ。
	//
	// 解釈も許さない。解釈は LLM を叩く外部呼び出しであり課金も伴うため、
	// 閲覧者に許すのは「閲覧」ではない（ADR 0017）。
	RoleViewer BoardRole = "viewer"
)

// rank はロールの強さを数にする。定義済みでなければ 0。
//
// **ロールの上下を知っているのはここだけ。** SQL にロールの集合を書くと
// 判定が 2 箇所になり、片方だけ変わる。永続化層が見るのは「メンバーかどうか」
// までにとどめる（ADR 0017）。
func (r BoardRole) rank() int {
	switch r {
	case RoleOwner:
		return 3
	case RoleEditor:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// Valid は r が定義済みのロールかどうかを返す。
func (r BoardRole) Valid() bool { return r.rank() > 0 }

// AtLeast は r が min 以上の権限を持つかどうかを返す。
//
// どちらかが未定義なら false。未知のロールを「とりあえず通す」方に倒さない。
func (r BoardRole) AtLeast(min BoardRole) bool {
	if !r.Valid() || !min.Valid() {
		return false
	}
	return r.rank() >= min.rank()
}

// BoardMember は 1 人のメンバー。
type BoardMember struct {
	// BoardID は対象ボードの ID。
	BoardID string
	// UserID は利用者の ID。
	//
	// 空文字は無効値ではなく「認証なしの所有者」1 人を表す（ADR 0016）。
	UserID string
	// Role は権限。
	Role BoardRole
	// CreatedAt はメンバーになった時刻。
	CreatedAt time.Time
}

// BoardAccess は操作者から見たボード 1 枚。
//
// ボードとロールを別々に引くと、引き当てと権限判定で 2 往復する。ロールは
// ボードの属性ではないが、「誰が見ているか」が決まれば 1 つに定まる。
type BoardAccess struct {
	// Board はボード本体。
	Board Board
	// Role は操作者のロール。
	Role BoardRole
}

// BoardRepository はボードとそのメンバーを永続化する。
//
// 時刻は呼び出し側が与える。実装が time.Now を握ると挙動が時計に依存し、
// テストが書きづらくなるため。
//
// **操作者は引数で受け取る。** ctx から実装が勝手に読む形にすると、絞り忘れても
// 動いてしまう。引数なら渡し忘れがコンパイルエラーになる（ADR 0016）。
// メンバーでないボードは「存在しない」ものとして扱い、権限エラーとは区別しない。
//
// **ここで見るのは「メンバーかどうか」まで。** ロールの上下は BoardRole.AtLeast が
// 1 箇所で持つ（ADR 0017）。SQL にロールの集合を書くと判定が 2 箇所になる。
type BoardRepository interface {
	// Create は新しいボードを保存し、owner をそのボードの RoleOwner にする。
	// ID が既存なら誤りとして扱う。
	//
	// ボードとメンバーは 1 トランザクションで入れる。片方だけ残ると、誰も
	// 開けないボードか、指す先の無いメンバーができる。
	Create(ctx context.Context, b Board, owner string) error
	// UpdateScene はシーンと画像と更新時刻を更新し、書いたあとにボードが
	// 持っている画像の ID を返す。CreatedAt は変えない。
	//
	// base は呼び出し側が編集の基準にした UpdatedAt。いまの版と違えば何も
	// 書かずに ErrConflict を返す（ADR 0020）。**照合と更新は 1 文で行う。**
	// 読んでから書く形にすると、その隙間に入った保存を上書きする。
	//
	// **シーンと画像は 1 トランザクションで書く**（ADR 0074）。照合に負けたら
	// 画像も足さず消さない。途中まで進んだ状態が残ると、シーンが指す画像が
	// 欠けるか、どのシーンからも指されない画像が残る。
	UpdateScene(
		ctx context.Context, actor, id string, w SceneWrite, base, updatedAt time.Time,
	) ([]string, error)
	// UpdateName は名前だけを更新する。
	//
	// **更新時刻は進めない。** `UpdatedAt` はシーンの版であり、保存の照合基準
	// として使われている（ADR 0020）。名前を直しただけで進めると、そのボードを
	// 開いている別のメンバーの次の保存が「他の人が保存した」で断られる。誰も
	// シーンを触っていないのに衝突として読まれることになるので、基準を取らない
	// 書き込みで版を前に出さない（ClaimUnowned と同じ考え方）。
	UpdateName(ctx context.Context, actor, id, name string) error
	// UpdateTarget は作成先だけを更新する。Scene は変えない。
	//
	// **更新時刻は進めない**（UpdateName と同じ、ADR 0092）。版を動かすのは
	// シーンの保存だけ。作成先を設定しただけで進めると、そのボードを開いている
	// 別のメンバーの次の保存が、誰もシーンを触っていないのに断られる。
	//
	// 固定済みかどうかはここでは見ない。判断に sync_runs が要るため、
	// ユースケース層が担う（ADR 0014）。
	UpdateTarget(ctx context.Context, actor, id string, t BoardTarget) error
	// UpdateTargetDisplay は作成先の表示用スナップショットだけを更新する。
	// **作成先そのものも、更新時刻も変えない**（ADR 0037 / 0092）。
	//
	// 固定済みのボードでも通る唯一の経路（ADR 0037）。作成先の 3 列を書ける
	// ようにしないこと。書けるようにすると、固定が意味を失う。
	//
	// 送られた ID が保存されている作成先と同じかどうかはここでは見ない。
	// 引き当ててから比べる必要があるので、ユースケース層が担う。
	UpdateTargetDisplay(ctx context.Context, actor, id string, d BoardTargetDisplay) error
	// Delete はボードを消す。存在しない、または操作者がメンバーでなければ
	// ErrNotFound。
	//
	// **ロールは見ない。** owner だけかはユースケース層が決める（他と同じ）。
	//
	// board_members / sync_runs / sync_items は一緒に消える。GitHub に作った
	// draft issue は消さない（消せない）ので、**残った draft issue の出どころは
	// 辿れなくなる**（ADR 0007 / 0042）。押す前に何が残るかを見せるのは
	// 呼び出し側の責務。
	Delete(ctx context.Context, actor, id string) error
	// Find は ID でボードを操作者のロールつきで引く。
	//
	// 存在しない、または操作者がメンバーでなければ (nil, nil)。
	Find(ctx context.Context, actor, id string) (*BoardAccess, error)
	// FindWithFiles は Find に加えて、そのボードの画像をすべて返す。
	//
	// **シーンと画像は同じ読み取りで揃える。** 別々に読むと、間に入った保存で
	// シーンが指す画像が欠ける（ADR 0074）。画像まで要るのはボードを開くとき
	// だけなので、Find には混ぜない。
	FindWithFiles(ctx context.Context, actor, id string) (*BoardAccess, []BoardFile, error)
	// FileSizes はボードが持っている画像の ID ごとのバイト数（Data の長さ）を
	// 返す。画像の中身は読まない。
	//
	// **操作者では絞らない。呼ぶ前に Find で確かめること**（ListMembers と同じ）。
	FileSizes(ctx context.Context, boardID string) (map[string]int64, error)
	// List は操作者がメンバーであるボードを UpdatedAt の降順で返す。
	//
	// **Board.Scene は空文字で返る。** 一覧はシーンを返さないので読まない。
	// 捨てるために全ボードぶんをメモリへ載せることになる。中身が要るなら
	// Find で 1 枚ずつ引く。
	List(ctx context.Context, actor string) ([]BoardAccess, error)

	// ListMembers はボードのメンバーを返す。
	//
	// **操作者では絞らない。呼ぶ前に Find で確かめること。** メンバー一覧は
	// board_id でしか引けず、その board を取れるのはメンバーだけなので、
	// 二重に絞ると、絞り忘れたときにどちらが効いているのか分からなくなる
	// （sync_runs と同じ理由、ADR 0016）。
	ListMembers(ctx context.Context, boardID string) ([]BoardMember, error)
	// AddMember はメンバーを 1 人足す。すでにメンバーなら ErrAlreadyExists。
	//
	// **操作者では絞らない。呼ぶ前に Find で owner であることを確かめること。**
	// ここに「owner だけ」を書くとロールの上下が SQL と Go の 2 箇所になる
	// （ADR 0017）。以下の 2 つも同じ。
	AddMember(ctx context.Context, m BoardMember) error
	// UpdateMemberRole はメンバーのロールを変える。
	// メンバーでなければ ErrNotFound。
	UpdateMemberRole(ctx context.Context, boardID, userID string, role BoardRole) error
	// RemoveMember はメンバーを外す。メンバーでなければ ErrNotFound。
	RemoveMember(ctx context.Context, boardID, userID string) error

	// CountUnowned は所有者の無いボードの数を返す。
	//
	// 認証を有効にした直後に、見えなくなったボードがあることを起動時に
	// 知らせるために使う。黙って消さない（ADR 0016）。
	CountUnowned(ctx context.Context) (int, error)
	// ClaimUnowned は所有者の無いボードをすべて owner のものにし、件数を返す。
	//
	// 更新時刻は変えない。引き受けはボードの中身を変えていないので、一覧の
	// 並びが入れ替わる理由が無い。
	ClaimUnowned(ctx context.Context, owner string) (int64, error)
}
