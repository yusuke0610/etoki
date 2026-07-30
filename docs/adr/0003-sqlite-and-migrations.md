# 0003. SQLite は modernc.org/sqlite、マイグレーションは embed + 自前 runner

- ステータス: 採用
- 日付: 2026-07-29

## 文脈

DB は SQLite を採用し、CGO を不要にしたい。マイグレーションの適用手段
（`make migrate`）も必要になる。

goose や golang-migrate といった既存ツールを使う選択肢もあるが、

- CGO 不要にするには `modernc.org/sqlite` のドライバ登録を自前で行う必要があり、
  結局グルーコードは書くことになる。
- 単一ファイルの SQLite に対して、複数 DB エンジン対応やロールバックといった
  これらのツールの主要機能はほぼ使わない。

## 決定

- ドライバは `modernc.org/sqlite`（純 Go 実装、CGO 不要）を使う。
- マイグレーションは `//go:embed migrations/*.sql` で埋め込み、`schema_migrations`
  テーブルで適用済みバージョンを管理する最小の runner を自前で書く。
- 実行口は `etoki migrate` サブコマンドとし、`make migrate` はそれを呼ぶ。
- ロールバック（down マイグレーション）は実装しない。前進のみとする。

## 結果

- 依存が増えず、バイナリ 1 つで配布・実行できる。
- マイグレーションの適用順序と冪等性の責任を自前で持つ。ここは Phase 1 で
  テストを先に書いて担保する。
- Postgres 実装を外部リポジトリに委ねる方針のため、この runner は SQLite 専用で
  よい。汎用化しない。
