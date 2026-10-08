-- キャンバスに貼った画像をシーンとは別に持つ（ADR 0074、#102）。
--
-- これまで画像は boards.scene の `files` に base64 で乗っていた。保存はシーン
-- 全体を書くので、図形を 1 つ動かしただけでも画像を丸ごと書き直し、3 状態の
-- 判定や一覧の件数のようにテキストしか使わない読み出しでも画像まで読んでいた。
--
-- id は Excalidraw が振った画像の ID で、ボードの中で一意。data は Excalidraw の
-- 画像データ（BinaryFileData）を JSON のまま持ち、etoki は中身を解釈しない。
--
-- bytes は data のバイト数。上限（usecase.MaxBoardFileBytes）を確かめるために
-- 大きさだけを読む口があり（FileSizes）、**data より前の列に置く**。SQLite は
-- 行を列の順に並べ、溢れたぶんを別のページへ逃がすので、前にある列だけを読めば
-- 画像の中身のページには触らない。**data と食い違わないよう CHECK で縛る。**
-- 食い違うと、上限の判定が実際の大きさと別のものを見る。
CREATE TABLE board_files (
  board_id TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
  id       TEXT NOT NULL CHECK (id <> ''),
  bytes    INTEGER NOT NULL,
  data     TEXT NOT NULL,

  PRIMARY KEY (board_id, id),
  CHECK (bytes = length(CAST(data AS BLOB)))
);

-- 既存のシーンから画像を移す。
--
-- 移すのは `files` がオブジェクトで、その値もオブジェクトのものだけ。Excalidraw が
-- 書く画像は必ずそうで、それ以外の形は画面に出せる画像ではない（`files` が配列
-- なら key が整数になるので、key の型で落ちる）。
--
-- 読めないシーンは保存時に弾いているので無いはずだが、**JSON の関数は読めない
-- 入力で移行ごと止まる。** WHERE で絞っても関数が先に呼ばれうるので、CASE で
-- 空のオブジェクトに差し替えてから渡す。
INSERT INTO board_files (board_id, id, bytes, data)
SELECT b.id, f.key, length(CAST(f.value AS BLOB)), f.value
  FROM boards b,
       json_each(CASE WHEN json_valid(b.scene) THEN b.scene ELSE '{}' END, '$.files') f
 WHERE typeof(f.key) = 'text'
   AND f.key <> ''
   AND f.type = 'object';

-- シーンから `files` を抜く。**updated_at は進めない。** 版は「何番目の保存か」
-- （ADR 0020）で、移行は保存ではない。進めると、開いていた画面の次の保存が
-- 理由なく 409 になる。
--
-- 3 状態の判定は変わらない。content_hash の入力はテキストと注釈のメタデータで、
-- 画像の実体は入っていない（TestAnnotationHash_IgnoresFiles）。
UPDATE boards
   SET scene = json_remove(scene, '$.files')
 WHERE CASE WHEN json_valid(scene) THEN json_type(scene, '$.files') IS NOT NULL
            ELSE 0 END;
