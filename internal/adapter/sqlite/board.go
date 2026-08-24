package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// BoardRepository は port.BoardRepository の SQLite 実装。
type BoardRepository struct {
	db *sql.DB
}

// NewBoardRepository は BoardRepository を作る。
func NewBoardRepository(db *sql.DB) *BoardRepository {
	return &BoardRepository{db: db}
}

var _ port.BoardRepository = (*BoardRepository)(nil)

// summaryColumns はシーン以外の列。Find と List で同じ並びを使う。
//
// scanSummary が並び順に依存するので、片方だけ足すと取り違える。
// 末尾の role はボードの列ではなく、操作者から見たロール（ADR 0017）。
const summaryColumns = `b.id, b.name,
	b.repository_owner, b.repository_name, b.project_id,
	b.project_number, b.project_title, b.project_url,
	b.created_at, b.updated_at, m.role`

// boardColumns は Find が SELECT する列。シーンを末尾に足す。
//
// **一覧はこれを使わない。** BoardSummary はシーンを含まないので、List が
// 読んでも捨てるだけになる。シーンには画像が base64 で入りうるため、
// 捨てるために全ボードぶんをメモリに載せることになる。
//
// 足す位置が末尾なのは、summaryColumns の並びを Find と List で崩さないため。
// 途中に入れると、共有している側の受け皿とずれる。
const boardColumns = summaryColumns + `, b.scene`

// memberJoin は操作者がメンバーであるボードだけに絞る結合。
//
// 参照系はすべてこれを通す。WHERE で絞る形と違い、結合を書き忘れると
// role が取れずコンパイルも SQL も通らない。
const memberJoin = `FROM boards b
	JOIN board_members m ON m.board_id = b.id AND m.user_id = ?`

// Create は新しいボードを保存し、owner を RoleOwner のメンバーにする。
func (r *BoardRepository) Create(ctx context.Context, b port.Board, owner string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create board %s: %w", b.ID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO boards (id, name, scene,
		                     repository_owner, repository_name, project_id,
		                     project_number, project_title, project_url,
		                     created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.Name, b.Scene,
		b.Target.RepositoryOwner, b.Target.RepositoryName, b.Target.ProjectID,
		b.Target.ProjectNumber, b.Target.ProjectTitle, b.Target.ProjectURL,
		formatTime(b.CreatedAt), formatTime(b.UpdatedAt),
	); err != nil {
		return fmt.Errorf("insert board %s: %w", b.ID, err)
	}

	// 作った本人を owner にするのは同じトランザクションで行う。分けると、
	// 誰もメンバーでないボードが残りうる。それは誰にも開けず、消せもしない。
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO board_members (board_id, user_id, role, created_at)
		 VALUES (?, ?, ?, ?)`,
		b.ID, owner, string(port.RoleOwner), formatTime(b.CreatedAt),
	); err != nil {
		return fmt.Errorf("insert owner of board %s: %w", b.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create board %s: %w", b.ID, err)
	}

	return nil
}

// UpdateScene はシーンと更新時刻だけを更新する。
//
// base がいまの版と違えば何も書かず port.ErrConflict を返す（ADR 0020）。
func (r *BoardRepository) UpdateScene(
	ctx context.Context, actor, id, scene string, base, updatedAt time.Time,
) error {
	// メンバーであることを WHERE に入れる。ここは Find を通らずに直接 UPDATE
	// する経路なので、絞り忘れると他人のボードを書き換えられる（ADR 0016）。
	//
	// **ロールは見ない。** editor 以上かどうかはユースケース層が
	// BoardRole.AtLeast で判断する。ここに書くと判定が 2 箇所になる（ADR 0017）。
	//
	// 版の照合も同じ 1 文に置く。先に SELECT して比べる形にすると、比べてから
	// 書くまでの隙間に入った保存を上書きする。照合したい相手はその隙間に
	// 現れるので、隙間を作った時点で守れない。
	res, err := r.db.ExecContext(ctx,
		`UPDATE boards SET scene = ?, updated_at = ?
		 WHERE id = ? AND updated_at = ? AND `+memberExists,
		scene, formatTime(updatedAt), id, formatTime(base), actor)
	if err != nil {
		return fmt.Errorf("update board %s: %w", id, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for board %s: %w", id, err)
	}
	if n > 0 {
		return nil
	}

	// 0 行の理由は 2 つある。触れないボードなのか、版が古いのか。UPDATE は
	// どちらかを返さないので、触れるかどうかだけを引き直して分ける。ボードを
	// 消す経路が無いので、この 2 文の間に「触れたものが触れなくなる」は起きない。
	ok, err := r.readable(ctx, actor, id)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("update board %s: %w", id, port.ErrConflict)
	}
	return fmt.Errorf("update board %s: %w", id, port.ErrNotFound)
}

// readable は操作者がそのボードを読めるかどうかを返す。
//
// 絞りは参照系と同じ結合を通す。ここに条件を書き下すと「メンバーかどうか」の
// 定義が 2 箇所になる。Find で引き直さないのは、判断に要らないシーンまで
// 読み出すことになるため。
func (r *BoardRepository) readable(ctx context.Context, actor, id string) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx,
		`SELECT 1 `+memberJoin+` WHERE b.id = ?`, actor, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check access to board %s: %w", id, err)
	}
	return true, nil
}

// UpdateTarget は作成先と更新時刻だけを更新する。
func (r *BoardRepository) UpdateTarget(
	ctx context.Context, actor, id string, t port.BoardTarget, updatedAt time.Time,
) error {
	return r.exec(ctx, "board "+id,
		`UPDATE boards
		 SET repository_owner = ?, repository_name = ?, project_id = ?,
		     project_number = ?, project_title = ?, project_url = ?,
		     updated_at = ?
		 WHERE id = ? AND `+memberExists,
		t.RepositoryOwner, t.RepositoryName, t.ProjectID,
		t.ProjectNumber, t.ProjectTitle, t.ProjectURL, formatTime(updatedAt), id, actor)
}

// memberExists は更新系で操作者がメンバーであることを確かめる述語。
//
// 参照系の結合と対になる。board_id は相関参照で書く。プレースホルダにすると
// 同じ id を 2 回渡すことになり、片方だけ差し替える書き間違いを許す。
const memberExists = `EXISTS (
	SELECT 1 FROM board_members WHERE board_id = boards.id AND user_id = ?
)`

// CountUnowned は所有者の無いボードの数を返す。
func (r *BoardRepository) CountUnowned(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM board_members WHERE user_id = '' AND role = ?`,
		string(port.RoleOwner)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count unowned boards: %w", err)
	}
	return n, nil
}

