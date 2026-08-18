-- その run が item に対して何をしたのかを記録する（ADR 0026）。
--
-- changed の注釈に対して、既存の draft issue を更新できるようにしたので、
-- 1 つの run の中に「新しく作った」と「前のを書き換えた」が混ざるようになった。
-- 作成の結果を見せるとき、どちらだったのかが分からないと、開発者は GitHub 側に
-- 何が増えたのかを数えられない。
--
-- **run は実行の記録のままにする（ADR 0007）。** 触っていない item をコピーして
-- 積むと、run が「1 回の実行」ではなく「そのときの全体像」に変わる。いま
-- GitHub に在るものは、run 履歴を item_id で畳んで出す
-- （MappingRepository.ListItemsByAnnotation）。畳めば、更新は同じ item_id に
-- 吸収され、今回触らなかった item も残り続ける。
--
-- 既定は 'created'。この列を足す前の run はすべて新規作成だった。
ALTER TABLE sync_items ADD COLUMN action TEXT NOT NULL DEFAULT 'created'
  CHECK (action IN ('created', 'updated'));
