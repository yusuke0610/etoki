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
  従う。**`走行中` や `枠切れ` を 0 件と読まない**（#256）。Dependabot の PR は
  full review が付かないので、この条件を下の「Dependabot の PR」で読み替える。
- **base が `main`。** 積んだ上の段は、下の段が入って `/sync-prs` で base を
  移すまで条件を満たさない。
- **`mergeable` が `clean`、CI が緑。**
- **人が立てた未解決のスレッドが無い。** `status.sh` の `threads` は、GraphQL の
  経路では CodeRabbit のスレッドしか数えない。PR の画面かレビューのスレッドの
  一覧で、人のスレッドも見る。
- **CI が、いまの main とのマージ結果で走っている。** 時刻ではなく main の sha で
  見る。CI を走らせる（5 の「CI を回し直す」で回し直す）ときに、`merge_commit_sha`
  の親がいまの main であることを確かめ、その main の sha を控える。マージの直前に
  main の sha を取り直して控えと比べ、違っていれば回し直す。控えが無い（どの main
  で走った CI か分からない）ときも回し直す。時刻で比べると、同じ秒に起きた更新や、
  コミッターの日付と main が進んだ時刻のずれを見逃す。
- **マージ順の表に載っている**（次の 2）。

**満たさない PR はマージしない。** 何が足りないかを添えてユーザーに渡す。

## 2. 表を出す

マージ順と base の表を出す。**止めている PR も載せる。**

| 順  | PR  | base    | head（40 桁） | 状態             | 先に入れるもの            |
| --- | --- | ------- | ------------- | ---------------- | ------------------------- |
| 1   | #A  | main    | `<sha>`       | 条件を満たす     | -                         |
| 2   | #B  | main    | `<sha>`       | 条件を満たす     | #A（#A が足す関数を呼ぶ） |
| -   | #C  | #B の枝 | `<sha>`       | まだマージしない | #B（積み）                |
| -   | #D  | main    | `<sha>`       | まだマージしない | ユーザーの判断待ち        |

**head は条件を判定した時点の 40 桁で載せる。** マージの直前に取り直した head と
比べるのはこの値で、比べないと、確認を待つあいだに push された未レビューの head を
そのままマージする（GitHub が `sha` で見るのはマージの瞬間の head だけ）。

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

**マージの直前に、その PR の head・`base`・`mergeable` と、review・CI の判定を
取り直す。** 1 本目も同じ。確認を取っているあいだに push や base の付け替えが
入りうるし、head が同じでも新しい指摘や落ちた check-run が付きうる。**head が表の
40 桁と違っていたら、マージせずに条件の判定とユーザーの確認からやり直す。**
base や `mergeable` が違っていたら、表を作り直す。**`status.sh` に
`=> full review 0 件` と `ci=緑` が出なくなっていたら、マージせずに止めて、表と
確認を作り直す。** Dependabot の PR には `=> full review 0 件` が出ない（依頼
しない）ので、`ci=緑` と、CI が走った main がいまの main と同じこと（1 の
「CI が、いまの main とのマージ結果で走っている」）を見る。中身の確かめは、head
**と main** が確かめたときと同じなら済んでいる。main が進んでいたら、major の
更新は破壊的変更と etoki の使い方を照らし直す。そのあいだに入ったコードが、
消えた API を使い始めているかもしれない（下の「Dependabot の PR」）。

```sh
.claude/skills/pr-watch/status.sh <番号>   # => full review 0 件 と ci=緑 が出ているか（Dependabot は ci=緑）
gh api "repos/yusuke0610/etoki/pulls/<番号>" --jq '"\(.head.sha) \(.base.ref) \(.mergeable)"'
# sha を渡すと、確かめた head から動いていたときに GitHub が断る。
# status.sh が出すのは先頭 7 桁なので、上で取り直した 40 桁を渡す。
gh api -X PUT "repos/yusuke0610/etoki/pulls/<番号>/merge" \
  -f merge_method=squash -f sha=<取り直した head の 40 桁の sha>
```

`gh pr merge` は GraphQL を使うので、クラウドのセッションでは通らない。
REST か、GitHub の MCP ツール（`merge_pull_request`）を使う。

**取り直してからマージするまでのあいだに付いた指摘や落ちた check-run は防げない。**
`sha` が断るのは head が動いたときだけで、main のルールも review や CI の結果を
マージの条件にしていない。**マージしたら、その PR の `status.sh` を取り直す。**
`fr` に指摘が増えていたか、`ci` が緑でなくなっていたら、ユーザーに知らせる
（戻すかどうかはユーザーが決める）。

**main に入ったかを確かめる。** 「マージ済み」と出ても、積んだ PR は base の枝に
入っただけで main には届いていないことがある。#218・#198・#211・#212 が
届かず、#219 と #220 で出し直した。

```sh
gh api "repos/yusuke0610/etoki/pulls/<番号>" --jq '"\(.merged) \(.base.ref) \(.merge_commit_sha)"'
git fetch origin main && git branch -r --contains <merge_commit_sha>   # origin/main が出ること
```

次の PR に進む前に `status.sh` を取り直す。main が進むと `mergeable` が
変わることがある。表で「先に入れるもの」にしていた PR が入ったなら、残りは
`/sync-prs` で取り込んで検査を通すまでマージしない。

