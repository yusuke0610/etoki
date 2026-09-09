-- etoki:rebuild-table
--
-- 結末（outcome）と理由（error）の食い違いを、DB でも 4 象限すべて弾く。
--
-- 0010 が置いた `CHECK (error IS NULL OR outcome IS 'incomplete')` は、
-- 裏返しの不整合――結末が 'incomplete' なのに理由が無い行――を通す。SaveRun は
-- アプリ層で弾いているが、復元や保守 SQL はそこを通らない。行として意味が
-- 決まらない組み合わせは DB でも作らせない（ADR 0043）。
--
-- **この 1 ファイルだけ外部キーを切って適用する**（先頭行の印、ADR 0046）。
-- SQLite は CHECK 制約を後から変えられないのでテーブルを作り直すしかないが、
-- sync_runs は sync_items から ON DELETE CASCADE 付きで参照される親テーブルで、
-- foreign_keys=ON のまま DROP すると暗黙の DELETE が CASCADE を発火させ、
-- 全ボードの実行履歴（ADR 0007）と作成済み draft issue の追跡情報が消える。

CREATE TABLE sync_runs_new (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  board_id              TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
  annotation_element_id TEXT NOT NULL,
  content_hash          TEXT NOT NULL,
  created_at            TEXT NOT NULL,
  outcome               TEXT CHECK (outcome IN ('complete', 'incomplete')),
  error                 TEXT,

  -- 理由の有無と結末を一致させる。**許す形と禁じる形の両方が 1 本の式で
  -- 決まる。** 象限は 4 つとも埋まっている:
  --
  --   outcome = 'complete'   / 理由なし → 許す
  --   outcome = 'incomplete' / 理由あり → 許す
  --   outcome = 'complete'   / 理由あり → 禁じる（0010 も禁じていた）
  --   outcome = 'incomplete' / 理由なし → 禁じる（0010 が通していた）
  --
  -- NULL（結末を記録していなかった頃の run）は「理由なし」の側に落ちて許され、
  -- 理由だけが入った行は禁じられる。移行前の行はそのまま読める（ADR 0043）。
  --
  -- 比較が `=` ではなく `IS` なのは 0010 と同じ理由。outcome が NULL のとき
  -- `outcome = 'incomplete'` は NULL に評価され、CHECK は偽でないので通る。
  -- 外側の `IS` も同じで、両辺が NULL になりうる比較を真偽に落とすために要る。
  CHECK ((error IS NOT NULL) IS (outcome IS 'incomplete'))
);

-- **移し替えは既存行を直さない。** 0010 が通していた「途中で失敗したのに理由が
-- 無い」行が実在したら、この INSERT が CHECK に弾かれてマイグレーションごと
-- 止まる。**それでよい。** 理由を捏造することも、結末を complete に倒すことも
-- できない（ADR 0043 が既存行を埋めなかったのと同じ理由）。止まったら、その行を
-- どうするかは開発者が決める（中核思想 3）。SaveRun 経由では作れない組み合わせ
-- なので、直接 SQL を書いた覚えが無ければ起きない。
INSERT INTO sync_runs_new (id, board_id, annotation_element_id,
                           content_hash, created_at, outcome, error)
  SELECT id, board_id, annotation_element_id,
         content_hash, created_at, outcome, error
    FROM sync_runs;

-- AUTOINCREMENT の水位を引き継ぐ。DROP TABLE は sqlite_sequence の行も消すので、
-- 何もしないと削除済みボードの run が使った id を新しい run が再利用する。
-- 「最新の run は created_at ではなく id で決める」（0001）が依っているのは
-- 単調増加なので、履歴のあいだで id を使い回さない。
--
-- 移行元が空なら sync_runs_new 側に行が無く、この UPDATE は何もしない。その
-- ときは水位も 0 でよい（残っている run が 1 つも無い）。
UPDATE sqlite_sequence
   SET seq = MAX(seq, COALESCE((SELECT seq FROM sqlite_sequence
                                 WHERE name = 'sync_runs'), 0))
 WHERE name = 'sync_runs_new';

DROP TABLE sync_runs;

ALTER TABLE sync_runs_new RENAME TO sync_runs;

-- DROP でインデックスも消えるので張り直す。定義は 0001 と同じ。
CREATE INDEX idx_sync_runs_latest
ON sync_runs (board_id, annotation_element_id, id DESC);
