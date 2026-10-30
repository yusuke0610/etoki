---
name: sync-prs
description: main が進んだあと、開いている PR のうち取り込みが要るものだけを選び、main（積んだ上の段は下の段の最後の head → main の順）をマージで取り込み、衝突を解き、検査を通してから push する。rebase と force push はしない。「衝突を解消して」「main の変更が各 PR に影響しないか見て」と言われたとき、/land で PR が main に入ったあとに使う。
---

# main への追従

main に何かが入ったあと、開いている PR を追従させる。

**取り込みには full review が付いてくる。** 取り込むと head が変わり、full review
（1 時間に 1 本）をやり直すことになる。**要らない取り込みも、要る取り込みの
漏れも、どちらも高くつく。** このスキルの半分は、取り込まない PR を見分ける
ことにある。

## 1. 一覧を出す

```sh
.claude/skills/sync-prs/list.sh
```

1 PR 1 行で、`mergeable`、base、main からの遅れ（main にあって head に無い
コミットの数）、積み方、この手元の worktree で開いているかが出る。

## 2. 取り込みが要る PR だけを選ぶ

**main から遅れているだけでは取り込まない。** 取り込むのは次のどれかに当たる
ものだけ。

| 当たるもの              | 見分け方                                                   |
| ----------------------- | ---------------------------------------------------------- |
| 衝突している            | `mergeable=false`（`dirty`）                               |
| 積んだ下の段が入った    | `/land` から渡された。または base が main に移って `dirty` |
| 意味の衝突がありうる    | `gone.sh` が名前を出した（下）                             |
| main の変更を前提に直す | PR の中身が main に入ったものを使う必要がある              |

**CI に main の修正（vulncheck など）を拾わせたいだけなら、取り込まない。**
close → reopen で済み、head が変わらないので full review もやり直さずに済む。
手順は `/land` の「CI を回し直す」。

`mergeable` が `null` のうちは GitHub が計算中。少し置いて取り直す。

### 意味の衝突を探す

```sh
.claude/skills/sync-prs/gone.sh <PR の枝>
```

**探す範囲は狭く決めてある。** PR の分岐点から main までで定義（Go の
`func` / `type`、TypeScript の `export`）が消え、main のどこにも残っていない
名前を、PR が足した行で使っていないか。引数の変わった関数や、PR が以前から
あった行で使っているものは拾わない。

**ここで出なくても、意味の衝突が無いとは言えない。** 確定は取り込んだあとの
`make lint` / `make test` が行う。`gone.sh` は「取り込みが要るか」を決める
手掛かりで、出たら取り込む側に入れる。

## 3. PR ごとに取り込む

### 枝を触ってよいか

**他所で開いている枝は、rebase も force push もしない。** 取り込みは常に
マージで行う。マージなら、他所の手元は `git pull` で追いつける。

- `list.sh` の `worktree=` が出た枝は、この手元の別の worktree で作業中。
  そこを使うか、作業しているセッションに任せる。
- **他のセッションが作業中だと分かっている枝は、取り込む前にユーザーに
  訊く。** クラウドのセッションの手元は `git worktree list` に出ないので、
  作業中かは訊かないと分からない。push が重なると、向こうの push が
  non-fast-forward で断られる。

### 取り込む

worktree で取り込み、いまの作業ツリーを汚さない。

```sh
git fetch origin main <枝>
git worktree add ../etoki-sync-<番号> <枝>
cd ../etoki-sync-<番号>
```

**積んだ上の段は、下の段の最後の head、main の順に取り込む。** 下の段が squash
で入ったあとに main を直に取り込むと、下の段の変更が両側にあって衝突する。
下の段の最後の head を先に取り込めば、その変更は共通の祖先になる。

```sh
git merge <下の段の最後の head の sha>   # 下の段の枝が消えていても、sha は /land の記録や PR に残る
git merge origin/main
```

上の段の base が下の段の枝のままなら、GitHub の上で base を main に移す。

```sh
gh api -X PATCH "repos/{owner}/{repo}/pulls/<番号>" -f base=main
```

積んでいない PR は `git merge origin/main` だけ。

### 衝突を解く

**`--ours` で済む場面がある。** 下の段の最後の head を取り込んだあとで、
main の tree がその head の tree と一致するとき。main は下の段を squash した
だけで、この枝がすでに持っているもの以外を足していない。衝突は、上の段が
下の段の行をさらに書き換えた箇所に出るだけなので、この枝の側を採ればよい。

```sh
git rev-parse 'origin/main^{tree}' '<下の段の最後の head>^{tree}'   # 2 行が一致するか
git checkout --ours -- <衝突したファイル> && git add <衝突したファイル>
```

一致しないなら、ファイルごとに中身を読んで解く。CLAUDE.md の表や
`docs/adr/README.md` の末尾のように、**両方が行を足しただけの衝突は両方残す。**
ADR の番号が重なったら、後から入る側（この PR）の番号を進める。

### 検査を通してから push する

**取り込んだら `make codegen` を回す。** main 側で生成器が上がっていると、
生成物が古いまま残る（#168）。**lockfile（`web/bun.lock`）が動いたら
`bun install` も回す。**

```sh
make codegen && git status --short   # 生成物が動いたらマージのコミットに含める
make lint
make test
```

UI かハンドラに main 側の変更が入ったなら `make test-e2e` も（`CONTRIBUTING.md`
「コミット前に通すもの」）。**意味の衝突は文字の衝突が無くてもここで初めて
分かる。** 落ちたら、マージのコミットの前に直す。

```sh
git push origin <枝>
cd - && git worktree remove ../etoki-sync-<番号>
```

**push しても CI が走らないなら、まだ衝突している。** `mergeable` が `false`
だと `pull_request` のワークフローは起動しない（#259）。

## 4. 結果を /pr-watch に渡す

取り込んだ PR は head が変わったので、full review をやり直す。表にして渡す。

| PR  | 取り込んだもの          | 解いた衝突       | 検査              | full review                   |
| --- | ----------------------- | ---------------- | ----------------- | ----------------------------- |
| #A  | main                    | なし             | lint / test       | やり直す                      |
| #B  | #A の最後の head → main | `CLAUDE.md` の表 | lint / test / e2e | やり直す                      |
| #C  | 取り込まない（CI だけ） | -                | -                 | 要らない（`/land` で reopen） |

## やらないこと

- **rebase と force push。** 他所の手元が追いつけなくなる。
- **マージ。** `/land` の担当。
- **取り込みが要らない PR の取り込み。** 遅れているだけなら置いておく。