**依存の無い PR も、main が進んだら CI を新しい main で回し直してから判定する。**
先に入れた PR は残りの head を変えないので CI は走り直さず、古い main との
マージ結果で付いた緑が残る。`status.sh` の `ci` は check-run の新旧を区別しない。
5 の「CI を回し直す」で close → reopen し、reopen より後に始まった check-run が
すべて緑で終わってから、この PR の判定に戻る。

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
   gh api "repos/yusuke0610/etoki/pulls/<番号>" --jq .merge_commit_sha
   gh api "repos/yusuke0610/etoki/commits/<そのsha>" --jq '.parents[].sha'
   gh api "repos/yusuke0610/etoki/git/ref/heads/main" --jq .object.sha
   ```

2. 親がいまの main なら close → reopen する。

   ```sh
   gh api -X PATCH "repos/yusuke0610/etoki/pulls/<番号>" -f state=closed
   gh api -X PATCH "repos/yusuke0610/etoki/pulls/<番号>" -f state=open
   ```

   **close する前に、番号をユーザーへの報告に書いておく。** reopen が失敗したり
   セッションが途中で切れたりすると PR は閉じたまま残り、開いている PR の一覧
   （`status.sh` / `list.sh`）にも出ない。reopen は取り直して `state` が `open`
   かを確かめ、開いていなければ開き直す。開き直せなければ、閉じたままの番号を
   ユーザーに知らせる。**途中で切れたセッションを引き継いだら、最初に、報告に
   残った番号の PR が閉じたままでないかを `state` で確かめ、閉じていれば開き直す。**

3. **CI のジョブ（`.github/workflows/` が起こす check-run）が、reopen より後に
   始まったものに入れ替わり、すべて緑で終わるまで待つ。** head が同じなので、
   新しい run が出るまでは古い緑がそのまま見える（`status.sh` の `ci` も同じ）。
   **どれかが赤になったら、その PR はマージせずに落ちたジョブをユーザーに知らせる。**
   reopen から 15 分たっても新しい run が出なければ、未起動として止めて知らせる
   （衝突で起動しないことが多い。`/pr-watch` の「CI が走らない」）。

   ```sh
   gh api "repos/yusuke0610/etoki/commits/<head>/check-runs" \
     --jq '.check_runs[] | "\(.started_at) \(.status) \(.conclusion) \(.name)"'
   ```

**head は変わらないので、full review はやり直さなくてよい。** 取り込み
（`/sync-prs`）との違いはそこ。

## マージしたと言われたら

ユーザーがマージしたときも、4 の「main に入ったか」と 5 の後続を見る。

- その PR の `status.sh <番号>` を取り直し、マージの前後に付いた指摘や赤い CI が
  あればユーザーに知らせる（4 のマージのあとと同じ）
- 入ったのは main か（積んだ PR を base の枝にマージしていないか）
- 積んだ上の段、`bun install`、CI を回し直す PR が残っていないか
- 表の残りの順番が変わらないか

## Dependabot の PR

**full review の代わりに、CI と中身で判断する**（ADR 0035 の追記）。CodeRabbit は
bot の PR を、review の席が無いとして飛ばす。`@coderabbitai full review` を依頼しても
「Review skipped」で終わる（#424）。**依頼しない。**

full review 0 件の代わりに見るのは 2 つ。ほかの条件（base・`mergeable`・CI が
いまの main とのマージ結果で走っている）はそのまま。

- **CI が緑。** main が進んでいたら、Update branch で main を取り込んでから
  回し直す（下）。`@dependabot rebase` には応じないことがある（#424）。

  ```sh
  gh api -X PUT "repos/yusuke0610/etoki/pulls/<番号>/update-branch" \
    -f expected_head_sha=<いまの head の 40 桁>
  ```

- **下の表の「見るもの」を確かめ終えた。**

中身ごとに見るものは次のとおり（ADR 0035）。**落ちているのは知らせで
あって、設定の不具合ではない**（`CONTRIBUTING.md`「依存の更新」）。

| 中身                                     | 見るもの                                                                                         |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------ |
| 生成器（`openapi-typescript`）が上がった | codegen drift で落ちる。その枝で `make codegen` を回して push する。head が変わるので CI を見る  |
| Go の更新                                | devShell の Go（nixpkgs）より新しい版を要求していないか。要求するなら nixpkgs を上げるまで止める |
| 固定しているもの（`@playwright/test`）   | version update は出てこないはず。出てきたら設定の漏れ。security update は出る（下）              |
| major の更新                             | 変更履歴の破壊的変更。lint / test が緑でも使う API に当たらないか。main が進めば見直す           |
| `web/bun.lock` が動く                    | 入れたら `bun install` を知らせる（5）                                                           |

**固定しているものにも security update は出る。** `ignore` で外しているのは
version update だけで、脆弱性の知らせは止めていない（ADR 0035）。同じ依存の
Dependabot alert が開いていれば security update で、設定の漏れではない。
コマンドが 0 以外で終わったら、0 件と読まずにどちらとも決めない。

```sh
gh api "repos/yusuke0610/etoki/dependabot/alerts?state=open&package=<依存名>" -q length
```

`@playwright/test` は devShell の `playwright-driver`（nixpkgs）と同じ版でないと
E2E が起動しない（`web/CLAUDE.md`）。nixpkgs に同じ版が来るまで止める。

確かめ終えて CI が緑なら、ほかの PR と同じく表に載せて確認を取る。表には、
full review の代わりに何を確かめたか（major の破壊的変更と、使っているかどうか）を
添える。

## やらないこと

- **条件を満たさない PR のマージ**（`CONTRIBUTING.md`「マージ」）。
- **確認なしのマージ。**
- **squash 以外でのマージ。** 積んだ上の段の取り込み方（`/sync-prs`）は、
  下の段が squash で入ることを前提にしている。
