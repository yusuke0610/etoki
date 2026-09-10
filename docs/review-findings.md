# CodeRabbit の指摘の件数

PR ごとの初回指摘数を記録する。**先回りの観点（`.claude/rules/`）を足したことが
効いているかは、件数でしか分からない**（[ADR 0033](adr/0033-review-derived-rules.md)）。

**書くのは `/pr-review` の最後、決着した指摘を rules に還すのと同じ場面。**
別の場面にすると、片方だけ忘れたことに気づけない。手順は
[`.claude/skills/pr-review/SKILL.md`](../.claude/skills/pr-review/SKILL.md)。

## 数え方

- **初回指摘** — その PR で最初に付いたレビューの指摘数。返信・確認応答・
  walkthrough は数えない。push 後の再レビューで新しく付いたものは数える。
- **直した** — 修正して push したもの。指摘とは違う形で直したものも含む。
- **取り下げ** — 理由を返して CodeRabbit が撤回したもの。
- **未決着** — マージ時点で決着していないもの。0 でない行には理由を添える。

**取り下げを別に数える。** rules に「やらないこと」を書いても、CodeRabbit の
入力にはならないので**この列は減らない**。減らせるのは「直した」の側、つまり
事前に潰せたはずの指摘だけ。両方を 1 つの数にすると、効いているかが見えなくなる。

## 記録

| PR                                                   | 初回指摘 | 直した | 取り下げ | 未決着 | 還した先                                                                                     |
| ---------------------------------------------------- | -------: | -----: | -------: | -----: | -------------------------------------------------------------------------------------------- |
| [#96](https://github.com/yusuke0610/etoki/pull/96)   |        6 |      2 |        4 |      0 | async-ui / validation-boundaries                                                             |
| [#100](https://github.com/yusuke0610/etoki/pull/100) |        4 |      1 |        3 |      0 | validation-boundaries                                                                        |
| [#104](https://github.com/yusuke0610/etoki/pull/104) |        4 |      3 |        1 |      0 | docs-consistency / test-effectiveness                                                        |
| [#105](https://github.com/yusuke0610/etoki/pull/105) |        3 |      1 |        2 |      0 | async-ui / validation-boundaries                                                             |
| [#106](https://github.com/yusuke0610/etoki/pull/106) |        2 |      1 |        1 |      0 | docs-consistency                                                                             |
| [#108](https://github.com/yusuke0610/etoki/pull/108) |        9 |      5 |        4 |      0 | 還していなかった。#126 で api-contract / async-ui / test-effectiveness / docs-consistency へ |
| [#109](https://github.com/yusuke0610/etoki/pull/109) |        5 |      3 |        2 |      0 | async-ui / validation-boundaries                                                             |
| [#117](https://github.com/yusuke0610/etoki/pull/117) |        4 |      0 |        3 |      1 | validation-boundaries。未決着は DB の CHECK（#126 で還した）                                 |
| [#121](https://github.com/yusuke0610/etoki/pull/121) |        1 |      1 |        0 |      0 | 還していなかった。#126 で async-ui へ                                                        |
| [#129](https://github.com/yusuke0610/etoki/pull/129) |        4 |      3 |        0 |      1 | async-ui / pr-review スキル / validation-boundaries を直接修正。未決着は DB の CHECK（#130） |

**#96 より前は数えていない。** ADR 0033 の棚卸し（37 PR）は系統だけを見ていて、
PR ごとの件数を残していない。

## いつ見返すか

**行を足す人が、5 行ぶん溜まったところで「直した」の列を見る。** 書く場面が
`/pr-review` の最後だけなので、見る場面もそこに寄せる。減っていなければ、足した
観点が CodeRabbit の見ているものと違うということなので、件数の多かった PR の
指摘を系統ごとに引き直す（この表を作ったときと同じ手順）。

**「取り下げ」が減らないのは想定どおり**なので、そちらは判断材料にしない。

## 表を作ったとき（#96〜#121、#126 で棚卸し）に分かったこと

**下は #96〜#121 の 9 PR を数えた時点の話で、以降の行は含まない。** 続きは表を
見る。

**取り下げが半分を超えていた**（38 件中 20 件）。ADR 0033 で rules に「やらない
こと」を集め始めたあとも減っていない。`satisfies Reply<T>` を重ねる提案は #81 で
撤回して rules に残したが、#96 と #105 で同じ形で来た。**却下の記録が効くのは
返事の速さで、指摘の発生ではない。**

**差分が大きい PR ほど多い。** この 9 PR では #108（9 件）が最大で、指摘ゼロの
PR は無かった。

**#108 と #121 は rules に還っていなかった。** `/pr-review` は「その PR を離れる
前に書き切る」と言っているのに、いちばん件数の多かった PR が抜けている。件数を
残していれば、還した先が空の行として見えていた。**この表を始めた理由がそれ。**
