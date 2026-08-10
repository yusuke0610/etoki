-- ボードの所有者（ADR 0016）。
--
-- 空文字は無効値ではなく「認証なしの所有者」1 人を表す。認証を設定していない
-- 構成では利用者 ID が常に空文字になり、全ボードがその 1 人のものになる。
-- これまでどおり全部が見えるのはそのため。
--
-- 認証を有効にすると利用者 ID が入るので、空文字のボード（＝有効化より前に
-- 作ったもの）は見えなくなる。`etoki claim <login>` で引き受ける。自動で
-- 寄せないのは、共有サーバーで先に入った人が全部を持っていく決まり方を
-- 説明できないため。
--
-- users への外部キーは張らない。利用者を消したときにボードを道連れにせず、
-- 所有者の無いボードとして残したいため。
ALTER TABLE boards ADD COLUMN owner_user_id TEXT NOT NULL DEFAULT '';

-- 一覧は所有者で絞って更新時刻順に並べる。既存の idx_boards_updated_at は
-- 所有者を先頭に持たないので、この並びには効かない。
CREATE INDEX idx_boards_owner_updated_at ON boards (owner_user_id, updated_at DESC);
