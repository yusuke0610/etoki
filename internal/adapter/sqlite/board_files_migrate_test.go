package sqlite_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/domain"
)

// ---------------------------------------------------------------------------
// 貼った画像をシーンの外へ移す移行（ADR 0074、#102）
// ---------------------------------------------------------------------------

// boardFilesMigration は画像を board_files へ移すマイグレーション。
// 確かめるテストは、直前（これを含まない）の状態から始める。
const boardFilesMigration = "0015_board_files.sql"

// sceneWithImages は注釈の中に画像を 1 枚置き、画像の実体を 2 枚持つシーン。
//
// 画像の実体はキーの順と空白を Excalidraw の直列化（インデント 2）に寄せてある。
// 移行は JSON の関数で抜き出すので、書かれた形のまま移るとは限らない。比べる
// ときは中身で比べる。
const sceneWithImages = `{
  "type": "excalidraw",
  "version": 2,
  "elements": [
    {"id":"annot-1","type":"frame","name":"決済まわり",
     "customData":{"etoki":{"granularity":"epic","kind":"sequence"}}},
    {"id":"t1","type":"text","text":"Stripe の SDK が古い","frameId":"annot-1"},
    {"id":"img-1","type":"image","fileId":"file-1","frameId":"annot-1"},
    {"id":"img-2","type":"image","fileId":"file-2"}
  ],
  "appState": {"viewBackgroundColor": "#ffffff"},
  "files": {
    "file-1": {
      "mimeType": "image/png",
      "id": "file-1",
      "dataURL": "data:image/png;base64,iVBORw0KGgo=",
      "created": 1700000000000
    },
    "file-2": {
      "mimeType": "image/jpeg",
      "id": "file-2",
      "dataURL": "data:image/jpeg;base64,/9j/4AAQ",
      "created": 1700000000001,
      "lastRetrieved": 1700000000002
    }
  }
}`

