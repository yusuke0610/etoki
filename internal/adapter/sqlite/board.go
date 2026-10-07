package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
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
// 読んでも捨てるだけになる。捨てるために全ボードぶんをメモリに載せることに
// なる。
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

// UpdateScene はシーンと画像と更新時刻を更新し、書いたあとにボードが持って
// いる画像の ID を返す。
//
// base がいまの版と違えば何も書かず port.ErrConflict を返す（ADR 0020）。
//
// **シーンと画像は 1 トランザクションで書く**（ADR 0074）。照合に負けたら
// 画像も足さず消さない。分けると、照合に負けた保存の画像だけが残ったり、
// シーンが指す画像が消えたりする。
func (r *BoardRepository) UpdateScene(
	ctx context.Context, actor, id string, w port.SceneWrite, base, updatedAt time.Time,
) ([]string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update board %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()

	// メンバーであることを WHERE に入れる。ここは Find を通らずに直接 UPDATE
	// する経路なので、絞り忘れると他人のボードを書き換えられる（ADR 0016）。
	//
	// **ロールは見ない。** editor 以上かどうかはユースケース層が
	// BoardRole.AtLeast で判断する。ここに書くと判定が 2 箇所になる（ADR 0017）。
	//
	// 版の照合も同じ 1 文に置く。先に SELECT して比べる形にすると、比べてから
	// 書くまでの隙間に入った保存を上書きする。照合したい相手はその隙間に
	// 現れるので、隙間を作った時点で守れない。
	//
	// **画像はこの文の後に書く。** 照合に通った保存だけが画像に触れる。
	res, err := tx.ExecContext(ctx,
		`UPDATE boards SET scene = ?, updated_at = ?
		 WHERE id = ? AND updated_at = ? AND `+memberExists,
		w.Scene, formatTime(updatedAt), id, formatTime(base), actor)
	if err != nil {
		return nil, fmt.Errorf("update board %s: %w", id, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected for board %s: %w", id, err)
	}
	if n == 0 {
		// 何も書いていないので、ここで閉じてから理由を引き直す。
		_ = tx.Rollback()
		return nil, r.updateMissed(ctx, actor, id)
	}

	for _, f := range w.Added {
		// 同じ ID がすでにあれば置き換える。保存はその時点のボードの姿を
		// 決めるもので、送られてきたものが正しい（ADR 0074）。
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO board_files (board_id, id, bytes, data) VALUES (?, ?, ?, ?)
			 ON CONFLICT (board_id, id) DO UPDATE
			 SET bytes = excluded.bytes, data = excluded.data`,
			id, f.ID, len(f.Data), f.Data,
		); err != nil {
			return nil, fmt.Errorf("put file %s of board %s: %w", f.ID, id, err)
		}
	}

	// シーンから外れた画像を消す。ID の集合は JSON の配列 1 つで渡す。
	// プレースホルダを ID の数だけ並べると、画像の多いボードで変数の上限に
	// 当たりうる。
	referenced, err := json.Marshal(nonNil(w.Referenced))
	if err != nil {
		return nil, fmt.Errorf("encode referenced files of board %s: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM board_files
		 WHERE board_id = ? AND id NOT IN (SELECT value FROM json_each(?))`,
		id, string(referenced),
	); err != nil {
		return nil, fmt.Errorf("delete unreferenced files of board %s: %w", id, err)
	}

	// 書いたあとに持っている画像を、同じトランザクションの中で読む。
	// 呼び出し側はこれを次の保存の基準にする（何を送らなくてよいか）。
	held, err := fileIDs(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update board %s: %w", id, err)
	}
	return held, nil
}

// updateMissed は照合つきの UPDATE が 0 行だった理由を返す。
//
// 0 行の理由は 2 つある。触れないボードなのか、版が古いのか。UPDATE は
// どちらかを返さないので、触れるかどうかだけを引き直して分ける。この 2 文の
// 間にボードが消される（Delete、ADR 0042）ことはありうるが、そのときは
// 「触れなくなった」が正しい答えなので、引き直しの結果をそのまま採ってよい。
func (r *BoardRepository) updateMissed(ctx context.Context, actor, id string) error {
	ok, err := r.readable(ctx, actor, id)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("update board %s: %w", id, port.ErrConflict)
	}
	return fmt.Errorf("update board %s: %w", id, port.ErrNotFound)
}

// nonNil は nil のスライスを空のスライスにする。JSON で null ではなく [] に
// するため。json_each(null) は何も返さないので、NOT IN が空集合と比べる
// 形にならない。
func nonNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

