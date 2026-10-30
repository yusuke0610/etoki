---
name: land
description: 開いている PR がマージの条件を満たすかを判定し、マージ順と base の表を出し、ユーザーの確認を取ってから squash でマージし、main に入ったかと後続（積んだ上の段・bun install・CI の回し直し）まで見る。Dependabot の PR の判断も含む。「マージできるものはマージして」「マージしてよいか確かめて」「マージした」と言われたときに使う。
---

# マージ（着地）

**マージは取り消しにくい外向きの操作。** 確かめる順番を毎回同じにするために
ある。条件を 1 つ落とすと（表に載っていない PR が先に入る、など）、GitHub の
上では緑のまま main が壊れる。

マージの条件そのものの正本は `CONTRIBUTING.md`「マージ」。ここに写すのは
判定の手順だけ。

## 入口

| 言われたこと                     | 進め方                                         |
| -------------------------------- | ---------------------------------------------- |
| 「マージしてよいか確かめて」     | 1〜2 まで。表を出して止まる                    |
| 「マージできるものはマージして」 | 1〜5。3 の確認は省かない                       |
| 「マージした」（ユーザーが）     | 「マージしたと言われたら」から。4 の確かめと 5 |

## 1. 条件を判定する

```sh
.claude/skills/pr-watch/status.sh
```

PR ごとに次を全部満たすかを見る。

- **`=> full review 0 件` が出ている。** 最新の head に対する full review で
  指摘が 0 件、という条件の判定は `/pr-watch` の「full review の読み方」に
  従う。**`走行中` や `枠切れ` を 0 件と読まない**（#256）。
- **base が `main`。** 積んだ上の段は、下の段が入って `/sync-prs` で base を
  移すまで条件を満たさない。
- **`mergeable` が `clean`、CI が緑。**
- **マージ順の表に載っている**（次の 2）。

**満たさない PR はマージしない。** 何が足りないかを添えてユーザーに渡す。

## 2. 表を出す

マージ順と base の表を出す。**止めている PR も載せる。**

| 順  | PR  | base    | 状態             | 先に入れるもの            |
| --- | --- | ------- | ---------------- | ------------------------- |
| 1   | #A  | main    | 条件を満たす     | -                         |
| 2   | #B  | main    | 条件を満たす     | #A（#A が足す関数を呼ぶ） |
| -   | #C  | #B の枝 | まだマージしない | #B（積み）                |
| -   | #D  | main    | まだマージしない | ユーザーの判断待ち        |

**止めている PR を表から外さない。** 外した PR が先にマージされ、main の
usecase テストがコンパイルできなくなった（#373 → #374）。表に「まだマージ
しない」で載っていれば、マージする側が気づける。

「先に入れるもの」には依存を書く。

- **積み。** base が別の PR の枝。下の段が先。
- **意味の衝突。** 文字は衝突しないが、片方が足す・消す・名前を変える識別子を
  もう片方が使う。CLAUDE.md の表に行を足す、ADR の番号を採る、のような
  軽い衝突も書いておくと、マージする側が驚かない（`CONTRIBUTING.md`
  「判断の記録」）。

この表の順は、`/pr-watch` が full review の枠を使う順にもなる。

## 3. 確認を取る

**マージの前に、表を見せてユーザーに確認を取る。** 「マージできるものは
マージして」と言われていても、どれをどの順で入れるかは表を見せてから決める。
確認なしではマージしない。

## 4. マージする

**1 本ずつ squash でマージし、入ったことを確かめてから次へ進む。**

```sh
# sha を渡すと、確かめた head から動いていたときに GitHub が断る
gh api -X PUT "repos/{owner}/{repo}/pulls/<番号>/merge" \
  -f merge_method=squash -f sha=<確かめた head の sha>
```

`gh pr merge` は GraphQL を使うので、クラウドのセッションでは通らない。
REST か、GitHub の MCP ツール（`merge_pull_request`）を使う。

**main に入ったかを確かめる。** 「マージ済み」と出ても、積んだ PR は base の枝に
入っただけで main には届いていないことがある。#218・#198・#211・#212 が
届かず、#219 と #220 で出し直した。

