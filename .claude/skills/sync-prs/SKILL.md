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
**fork の PR は `fork(…) 対象外` と出て、ここでは扱わない。** head が origin に
無く、取り込んでも push する先が無い。

## 2. 取り込みが要る PR だけを選ぶ

**main から遅れているだけでは取り込まない。** 取り込むのは次のどれかに当たる
ものだけ。

| 当たるもの           | 見分け方                                                            |
| -------------------- | ------------------------------------------------------------------- |
| 衝突している         | `mergeable=false`（`dirty`）                                        |
| 積んだ下の段が入った | `/land` から渡された。または `list.sh` の積みに「マージ済み」と出た |

**積みに「判定できない」と出た PR は、取り込まずに止める。** マージ済みの下の段が
あるかを `list.sh` が確かめられなかった（検索が落ちた、打ち切られた、など）。
取り込むかどうかは、下の段を確かめてから決める。
| 意味の衝突がありうる | `gone.sh` が名前を出した（下） |
| main の変更を前提に直す | PR の中身が main に入ったものを使う必要がある |

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

**ここで出なくても、意味の衝突が無いとは言えない。** 取り込んだあとの
`make lint` / `make test` で見つかるのも、型とテストが見ている範囲だけ。`gone.sh` は
「取り込みが要るか」を決める手掛かりで、出たら取り込む側に入れる。

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
git merge --ff-only origin/<枝>   # 同じ名前の古いローカル枝を掴んでいたら進める
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/<枝>)"   # 手元が先に進んでいないか
test -z "$(git status --porcelain)"   # 手を付けていない作業ツリーか
```

**`git status --porcelain` が空でなければ、止めてユーザーに訊く。** `worktree add`
が既にある worktree を指していた、などで、ほかの作業の変更や未追跡のファイルが
残っている。そのまま進むと、下の検査とコミットに混ざって push される。

**`--ff-only` が通らないか、そのあと `HEAD` が `origin/<枝>` と一致しなければ、
止めてユーザーに訊く。** 手元の枝に push していないコミットがある（手元が先に
進んでいると `--ff-only` は通ってしまう）。そのまま取り込むと、検査するのは PR と
違うコードになり、push には PR に無かったコミットが混ざる。

**積んだ上の段は、下の段の最後の head、main の順に取り込む。** 下の段が squash
で入ったあとに main を直に取り込むと、下の段の変更が両側にあって衝突する。
下の段の最後の head を先に取り込めば、その変更は共通の祖先になる。

```sh
# 下の段の枝が消えていても、head は PR の ref から取れる。sha は /land の記録や PR に残る
git fetch origin refs/pull/<下の段の PR 番号>/head
git merge <下の段の最後の head の sha>
git rev-parse 'origin/main^{tree}' '<下の段の最後の head>^{tree}'   # 2 行が一致するか
git merge -s ours origin/main   # 一致したとき
git merge origin/main           # 一致しないとき
```

**main の tree が下の段の最後の head の tree と一致したら、`-s ours` でつなぐ。**
main は下の段を squash しただけで、この枝がすでに持っているもの以外を足して
いない。ふつうのマージにすると、merge base が squash 前の main になり、上の段が
下の段の変更を元に戻した箇所を、衝突なしで下の段の側に戻してしまう。`-s ours` は
この枝の tree を変えずに main を祖先にする。

**一致しないときは、ふつうのマージのあとに、上の段の変更が残っているかを見る。**
main が下の段のあとにも進んでいるので、同じ戻しが起きうる。取り込む前の上の段と
いまの HEAD の差が、main が下の段のあとに足した分（`git diff <下の段の最後の head>
origin/main`）だけになっているかを読む。

**1 つのマージが衝突したら、解いて `git merge --continue` で終えてから次へ
進む。** 終えないまま次の `git merge` を叩くと、未完了のマージに断られる。

積んでいない PR は `git merge origin/main` だけ。

### 衝突を解く

main の tree が下の段の最後の head と一致するときは、上の `-s ours` で衝突は
出ない。それ以外は、ファイルごとに中身を読んで解く。CLAUDE.md の表や
`docs/adr/README.md` の末尾のように、**両方が行を足しただけの衝突は両方残す。**
ADR の番号が重なったら、後から入る側（この PR）の番号を進める。

### 検査を通してから push する

**取り込んだら `make codegen` を回す。** main 側で生成器が上がっていると、
生成物が古いまま残る（#168）。**作ったばかりの worktree には `web/node_modules` が
無い**ので、lockfile が動いていなくても先に `bun install --frozen-lockfile` を回す
（codegen が `openapi-typescript` を読む）。

**道具は取り込んだあとの flake のものを使う。** `make` は devShell の印の値を
その作業ツリーの `flake.nix` / `flake.lock` と比べ、違えば入り直す（Makefile の
冒頭、#409）。main を取り込めばこの Makefile も入るので、`make` を通す限り古い
devShell の道具では走らない。`go test` や `bunx` を直に叩くときは、その worktree で
`nix develop` に入り直す。

```sh
nix develop --command bash -c 'cd web && bun install --frozen-lockfile'
make codegen && git status --short   # 生成物が動いたら下のコミットに含める
make lint
make test
make test-e2e   # UI かハンドラに main 側の変更が入ったときだけ
```

`make test-e2e` を回すかは `CONTRIBUTING.md`「コミット前に通すもの」。**文字の
衝突が無い意味の衝突も、検査が見ている範囲ならここで見つかる。** テストの無い
振る舞いの変化は、ここでは見つからない。落ちたら直して、もう一度回す。

**衝突の無いマージはその場でコミットされる。** 生成物と、検査で落ちて直した
もの（E2E を含む）はマージのコミットに入らず作業ツリーに残るので、push の前に
別のコミットにする。**動いたものが無ければコミットしない。** 何もステージせずに
`git commit` すると「nothing to commit」で止まる。

```sh
git status --short   # 何も出なければ、下のコミットは飛ばす
git add -- <動いた生成物と直したファイル> && git commit -m "build: regenerate after merging main"
```

**`git add -A` にしない。** ignore されていないものは何でも入る。名前を挙げて
入れれば、見ていないファイルは入らない。

```sh
# どれかが落ちたら、そこで止まる（&& でつなぐ）
git push origin <枝> &&
  test "$(git ls-remote origin refs/heads/<枝> | cut -f1)" = "$(git rev-parse HEAD)" &&
  cd - && git worktree remove ../etoki-sync-<番号>
```

**push が断られたか、origin の head が手元と違えば、そこで止めて訊く。** worktree も
base もそのまま残す。`ls-remote` が落ちたときも同じ。
ほかのセッションが同じ枝へ先に push していると断られる。そのまま進むと、検査した
head が GitHub に無いまま worktree を消し、base だけを変えてしまう。push のあと、
origin の枝の head が手元の `HEAD` と同じかも見る。

**下の段がマージ済みで、上の段の base が下の段の枝のままなら、push したあとに
base を main に移す。** 下の段がまだ開いているなら、base は下の段の枝のまま
にする（main に移すと、下の段の変更が上の段の差分に混ざる）。先に移すと、検査が
落ちて push できなかったとき、取り込む前の head のまま base だけが main になった
PR が残る。`/land` は base が main の PR をマージの候補にするので、検査していない
状態を入れうる。

```sh
gh api -X PATCH "repos/{owner}/{repo}/pulls/<番号>" -f base=main
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