// ClaimUnowned は所有者の無いボードをすべて owner のものにする。
func (r *BoardRepository) ClaimUnowned(ctx context.Context, owner string) (int64, error) {
	if owner == "" {
		// 空文字は「所有者が無い」そのもの。引き受けたことにならない。
		return 0, fmt.Errorf("claim boards: %w: owner is required", port.ErrNotFound)
	}

	// role の条件は CountUnowned と揃える。片方だけが招待された行まで拾うと、
	// 数えた件数と引き受けた件数が食い違い、owner のつもりで viewer の行を
	// 受け取ることになる。
	res, err := r.db.ExecContext(ctx,
		`UPDATE board_members SET user_id = ? WHERE user_id = '' AND role = ?`,
		owner, string(port.RoleOwner))
	if err != nil {
		return 0, fmt.Errorf("claim boards: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected for claim: %w", err)
	}
	return n, nil
}

// ListMembers はボードのメンバーを返す。
//
// 並びは古い順。owner が先頭に来るので、一覧の先頭が作った人になる。
func (r *BoardRepository) ListMembers(
	ctx context.Context, boardID string,
) ([]port.BoardMember, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT board_id, user_id, role, created_at FROM board_members
		 WHERE board_id = ? ORDER BY created_at, user_id`, boardID)
	if err != nil {
		return nil, fmt.Errorf("list members of board %s: %w", boardID, err)
	}
	defer func() { _ = rows.Close() }()

	var members []port.BoardMember
	for rows.Next() {
		var (
			m         port.BoardMember
			role      string
			createdAt string
		)
		if err := rows.Scan(&m.BoardID, &m.UserID, &role, &createdAt); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}

		m.Role = port.BoardRole(role)
		if m.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate members: %w", err)
	}

	return members, nil
}

// AddMember はメンバーを 1 人足す。
func (r *BoardRepository) AddMember(ctx context.Context, m port.BoardMember) error {
	// 既存なら何もしない。黙って上書きすると「招待した」と「ロールを変えた」を
	// 呼び出し側が区別できなくなる。件数で見分けて ErrAlreadyExists を返す。
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO board_members (board_id, user_id, role, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (board_id, user_id) DO NOTHING`,
		m.BoardID, m.UserID, string(m.Role), formatTime(m.CreatedAt))
	if err != nil {
		return fmt.Errorf("add member to board %s: %w", m.BoardID, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for add member: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("add member to board %s: %w", m.BoardID, port.ErrAlreadyExists)
	}

	return nil
}