```sh
gh api "repos/{owner}/{repo}/pulls/<番号>" --jq '"\(.merged) \(.base.ref) \(.merge_commit_sha)"'
git fetch origin main && git branch -r --contains <merge_commit_sha>   # origin/main が出ること
```

次の PR に進む前に `status.sh` を取り直す。main が進むと `mergeable` が
変わることがある。表で「先に入れるもの」にしていた PR が入ったなら、残りは
`/sync-prs` で取り込んで検査を通すまでマージしない。

## 5. 後続を片付ける

- **積んだ上の段は `/sync-prs` に渡す。** 下の段が squash で入ると、上の段は
  main と衝突する。取り込み方はそちら。
- **`web/bun.lock` が動いた PR を入れたら、ユーザーに知らせる。** 手元で
  `bun install`（`make setup`）を回さないと、古い依存のまま動く。
- **main 側の修正（vulncheck など）を CI に拾わせたいだけの PR は、取り込まずに
  close → reopen する。** 下の「CI を回し直す」。

### CI を回し直す

**CI は PR の head ではなく、マージ結果（`merge_commit_sha`）で走る。** re-run は
古いマージ結果のまま走るので、main の修正を拾わない。

1. `merge_commit_sha` の親が、いまの main の先頭かを見る。**GitHub はマージ結果
   を遅れて作り直す**ので、main が進んだ直後は古いままのことがある。

   ```sh
   gh api "repos/{owner}/{repo}/pulls/<番号>" --jq .merge_commit_sha
   gh api "repos/{owner}/{repo}/commits/<そのsha>" --jq '.parents[].sha'
   gh api "repos/{owner}/{repo}/git/ref/heads/main" --jq .object.sha
   ```

2. 親がいまの main なら close → reopen する。

   ```sh
   gh api -X PATCH "repos/{owner}/{repo}/pulls/<番号>" -f state=closed
   gh api -X PATCH "repos/{owner}/{repo}/pulls/<番号>" -f state=open
   ```

**head は変わらないので、full review はやり直さなくてよい。** 取り込み
（`/sync-prs`）との違いはそこ。

## マージしたと言われたら

ユーザーがマージしたときも、4 の「main に入ったか」と 5 の後続を見る。

- 入ったのは main か（積んだ PR を base の枝にマージしていないか）
- 積んだ上の段、`bun install`、CI を回し直す PR が残っていないか
- 表の残りの順番が変わらないか

## Dependabot の PR

**同じ条件で判断する。別の手順にはしない。** Dependabot の PR も、Claude が
マージしてよいのは最新の head への full review が 0 件のものだけ。CodeRabbit は
bot の PR を「Bot user detected」で飛ばすので、枠を使うなら `/pr-watch` で
依頼する。

そのうえで、中身ごとに見るものがある（ADR 0035）。**落ちているのは知らせで
あって、設定の不具合ではない**（`CONTRIBUTING.md`「依存の更新」）。

| 中身                                     | 見るもの                                                                                         |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------ |
| 生成器（`openapi-typescript`）が上がった | codegen drift で落ちる。その枝で `make codegen` を回して push する。head が変わるので依頼し直す  |
| Go の更新                                | devShell の Go（nixpkgs）より新しい版を要求していないか。要求するなら nixpkgs を上げるまで止める |
| 固定しているもの（`@playwright/test`）   | 出てこないはず。出てきたら設定の漏れ。理由は `web/CLAUDE.md`                                     |
| major の更新                             | 変更履歴の破壊的変更。lint / test が緑でも、使っている API が変わっていないか                    |
| `web/bun.lock` が動く                    | 入れたら `bun install` を知らせる（5）                                                           |

どれにも当たらず、CI が緑で full review が 0 件なら、ほかの PR と同じく表に
載せて確認を取る。

## やらないこと

- **条件を満たさない PR のマージ**（`CONTRIBUTING.md`「マージ」）。
- **確認なしのマージ。**
- **squash 以外でのマージ。** 積んだ上の段の取り込み方（`/sync-prs`）は、
  下の段が squash で入ることを前提にしている。
