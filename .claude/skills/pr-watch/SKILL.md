---
name: pr-watch
description: 開いている PR を見回り、full review の枠をどの PR に使うかを決め、依頼し、返信を待って「最新の head に対する full review が 0 件か」を判定する。0 件になった PR の件数を記録する。「PR を見回って」「full review を回して」「指摘がなくなるまで回して」と言われたとき、/loop で PR を見回るとき、PR を作ったセッションから見回りを渡されたときに使う。1 本の PR の指摘に答えるのは /pr-review、マージは /land。
---

# PR の見回り

複数の PR について、**full review の枠を使う順番を決め、依頼し、結果を読む。**
出すのは「この PR は最新の head に対する full review で 0 件になった」まで。

| 頼まれたこと                         | スキル       |
| ------------------------------------ | ------------ |
| 枠と順番を回し、0 件かを判定する     | `/pr-watch`  |
| 1 本の PR に付いた指摘に答える       | `/pr-review` |
| マージしてよいかを確かめ、マージする | `/land`      |
| main が進んだあと、PR を追従させる   | `/sync-prs`  |

**指摘への対応の手順はここに写さない。** 指摘が付いたら `/pr-review` を呼ぶ。
指摘への対応だけを頼まれたときに、見回りの手順まで読み込まずに済むように
分けてある（#398）。

## レビューは自動では付かない

**このリポジトリでは、PR を作っても CodeRabbit のレビューは付かない。** スター数
が少ないリポジトリは自動レビューの対象外で、要約のコメントは「This repository
does not receive automatic reviews」と「Trigger review」の欄を出したまま
止まる。Dependabot の PR は「Bot user detected」で止まる。

**レビューは `@coderabbitai full review` で依頼して初めて付く。** 増分の
`@coderabbitai review` はマージの条件にならない（`CONTRIBUTING.md`「マージ」）
ので、依頼するのは常に full review。

## 状態を見る

```sh
.claude/skills/pr-watch/status.sh          # 開いている PR すべて
.claude/skills/pr-watch/status.sh 394 375  # 番号で絞る
```

1 PR 1 行で出る。

| 列          | 中身                                                                         |
| ----------- | ---------------------------------------------------------------------------- |
| `head`      | いまの head                                                                  |
| `fr`        | 最後の full review の状態（下の「full review の読み方」）                    |
| `threads`   | CodeRabbit の未解決スレッドの数。取れなければ `?`（0 件とは読まない）        |
| `mergeable` | `mergeable` と `mergeable_state`（`clean` / `dirty` / `blocked` など）       |
| `ci`        | head の check-run。`未起動` は衝突を疑う（下の「CI が走らない」）            |
| `base`      | base のブランチ。`main` 以外は積んだ PR                                      |
| `=>`        | **`full review 0 件` が出たものだけが、マージの条件の full review を満たす** |

最後の行に、直近に終わった full review の時刻が出る。枠の目安に使う。

クラウドのセッションでは GraphQL が塞がれている。`status.sh` は REST だけで
引き、スレッドはプロキシの経路（`ccr/review_threads`）に切り替える。こちらは
書き手で絞れないので、**人が立てたスレッドも数える。**

## full review の読み方

**いちばん間違えやすいところ。** `status.sh` が判定まで出すが、読み方を知らない
と出力を疑えないので、ここに書く。

1. **依頼より後に head が進んでいたら、その依頼は数えない**（`要再依頼`）。
   docs だけのコミットでも同じ（`CONTRIBUTING.md`「マージ」）。
2. **依頼への返信を見る。** 返信は 1 つのコメントで、中身が書き換わる。
   - `triggered` のうちは**走っている**（`走行中`）。**ここで 0 件と読まない。**
     #256 は走行中を 0 件と読んでマージし、指摘がマージの後に付いた。
   - `Full review finished` になったら走り終えた。
   - `Action not completed` / `Review rate limited` は枠切れ（`枠切れ`）。
     その依頼では走らないので、`available in N minutes` の後に**依頼し直す。**
3. **走り終えたら、要約が読んだ commit を見る**（`要再依頼(読んだのは …)`）。
   要約の「between A and B」の B が、full review が読んだ head。**B がいまの
   head と一致しなければ、そのレビューは古い。**
4. **指摘 0 件の full review では、review が増えない。** `findings.sh` の
   1 行目（review の件数）は変わらないので、そこでは判定しない。判定は
   「head を読んだ」と「未解決スレッド 0」の 2 つで行う。

**要約の `updated_at` だけでは判定しない。** 要約は full review のあとに push
しただけでも書き換わる（「Trigger review」の欄が新しい head を指す）。
`updated_at` が依頼より後でも、読んだ commit が古いことがある（#394）。

