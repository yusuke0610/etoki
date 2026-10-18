// Package sqlite は SQLite による永続化アダプタを提供する。
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// modernc.org/sqlite は純 Go 実装のドライバで CGO を必要としない。
	// ドライバ名は "sqlite"（mattn/go-sqlite3 の "sqlite3" ではない）。
	_ "modernc.org/sqlite"
)

// Open は SQLite データベースを開く。path のディレクトリは呼び出し側が用意する。
func Open(ctx context.Context, path string) (*sql.DB, error) {
	// PRAGMA は接続ごとに効くため DSN で指定する。database/sql は接続を
	// プールして張り直すので、開いた直後に一度実行するだけでは足りない。
	//
	//   foreign_keys  SQLite は既定で外部キーを検査しない。ON にしないと
	//                 ON DELETE CASCADE が働かない。
	//   busy_timeout  書き込み競合時に即エラーを返さず待つ。
	//   journal_mode  WAL は読み書きの同時実行に強い。
	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
		path,
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite %s: %w", path, err)
	}

	return db, nil
}

// formatTime は時刻を保存用の文字列にする。
//
// time.Time をドライバに直接渡すと、保存形式とタイムゾーンの扱いが
// ドライバの実装依存になる。UTC の RFC3339Nano に固定して往復させる。
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// fixedTimeLayout は小数 9 桁で固定した RFC 3339。
const fixedTimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// formatFixedTime は時刻を桁数の揃った文字列にする。
//
// **SQL で文字列の大小を比べる列（期限）は、書き込みも比べる値もこれで作る。**
// formatTime の RFC3339Nano は小数部の末尾の 0 を落とすので、".5Z" が ".45Z"
// より前に、"00Z" が "00.3Z" より後に並び、時刻の順と食い違う（ADR 0076、#274）。
// 読むのは parseTime のままでよい（RFC3339Nano の解析は桁数を問わない）。
//
// sessions と oauth_states の期限は、以前は formatTime で書いていた。その行は
// マイグレーション 0017 が固定幅に揃える。
//
// **formatTime をこれに置き換えない。** boards.updated_at は版の照合を等値で
// 行う（ADR 0020）ので、書式を変えると既存の行と一致しなくなる。
func formatFixedTime(t time.Time) string {
	return t.UTC().Format(fixedTimeLayout)
}

// parseTime は formatTime と formatFixedTime が書いた文字列を時刻に戻す。
func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
	}
	return t, nil
}
