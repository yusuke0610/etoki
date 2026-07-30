// Package migrations は SQLite のマイグレーション SQL を埋め込む。
//
// SQL をバイナリに同梱することで、実行時にファイルの配置を気にせず
// マイグレーションを適用できる。判断の経緯は
// docs/adr/0003-sqlite-and-migrations.md を参照。
package migrations

import "embed"

// FS はマイグレーション SQL を含むファイルシステム。
// ファイル名の昇順が適用順序になる。
//
//go:embed *.sql
var FS embed.FS