## 枠の使い方

**full review の枠は org 全体で 1 時間に 1 本。** 走り終えるまで 6〜10 分
かかる。PR が複数あるときは、1 本ずつ順番に回す。

### 枠を使う PR を選ぶ

**マージ順の表（`/land`）の先頭から使う。** 0 件になっても、先に入れるものが
入らないとマージできない PR に枠を使っても、その枠は先送りされるだけ。積んだ
PR は表で下の段が先に来るので、この順で回せば下の段から埋まる。

表がまだ無いときは `/land` の「表を出す」で作る。番号の小さい順は採らない。
番号は作った順で、入れる順ではない。

### 依頼する前に揃えるもの

**枠は 1 時間に 1 本なので、依頼してから直すことになる状態では依頼しない。**

- CI が緑（走行中なら待つ）
- `mergeable` が `clean`。衝突しているなら `/sync-prs` で先に取り込む。
  取り込むと head が変わり、依頼はやり直しになる
- 未解決のスレッドが 0（前回の指摘を全部決着させた）
- 直すものはまとめて push し終えた

揃っている PR が無いなら、その回は依頼しない。

### 依頼と待ち

```sh
gh api -X POST "repos/{owner}/{repo}/issues/<番号>/comments" -f body='@coderabbitai full review'
```

1. **依頼したら、返信が `Full review finished` か `rate limited` になるまで待つ。**
   `status.sh <番号>` の `fr` が `走行中` から変わるのを見る。
2. **`Full review finished` が出たら、`status.sh` の `=>` で判定する。**
   - `full review 0 件` → 「0 件になったら」へ。
   - 未解決スレッドが残る → `/pr-review` で決着させる。直して push したら、
     次の枠で依頼し直す。
3. **`rate limited` なら、`available in N minutes` の後に依頼し直す。** 枠の
   読みが外れたということなので、そのあいだに他の PR に依頼しない。

## 0 件になったら

**PR を離れる場面なので、`docs/review-findings.md` に行を書く。** 書くもの・
数え方・還流と同じ場面で書く理由は `/pr-review` の「件数を記録する」と
`docs/review-findings.md` の冒頭にある。ここには写さない。

見回りは `/pr-review` を通らずに回ることが多く、ここで書かないと行が抜ける
（#336 で止まっていた）。

記録を書いたら、マージに進むかを `/land` に渡す。**このスキルはマージしない。**

## 見回りの間隔

`/loop` で回すときの間隔は、**待っているものから決める。**

| 待っているもの                        | 次に見るまで                                       |
| ------------------------------------- | -------------------------------------------------- |
| full review の結果                    | 返信の時刻から 10 分。まだ走っていれば 5 分刻み    |
| 枠（`rate limited` や直近の終了時刻） | `available in N minutes` か、直近の終了の 1 時間後 |
| CI                                    | 走り出してから 10 分                               |
| 何も待っていない（全部ユーザー待ち）  | 30 分。何度続いても縮めない                        |

**待つものが無いのに短い間隔で回さない。** 見るたびに API を叩くだけで、
状態は変わらない。

## 見回りは 1 つのセッションで

**PR を作ったセッションは見回らない。見回りは 1 つのセッションにまとめる。**
枠は org で 1 本なので、セッションごとに見回ると、互いの依頼を知らずに枠を
取り合い、`rate limited` で依頼が走らずに終わる。

### 受け取り口

PR を作ったセッションは、見回りのセッションに次を渡す。

- **PR の番号と base。** base が `main` 以外なら、積んでいる下の段の番号
- **衝突しうる相手。** 同じファイルを触っている開いた PR（`CLAUDE.md` の表、
  ADR の番号と `docs/adr/README.md` の末尾など）
- **`bun install` が要るか。** `web/bun.lock` が動いた PR
- **まだ直していない指摘や、ユーザー待ちの判断があるか**

受け取った側は `status.sh` で状態を取り直し、マージ順の表に載せる。

## CI が走らない

**push しても CI が走らないなら、衝突を疑う。** `mergeable` が `false`
（`dirty`）だと、`pull_request` のワークフローは起動しない。#259 ではこれで
1 時間空回りした。`status.sh` の `ci=未起動` と `mergeable=false/dirty` が
並んでいたら `/sync-prs` に回す。

## やらないこと

- **マージ。** `/land` の担当。
- **増分の `@coderabbitai review` を依頼すること。** マージの条件に
  ならない。
- **走行中や枠切れを 0 件と読むこと。** 判定は `status.sh` の `=>` だけを見る。
