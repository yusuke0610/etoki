# 0049: 親テーブルの作り直しは外部キーを切って適用する（印は SQL の先頭行）

ステータス: 採用

## 文脈

`sync_runs` の CHECK 制約が、結末と理由の食い違いを片側しか禁じていなかった
（`outcome = 'incomplete'` なのに理由が無い行が入る、#130）。`SaveRun` は同じ
食い違いを弾いているが、復元や保守 SQL はそこを通らない。行として意味が決まらない
組み合わせは DB でも作らせない、というのが ADR 0043 の決定で、そこが守れて
いなかった。

**SQL だけでは直せなかった。** SQLite は CHECK 制約を後から変えられないので、
新しい表を作って `INSERT ... SELECT` し、旧表を `DROP` して `RENAME` する手順が
要る。ところがこの手順は、ADR 0003 で決めた自前 runner と噛み合わない。

- `applyMigration` は 1 ファイルの SQL 全体を**常に 1 つのトランザクション**で
  ラップする。
- 接続は `foreign_keys=ON` で開いている（`internal/adapter/sqlite/db.go` の DSN）。
- `PRAGMA foreign_keys` は**トランザクションの中では no-op**。マイグレーション
  SQL の中に `PRAGMA foreign_keys=OFF` と書いても効かず、エラーにもならない。
- `foreign_keys=ON` のまま親テーブルを `DROP TABLE` すると、SQLite は暗黙の
  DELETE を発行し、子テーブルの `ON DELETE CASCADE` が発火する。

`sync_runs` は `sync_items` から `ON DELETE CASCADE` 付きで参照される親テーブル
なので、この手順をそのまま書くと**全ボードの実行履歴（ADR 0007）と、作成済み
draft issue の追跡情報（ADR 0026）が静かに消える。** 作成は取り消せない操作
（ADR 0009）なので、消えたぶんは GitHub 側に残ったまま二度と辿れない。

## 決定

**外部キーの切り替えだけをトランザクションの外に出す経路を runner に足し、
それを使うマイグレーションを SQL 側の印で選ぶ。**

### 印は SQL ファイルの先頭行に置く

`-- etoki:rebuild-table` を先頭行に単独で書いたファイルだけが、この経路に入る。

Go 側にファイル名の一覧を持たせない。持たせると「なぜ特別扱いなのか」が
マイグレーション本体と離れ、SQL を読んでも分からなくなる。**印がある側だけを
特別扱いにして、既定は安全な側（従来どおり 1 トランザクション）に置く。**

前方一致では見ない。`-- etoki:rebuild-table-later` のような別物の行が黙って
外部キーを切る経路に入るため、先頭行が印そのものであることを要求する。

### 外へ出すのは PRAGMA の 2 行だけ

作り直しの SQL 本体と `schema_migrations` への記録は、これまでどおり 1 つの
トランザクションの中で行う。**片方だけが残る状態を作らないという ADR 0003 の
性質は動かさない。** 外に出るのは切る／戻すの 2 行に限る。

### 接続を 1 本に固定する

`PRAGMA` は接続ごとに効き、`database/sql` はプールから接続を配る。
`db.ExecContext` で切っても、続くトランザクションが別の接続に載れば外部キーは
ON のままで、**親表の DROP が子表を消す。** `db.Conn` で固定してから切る。

### 効いたことを読み返して確かめる

切ったつもりで切れていないことが、いちばん高くつく失敗（黙って履歴が消える）に
直結する。`PRAGMA foreign_keys` を読み返し、期待どおりでなければ何もせずに
止める。戻すときも同じ。**切ったままプールへ帰した接続は、以後の
`ON DELETE CASCADE` を黙って効かなくする。**

### 戻しはキャンセルの対象外にし、戻せなければ接続ごと捨てる

**戻しに呼び出し元の `ctx` をそのまま使わない。** 切ったあとにキャンセルされる
と、`database/sql` は以後の実行を `context canceled` で断るので、戻しまで道連れ
になる。期限だけを切った `context.WithoutCancel` で戻す。

それでも戻せなかったら、**その接続をプールへ帰さない。** キャンセルやタイムアウト
は接続を壊さないので、`Conn.Close()` はそれを物理接続としてプールへ戻す。
`database/sql` の `ResetSession` は `PRAGMA` を戻さないので、次にその接続を掴んだ
書き込みは `foreign_keys=OFF` のまま走り、**`ON DELETE CASCADE` が黙って効かなく
なる。** 「戻せたことを確かめられなかった接続は使わせない」をこちら側で決め、
`Conn.Raw` に `driver.ErrBadConn` を返させて捨てる。配り直されるのは DSN の
`foreign_keys=ON` が効いた新しい接続になる。**ドライバが接続をどう扱うかに
依らない。**

**適用の失敗と戻しの失敗は両方返す**（`errors.Join`）。戻せなかったことは接続の
状態についての情報で、適用がなぜ失敗したかとは別の話。片方に畳むと、DB を直す
側がどちらを見ればよいか分からなくなる。

### トリガーで代用しない

`BEFORE INSERT` / `BEFORE UPDATE` のトリガーで `RAISE(ABORT)` すれば、テーブルを
作り直さずに同じ組み合わせを弾ける。runner にも触らずに済む。**それでも採らない。**
不変条件が `error` 列に残った 0010 の CHECK とトリガー 2 本に分かれ、**同じ規則が
3 箇所**になる。片方だけ変わる形を避けるために CHECK へ全象限を書く、という判断
（`.claude/rules/validation-boundaries.md`）と正面から食い違う。

作り直しの手当てが要るのはこの 1 回ではない。親テーブルの制約を直す必要は今後も
出るので、**回避策をその都度足すより、runner に 1 度だけ穴を開けるほうが安い。**

### COMMIT の前に `PRAGMA foreign_key_check`

外部キーを切っている区間では、参照が壊れても書き込みの側はエラーにならない。
COMMIT の前に検査し、1 行でも返ったら何も書かずに止める。

## 結果

- `sync_runs` の CHECK を、4 象限すべてを明示的に決める形に直せた
  （`migrations/0011_sync_run_outcome_check.sql`）。
- 以後、子テーブルから `ON DELETE CASCADE` で参照される表（`sync_runs` など）を
  作り直すマイグレーションを、履歴を失わずに書ける。
- **ロールバックは依然として実装しない**（ADR 0003）。前進のみという性質は
  変わらない。
- **この経路を使うマイグレーションは、子テーブルの行が残ることをテストで
  固定する。** `TestMigrate_RebuildKeepsChildRows` は、分岐を外すと
  `sync_items` が 0 件になって落ちる。仕組みの正しさを型では表せないので、
  そこは検知できるテストで持つ。
- **接続の後始末も同じくテストで持つ。**
  `TestRestoreForeignKeys_SurvivesCanceledContext` は戻しを `ctx` のままに
  戻すと `context canceled` で落ち、
  `TestDiscardConn_DoesNotReturnConnectionToPool` は捨てるのをやめると、
  次に配られた接続の `foreign_keys` が 0 のままで落ちる。
