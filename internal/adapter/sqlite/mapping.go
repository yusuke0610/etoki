package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/yusuke0610/etoki/port"
)

// MappingRepository は port.MappingRepository の SQLite 実装。
type MappingRepository struct {
	db *sql.DB
}

// NewMappingRepository は MappingRepository を作る。
func NewMappingRepository(db *sql.DB) *MappingRepository {
	return &MappingRepository{db: db}
}

var _ port.MappingRepository = (*MappingRepository)(nil)

// SaveRun は run とその Items を 1 トランザクションで保存する。
//
// 途中で失敗したら run ごと保存しない。部分的に書き込まれた run が残ると、
// 3 状態判定が「作成済み」と誤答してしまうため。
func (r *MappingRepository) SaveRun(ctx context.Context, run port.SyncRun) (int64, error) {
	// DB に触る前に弾けるものは弾く。CHECK 制約でも防げるが、エラーメッセージが
	// 「どの item が不正か」を示せる分こちらが役に立つ。
	for _, it := range run.Items {
		if !it.Kind.Valid() {
			return 0, fmt.Errorf("invalid kind %q for local_id %q", it.Kind, it.LocalID)
		}
		if !it.Action.Valid() {
			return 0, fmt.Errorf("invalid action %q for local_id %q", it.Action, it.LocalID)
		}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin save run: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO sync_runs (board_id, annotation_element_id, content_hash, created_at)
		 VALUES (?, ?, ?, ?)`,
		run.BoardID, run.AnnotationID, run.ContentHash, formatTime(run.CreatedAt),
	)
	if err != nil {
		return 0, fmt.Errorf("insert sync_run: %w", err)
	}

	runID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id for sync_run: %w", err)
	}

	for _, it := range run.Items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sync_items
			   (run_id, item_id, kind, title, body, local_id, parent_local_id,
			    action, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			runID, it.ItemID, string(it.Kind), it.Title, it.Body,
			it.LocalID, it.ParentLocalID, string(it.Action), formatTime(it.CreatedAt),
		); err != nil {
			return 0, fmt.Errorf("insert sync_item %q: %w", it.LocalID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit save run: %w", err)
	}

	return runID, nil
}

// FindLatestRun は注釈の最新の run を Items 込みで返す。
func (r *MappingRepository) FindLatestRun(ctx context.Context, boardID, annotationID string) (*port.SyncRun, error) {
	// 最新は created_at ではなく id で決める。時刻は呼び出し側が与えるため
	// 同一時刻の run がありえて、created_at だけでは順序が定まらない。
	row := r.db.QueryRowContext(ctx,
		`SELECT id, board_id, annotation_element_id, content_hash, created_at
		   FROM sync_runs
		  WHERE board_id = ? AND annotation_element_id = ?
		  ORDER BY id DESC
		  LIMIT 1`,
		boardID, annotationID,
	)

	run, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find latest run for %s/%s: %w", boardID, annotationID, err)
	}

	items, err := r.itemsByRunIDs(ctx, []int64{run.ID})
	if err != nil {
		return nil, err
	}
	run.Items = items[run.ID]

	return &run, nil
}

// ListLatestRunsByBoard はボード内の注釈ごとに最新の run を返す。
func (r *MappingRepository) ListLatestRunsByBoard(ctx context.Context, boardID string) ([]port.SyncRun, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT r.id, r.board_id, r.annotation_element_id, r.content_hash, r.created_at
		   FROM sync_runs AS r
		   JOIN (
		          SELECT annotation_element_id, MAX(id) AS max_id
		            FROM sync_runs
		           WHERE board_id = ?
		           GROUP BY annotation_element_id
		        ) AS latest
		     ON r.id = latest.max_id
		  ORDER BY r.annotation_element_id`,
		boardID,
	)
	if err != nil {
		return nil, fmt.Errorf("list latest runs for board %s: %w", boardID, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		runs []port.SyncRun
		ids  []int64
	)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan sync_run: %w", err)
		}
		runs = append(runs, run)
		ids = append(ids, run.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync_runs: %w", err)
	}

	// items は run ごとに引かず 1 クエリでまとめて取る。注釈数だけクエリが
	// 増えるのを避けるため。
	items, err := r.itemsByRunIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range runs {
		runs[i].Items = items[runs[i].ID]
	}

	return runs, nil
}