// queryer は *sql.DB と *sql.Tx の共通部分。
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// fileIDs はボードが持っている画像の ID を昇順で返す。
func fileIDs(ctx context.Context, q queryer, boardID string) ([]string, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id FROM board_files WHERE board_id = ? ORDER BY id`, boardID)
	if err != nil {
		return nil, fmt.Errorf("list file ids of board %s: %w", boardID, err)
	}
	defer func() { _ = rows.Close() }()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan file id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file ids: %w", err)
	}
	return ids, nil
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

// Delete はボードを消す。
//
// **メンバーであることを WHERE に入れる。** Find を通らずに直接 DELETE する
// 経路なので、絞り忘れると他人のボードを消せる（ADR 0016）。**ロールは見ない。**
// owner だけかはユースケース層が判断する（ADR 0017）。
//
// board_members / sync_runs / sync_items は外部キーの ON DELETE CASCADE で
// 一緒に消える。**ここで消す順を書かない。** 書くと消える範囲の定義がスキーマと
// Go の 2 箇所になり、テーブルを足したときに片方だけ古くなる。
func (r *BoardRepository) Delete(ctx context.Context, actor, id string) error {
	return r.exec(ctx, "board "+id,
		`DELETE FROM boards WHERE id = ? AND `+memberExists,
		id, actor)
}

// UpdateName は名前だけを更新する。
//
// **updated_at は SET に書かない。** あれはシーンの版であり保存の照合基準
// （ADR 0020）。名前を直しただけで進めると、開いている別のメンバーの次の保存が
// 理由なく 409 になる。
func (r *BoardRepository) UpdateName(ctx context.Context, actor, id, name string) error {
	return r.exec(ctx, "board "+id,
		`UPDATE boards SET name = ? WHERE id = ? AND `+memberExists,
		name, id, actor)
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

// UpdateTargetDisplay は作成先の表示用スナップショットと更新時刻だけを更新する。
//
// **作成先の 3 列は SET に書かない。** 固定後に通る唯一の経路なので、ここが
// 書けるようになると固定が意味を失う（ADR 0037）。
func (r *BoardRepository) UpdateTargetDisplay(
	ctx context.Context, actor, id string, d port.BoardTargetDisplay, updatedAt time.Time,
) error {
	return r.exec(ctx, "board "+id,
		`UPDATE boards
		 SET project_number = ?, project_title = ?, project_url = ?, updated_at = ?
		 WHERE id = ? AND `+memberExists,
		d.ProjectNumber, d.ProjectTitle, d.ProjectURL, formatTime(updatedAt), id, actor)
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

// FindWithFiles は Find に加えて、そのボードの画像をすべて返す。
//
// **シーンと画像は同じトランザクションで読む**（ADR 0074）。WAL では
// トランザクションの中の読み取りは同じ時点を見るので、間に保存が入っても
// シーンが指す画像が欠けない。
func (r *BoardRepository) FindWithFiles(
	ctx context.Context, actor, id string,
) (*port.BoardAccess, []port.BoardFile, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin find board %s: %w", id, err)
	}
	// 読むだけなので、閉じ方は Rollback で足りる。
	defer func() { _ = tx.Rollback() }()

	a, err := scanBoard(tx.QueryRowContext(ctx,
		`SELECT `+boardColumns+` `+memberJoin+` WHERE b.id = ?`, actor, id))
	if errors.Is(err, sql.ErrNoRows) {
		// Find と同じく「無い」と「メンバーでない」を区別しない。
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("find board %s: %w", id, err)
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id, data FROM board_files WHERE board_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("list files of board %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()

	files := []port.BoardFile{}
	for rows.Next() {
		var f port.BoardFile
		if err := rows.Scan(&f.ID, &f.Data); err != nil {
			return nil, nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate files: %w", err)
	}

	return &a, files, nil
}

// FileSizes はボードが持っている画像の ID ごとのバイト数を返す。
//
// **data の列は読まない。** bytes は data より前の列に置いてあるので、画像の
// 中身が載っているページに触らずに済む（マイグレーション 0015）。
func (r *BoardRepository) FileSizes(
	ctx context.Context, boardID string,
) (map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, bytes FROM board_files WHERE board_id = ?`, boardID)
	if err != nil {
		return nil, fmt.Errorf("list file sizes of board %s: %w", boardID, err)
	}
	defer func() { _ = rows.Close() }()

	sizes := map[string]int64{}
	for rows.Next() {
		var (
			id    string
			bytes int64
		)
		if err := rows.Scan(&id, &bytes); err != nil {
			return nil, fmt.Errorf("scan file size: %w", err)
		}
		sizes[id] = bytes
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file sizes: %w", err)
	}
	return sizes, nil
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