// UpdateMemberRole はメンバーのロールを変える。
func (r *BoardRepository) UpdateMemberRole(
	ctx context.Context, boardID, userID string, role port.BoardRole,
) error {
	return r.exec(ctx, "member "+userID+" of board "+boardID,
		`UPDATE board_members SET role = ? WHERE board_id = ? AND user_id = ?`,
		string(role), boardID, userID)
}

// RemoveMember はメンバーを外す。
func (r *BoardRepository) RemoveMember(ctx context.Context, boardID, userID string) error {
	return r.exec(ctx, "member "+userID+" of board "+boardID,
		`DELETE FROM board_members WHERE board_id = ? AND user_id = ?`,
		boardID, userID)
}

// exec は 1 行更新を実行し、対象が無ければ ErrNotFound にする。
//
// UPDATE と DELETE は対象が無くてもエラーにならない。存在しない行への更新が
// 黙って成功すると、呼び出し側は保存できたと思い込む。
func (r *BoardRepository) exec(ctx context.Context, what, query string, args ...any) error {
	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update %s: %w", what, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for %s: %w", what, err)
	}
	if n == 0 {
		return fmt.Errorf("update %s: %w", what, port.ErrNotFound)
	}

	return nil
}

// Find は ID でボードを操作者のロールつきで引く。
func (r *BoardRepository) Find(
	ctx context.Context, actor, id string,
) (*port.BoardAccess, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+boardColumns+` `+memberJoin+` WHERE b.id = ?`, actor, id)

	a, err := scanBoard(row)
	if errors.Is(err, sql.ErrNoRows) {
		// 「無い」と「メンバーでない」を区別しない。区別すると、ID を総当たり
		// して他人のボードの存在を確かめられる（ADR 0016 / 0017）。
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find board %s: %w", id, err)
	}

	return &a, nil
}

// List は操作者がメンバーであるボードを更新時刻の降順で返す。
func (r *BoardRepository) List(ctx context.Context, actor string) ([]port.BoardAccess, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+summaryColumns+` `+memberJoin+` ORDER BY b.updated_at DESC, b.id`, actor)
	if err != nil {
		return nil, fmt.Errorf("list boards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var boards []port.BoardAccess
	for rows.Next() {
		a, err := scanSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan board: %w", err)
		}
		boards = append(boards, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate boards: %w", err)
	}

	return boards, nil
}

// rowScanner は *sql.Row と *sql.Rows の共通部分。
type rowScanner interface {
	Scan(dest ...any) error
}

// scanSummary は summaryColumns の並びを読む。
//
// extra は summaryColumns の後ろに足した列の受け皿。呼び出し側が SELECT に
// 足したぶんだけ渡す。列と受け皿の数が合わなければ Scan が落ちるので、
// 片方だけ足したまま素通りすることはない。
func scanSummary(s rowScanner, extra ...any) (port.BoardAccess, error) {
	var (
		a                    port.BoardAccess
		role                 string
		createdAt, updatedAt string
	)
	dest := append([]any{&a.Board.ID, &a.Board.Name,
		&a.Board.Target.RepositoryOwner, &a.Board.Target.RepositoryName,
		&a.Board.Target.ProjectID,
		&a.Board.Target.ProjectNumber, &a.Board.Target.ProjectTitle,
		&a.Board.Target.ProjectURL,
		&createdAt, &updatedAt, &role}, extra...)
	if err := s.Scan(dest...); err != nil {
		return port.BoardAccess{}, err
	}

	a.Role = port.BoardRole(role)

	var err error
	if a.Board.CreatedAt, err = parseTime(createdAt); err != nil {
		return port.BoardAccess{}, err
	}
	if a.Board.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return port.BoardAccess{}, err
	}

	return a, nil
}

// scanBoard は boardColumns の並びを読む。シーンを含む。
func scanBoard(s rowScanner) (port.BoardAccess, error) {
	// port.BoardAccess は scanSummary が作って返すので、その中の Scene を
	// 直接 Scan の受け皿にはできない。いったん受けてから詰める。
	var scene string

	a, err := scanSummary(s, &scene)
	if err != nil {
		return port.BoardAccess{}, err
	}
	a.Board.Scene = scene

	return a, nil
}
