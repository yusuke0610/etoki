-- 書き込みが GitHub に届いたことを確かめられたかどうかを持たせる。
--
-- ここまでの sync_items は「作った（または書き換えた）ことが確かな 1 件」しか
-- 表せなかった。GitHub が受理したあとで応答だけを失うと item ID が分からず、
-- その行は書けないまま捨てられていた（#170）。捨てると、GitHub には draft
-- issue があるのに記録が無い状態になる。ADR 0009 がいちばん避けたかったもの。
--
-- **既定値 1 は移行のためだけにある。** この列を足す前の行はすべて確定済みで、
-- 未確定の行は書けなかった。SaveRun は常に明示的に書く。
--
-- CHECK は「確定しているなら item ID がある」を固定する。未確定の作成は ID を
-- 知らないので空文字で入る。確定しているのに ID が無い行は、畳み込み
-- （ADR 0026）のグループを空文字で作ってしまい、別々の item が 1 つに混ざる。
ALTER TABLE sync_items
  ADD COLUMN confirmed INTEGER NOT NULL DEFAULT 1
  CHECK (confirmed IN (0, 1) AND (confirmed = 0 OR item_id <> ''));
