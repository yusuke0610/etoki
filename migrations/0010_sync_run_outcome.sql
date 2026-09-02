-- run が最後まで進んだかどうかと、途中で失敗した理由を記録する（ADR 0043）。
--
-- 途中で失敗しても作れたぶんは run として残す（ADR 0009）。だがその「途中で
-- 失敗した」という事実はどこにも保存しておらず、生きているのは作成の応答
-- 1 回ぶんだけだった。リロードすると、履歴からは「作れた件数が少ない run」と
-- 区別が付かない。再実行すべきかどうかを決める材料が消えているので、状態を
-- 見せて開発者に選ばせるという方針（中核思想 3）が成り立たなくなる。
--
-- **NULL を埋めない。** この列を足す前の run は「成功した」ではなく「記録して
-- いない」である。列を足す前にも途中失敗は起きていて、'complete' で埋めると
-- 知らないことを成功として断言することになる。sync_items の body（マイグレー
-- ション 0007）や action（同 0009）を既定値で埋められたのは、埋めた値が事実だ
-- と言えたからで、ここは言えない。
--
-- error は incomplete のときだけ入る。GitHub が返した生の文字列であり、
-- 利用者向けの文言ではない（ADR 0034）。
ALTER TABLE sync_runs ADD COLUMN outcome TEXT
  CHECK (outcome IN ('complete', 'incomplete'));

-- 比較は `=` ではなく `IS`。outcome が NULL のとき `outcome = 'incomplete'` は
-- NULL に評価され、CHECK は偽でないので通ってしまう。それでは「記録していない
-- のに理由だけ入った行」を DB が受け入れることになり、読む側がその 1 行の意味を
-- 決められない。`IS` は NULL も値として比べる。
ALTER TABLE sync_runs ADD COLUMN error TEXT
  CHECK (error IS NULL OR outcome IS 'incomplete');