// migratedDB は画像を移す直前まで適用した DB を作り、seed を流してから残りを
// 適用する。
func migratedDB(t *testing.T, seed func(db *sql.DB)) *sql.DB {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrateUpTo(t, db, boardFilesMigration)
	seed(db)

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

// insertBoard は移行前の形でボードを 1 枚入れる。リポジトリは移行後の形を
// 前提にするので、ここは SQL で直接入れる。
func insertBoard(t *testing.T, db *sql.DB, id, scene string) {
	t.Helper()

	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO boards (id, name, scene, created_at, updated_at)
		 VALUES (?, 'テストボード', ?, '2026-01-01T00:00:00Z', '2026-01-02T03:04:05.123456789Z')`,
		id, scene,
	); err != nil {
		t.Fatalf("insert board %s: %v", id, err)
	}
}

// sceneOf は保存されているシーンをそのまま返す。
func sceneOf(t *testing.T, db *sql.DB, id string) string {
	t.Helper()

	var scene string
	if err := db.QueryRowContext(t.Context(),
		`SELECT scene FROM boards WHERE id = ?`, id).Scan(&scene); err != nil {
		t.Fatalf("select scene of %s: %v", id, err)
	}
	return scene
}

// jsonValue は JSON を Go の値に読む。キーの順と空白に依らずに比べるため。
func jsonValue(t *testing.T, raw string) any {
	t.Helper()

	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	return v
}

// 画像の実体がシーンから board_files へ移り、シーンには画像以外がそのまま残る。
//
// **一番失いたくないのは画像そのもの。** 移した先の中身を、元のシーンにあった
// 実体と 1 枚ずつ突き合わせる。lastRetrieved のような任意のフィールドも落とさない
// （etoki は中身を解釈しない）。
func TestMigrate_MovesSceneFilesToBoardFiles(t *testing.T) {
	t.Parallel()

	db := migratedDB(t, func(db *sql.DB) {
		insertBoard(t, db, "board-1", sceneWithImages)
	})

	original := jsonValue(t, sceneWithImages).(map[string]any)
	originalFiles := original["files"].(map[string]any)

	rows, err := db.QueryContext(t.Context(),
		`SELECT id, bytes, data FROM board_files WHERE board_id = 'board-1' ORDER BY id`)
	if err != nil {
		t.Fatalf("select board_files: %v", err)
	}
	defer func() { _ = rows.Close() }()

	moved := map[string]any{}
	for rows.Next() {
		var (
			id, data string
			bytes    int64
		)
		if err := rows.Scan(&id, &bytes, &data); err != nil {
			t.Fatalf("scan board_files: %v", err)
		}
		if bytes != int64(len(data)) {
			t.Errorf("%s: bytes = %d, want %d（data の長さ）", id, bytes, len(data))
		}
		moved[id] = jsonValue(t, data)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate board_files: %v", err)
	}

	if !reflect.DeepEqual(moved, originalFiles) {
		t.Errorf("移した画像 = %v\nwant %v", moved, originalFiles)
	}

	// シーンからは files だけが抜け、ほかのキーは中身ごと残る。
	migrated := jsonValue(t, sceneOf(t, db, "board-1")).(map[string]any)
	if _, ok := migrated["files"]; ok {
		t.Error("移行後のシーンに files が残っている")
	}
	delete(original, "files")
	if !reflect.DeepEqual(migrated, original) {
		t.Errorf("files 以外が変わった:\n got %v\nwant %v", migrated, original)
	}
}

// 移行で版（updated_at）を進めない。
//
// 版は「何番目の保存か」（ADR 0020）。移行は保存ではないので、進めると開いていた
// 画面の次の保存が理由なく 409 になる。比べるのは書いた文字列そのもの。
// 時刻として読み直して比べると、精度を落として書き直したことに気づけない。
func TestMigrate_BoardFilesKeepsBoardVersion(t *testing.T) {
	t.Parallel()

	db := migratedDB(t, func(db *sql.DB) {
		insertBoard(t, db, "board-1", sceneWithImages)
	})

	var updatedAt string
	if err := db.QueryRowContext(t.Context(),
		`SELECT updated_at FROM boards WHERE id = 'board-1'`).Scan(&updatedAt); err != nil {
		t.Fatalf("select updated_at: %v", err)
	}
	if want := "2026-01-02T03:04:05.123456789Z"; updatedAt != want {
		t.Errorf("updated_at = %q, want %q", updatedAt, want)
	}
}

// 移行の前後で、注釈の content_hash が変わらない。
//
// 変わると、移行しただけで作成済みの注釈が一斉に「変更あり」に落ちる。中身は
// 何も変わっていないのに、更新するかを開発者に選ばせることになる。
// ハッシュは sync_runs に控えた値と比べて 3 状態を決める（DecideState）ので、
// 移行の前に控えたはずの値で比べる。
func TestMigrate_BoardFilesKeepsAnnotationStates(t *testing.T) {
	t.Parallel()

	before, err := domain.ParseScene([]byte(sceneWithImages))
	if err != nil {
		t.Fatalf("ParseScene(before): %v", err)
	}
	annotations := before.Annotations()
	if len(annotations) != 1 {
		t.Fatalf("注釈 = %d 個, want 1（テストデータを読み違えている）", len(annotations))
	}
	recorded := before.AnnotationHash(annotations[0])

	db := migratedDB(t, func(db *sql.DB) {
		insertBoard(t, db, "board-1", sceneWithImages)
	})

	after, err := domain.ParseScene([]byte(sceneOf(t, db, "board-1")))
	if err != nil {
		t.Fatalf("ParseScene(after): %v", err)
	}
	current := after.AnnotationHash(after.Annotations()[0])

	if got := domain.DecideState(&recorded, current); got != domain.StateCreated {
		t.Errorf("移行後の状態 = %v, want %v（ハッシュ %s → %s）",
			got, domain.StateCreated, recorded, current)
	}
}

// 画像を持たないボードは、行も増えずシーンもそのまま残る。空の files だけは
// 抜く（移行後のシーンは files を持たない、という形に揃える）。
func TestMigrate_BoardFilesWithoutImages(t *testing.T) {
	t.Parallel()

	const (
		noFiles    = `{"type":"excalidraw","elements":[{"id":"r","type":"rectangle"}]}`
		emptyFiles = `{"type":"excalidraw","elements":[],"files":{}}`
	)

	db := migratedDB(t, func(db *sql.DB) {
		insertBoard(t, db, "no-files", noFiles)
		insertBoard(t, db, "empty-files", emptyFiles)
	})

	if n := countRows(t, db, `SELECT COUNT(*) FROM board_files`); n != 0 {
		t.Errorf("board_files = %d 件, want 0", n)
	}
	// files を持たないシーンは書き換えない。書き換えると、移行のたびに画像と
	// 関係の無いボードまで JSON の形が変わる。
	if got := sceneOf(t, db, "no-files"); got != noFiles {
		t.Errorf("files の無いシーンが書き換わった: %s", got)
	}
	if got := jsonValue(t, sceneOf(t, db, "empty-files")); !reflect.DeepEqual(got,
		jsonValue(t, `{"type":"excalidraw","elements":[]}`)) {
		t.Errorf("空の files が残っている: %v", got)
	}
}

// 読めないシーンがあっても移行は止まらない。そのボードには触らない。
//
// 保存時に検証しているので来ないはずだが、来たときに移行ごと止まると、
// ほかのボードの画像も移せず etoki が起動しなくなる。
func TestMigrate_BoardFilesSkipsUnreadableScene(t *testing.T) {
	t.Parallel()

	const broken = `{"elements":[`

	db := migratedDB(t, func(db *sql.DB) {
		insertBoard(t, db, "broken", broken)
		insertBoard(t, db, "board-1", sceneWithImages)
	})

	if got := sceneOf(t, db, "broken"); got != broken {
		t.Errorf("読めないシーンが書き換わった: %s", got)
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM board_files WHERE board_id = 'board-1'`); n != 2 {
		t.Errorf("board-1 の画像 = %d 件, want 2", n)
	}
}

// bytes は data と食い違えない。食い違うと、上限の判定が実際の大きさと
// 別のものを見る。移行やリポジトリを通らない書き込みも DB が弾く。
func TestBoardFiles_RejectsMismatchedBytes(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")

	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO board_files (board_id, id, bytes, data)
		 VALUES ('board-1', 'file-1', 1, '{"id":"file-1"}')`,
	); err == nil {
		t.Error("bytes が data の長さと違う行が入った")
	}
}