// itemsByRunIDs は複数の run の items をまとめて引き、run ID ごとに束ねて返す。
func (r *MappingRepository) itemsByRunIDs(ctx context.Context, runIDs []int64) (map[int64][]port.SyncItem, error) {
	result := make(map[int64][]port.SyncItem, len(runIDs))
	if len(runIDs) == 0 {
		return result, nil
	}

	// IN 句のプレースホルダは件数分を組み立てる必要がある。値そのものは
	// 引数として渡すので、SQL 文字列に埋め込むのは "?" だけ。
	placeholders := strings.Repeat(",?", len(runIDs))[1:]
	args := make([]any, len(runIDs))
	for i, id := range runIDs {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, run_id, item_id, kind, title, body, local_id, parent_local_id,
		        action, created_at
		   FROM sync_items
		  WHERE run_id IN (`+placeholders+`)
		  ORDER BY id`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("select sync_items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			it        port.SyncItem
			kind      string
			action    string
			createdAt string
		)
		if err := rows.Scan(
			&it.ID, &it.RunID, &it.ItemID, &kind, &it.Title, &it.Body,
			&it.LocalID, &it.ParentLocalID, &action, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan sync_item: %w", err)
		}

		it.Kind = port.ItemKind(kind)
		it.Action = port.SyncAction(action)
		if it.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}

		result[it.RunID] = append(result[it.RunID], it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync_items: %w", err)
	}

	return result, nil
}

// ListItemsByAnnotation はその注釈が GitHub に在らしめているものを返す。
//
// **run 履歴を item_id で畳む（ADR 0026）。** 最新 run の items ではない。
// 更新は同じ item_id に吸収され、今回触らなかった item も残り続ける。
//
// 採るのは item ごとの `MAX(sync_items.id)`、並べるのは `MIN` の順。created_at は
// 呼び出し側が与えるので同一時刻がありえて順序が定まらない（最新 run を id で
// 決めているのと同じ理由）。
//
// **並びは「最初に作られた順」で固定する。** 最新の記録の順に並べると、1 件
// 更新しただけでその item が一覧の末尾へ動く。中身を書き換えただけで並びが
// 変わると、開発者は同じものを見ていることを確かめ直すことになる。
func (r *MappingRepository) ListItemsByAnnotation(
	ctx context.Context, boardID, annotationID string,
) ([]port.SyncItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT i.id, i.run_id, i.item_id, i.kind, i.title, i.body,
		        i.local_id, i.parent_local_id, i.action, i.created_at
		   FROM sync_items i
		   JOIN (
		     SELECT i2.item_id,
		            MIN(i2.id) AS first_id,
		            MAX(i2.id) AS last_id
		       FROM sync_items i2
		       JOIN sync_runs r2 ON r2.id = i2.run_id
		      WHERE r2.board_id = ? AND r2.annotation_element_id = ?
		      GROUP BY i2.item_id
		   ) folded ON folded.last_id = i.id
		  ORDER BY folded.first_id`,
		boardID, annotationID,
	)
	if err != nil {
		return nil, fmt.Errorf("select sync_items by annotation: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// nil ではなく空スライスを返す。「一度も実行していない」と「実行したが
	// 0 件だった」を呼び出し側が区別する必要はなく、どちらも在るものが無い。
	items := []port.SyncItem{}

	for rows.Next() {
		var (
			it        port.SyncItem
			kind      string
			action    string
			createdAt string
		)
		if err := rows.Scan(
			&it.ID, &it.RunID, &it.ItemID, &kind, &it.Title, &it.Body,
			&it.LocalID, &it.ParentLocalID, &action, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan sync_item: %w", err)
		}

		it.Kind = port.ItemKind(kind)
		it.Action = port.SyncAction(action)
		if it.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}

		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync_items: %w", err)
	}

	return items, nil
}

func scanRun(s rowScanner) (port.SyncRun, error) {
	var (
		run       port.SyncRun
		createdAt string
	)
	if err := s.Scan(
		&run.ID, &run.BoardID, &run.AnnotationID, &run.ContentHash, &createdAt,
	); err != nil {
		return port.SyncRun{}, err
	}

	var err error
	if run.CreatedAt, err = parseTime(createdAt); err != nil {
		return port.SyncRun{}, err
	}

	return run, nil
}
