# 0035: 依存の更新は Dependabot で知る（flake.lock は手のまま残す）

ステータス: 採用

## 文脈

`.github/` にあるのは `workflows/ci.yml` だけで、更新を拾う仕組みが無かった。
`flake.lock` / `bun.lock` / `go.mod` の更新は、誰かが思い出したときにしか起きない。

放っておくと困るのは、**このリポジトリがバージョンの連動を多く抱えている**ため。
CLAUDE.md と `web/CLAUDE.md` が「非自明な設定」として挙げているものが、そのまま
更新時の落とし穴になっている。

- `@playwright/test` は exact 指定で、devShell が渡す `playwright-driver.browsers`
  （nixpkgs 側）と揃っていないと E2E が起動しない
- `oapi-codegen`（`flake.lock`）と `openapi-typescript`（`bun.lock`）は生成器で、
  **バージョンが動けば生成物の形も変わる**。更新のコミットには `make codegen` の
  結果が要る（ADR 0011）
- React は 18 系に固定（`@excalidraw/excalidraw` との組み合わせ）

連動が多いほど、まとめて上げるのは高くつく。**間隔が空くほど「一度に動く量」が
増えるので、放置は先送りではなく増額。**

## 決定

**Dependabot で更新を知る。ただし「更新 PR がそのままマージできる」ことは
前提にしない。**

### Dependabot を使う。Renovate は採らない

Renovate のほうが表現力は上で、`postUpgradeTasks` を使えば更新 PR の中で
`make codegen` まで走らせられる。**それができれば、この構成でいちばん面倒な部分が
消える。**

採らなかったのは、その機能が self-hosted 前提で、単一ユーザーのローカルツール
（ADR 0004）に運用するものを 1 つ増やすため。ここで欲しいのは「更新を知る」ことで、
そこまでは GitHub 標準で足りる。

### エコシステムごとに週 1 本へまとめる

`groups` で 1 本にする。**PR が散らばると誰も見なくなる**ので、頻度と本数を絞る
ほうが、拾える確率は上がる。

### 更新 PR は赤くなってよい。落ちることが知らせ

生成器が動けば生成物を作り直す必要があるが、Dependabot はそれをやらない。だから
`openapi-typescript` が上がった PR は **codegen drift のジョブで落ちる**
（ADR 0011）。

**これを避けるために生成器を更新対象から外す、という選択は採らなかった。**
外すと、生成器だけが誰にも知らされないまま古くなる。落ちた PR は「連動している
ものが動いた」という知らせであって、失敗ではない。直すのは人で、やることは
その PR で `make codegen` を回すだけ。

### 固定しているものは対象から外す

`@playwright/test` は全部、React 系は major を無視する。**理由は
`web/CLAUDE.md` にあり、設定には書き写さない。** 同じ知識を 2 箇所に置くと
片方だけ変わる（`.markdownlint-cli2.yaml` が除外を自前で列挙せず `.gitignore` を
見ているのと同じ判断）。

**外すのは version updates だけで、`update-types` に 3 種類とも並べる。**
`ignore` に依存名だけを書くと、その依存は security updates も出なくなる
（`update-types` を書いたときだけ version updates に限定される）。ここで
止めたいのは「勝手に上がること」であって、**脆弱性の知らせまで止めると、
固定したことが黙って危険を抱える形になる。**

### flake.lock は自動化しない

**Dependabot には nix のエコシステムがあり、`flake.lock` の入力を上げる PR は
作れる**（version updates のみ。`flake.nix` に直接書いた ref は対象外）。
対応していないから手でやる、ではなく、**このリポジトリでは有効にしない**という
判断。

`flake.lock` の更新は 1 本で toolchain 全体（Go / Bun / `oapi-codegen` /
`playwright-driver`）を動かす。**そのうち `playwright-driver` は `bun.lock` 側の
`@playwright/test` と対で決まる**ので、直すには別のエコシステムのロックファイルを
同じ PR で動かすことになる。Dependabot はエコシステムをまたがないから、**この PR
だけは必ず人が引き取って手で揃える。** 出しても「手で `nix flake update` を回す」
までの距離が縮まらない。

定期実行の workflow で `nix flake update` の PR を作る案も採らなかった。

**`GITHUB_TOKEN` が作った PR では workflow が走らない**（再帰的な実行を防ぐための
GitHub の仕様）。つまり自動で作った flake.lock の PR には CI が付かず、**いちばん
見たい「E2E がまだ動くか」が出ない。** 更新されたことだけが分かる PR は、
`nix flake update` を手で叩くより安くない。

PAT を置けば回避できるが、それは長命の資格情報を 1 つ増やすということ。
**知らせとして弱いものに資格情報は払わない。**

## 結果

- `go.mod` / GitHub Actions / `web` の依存は、週次でまとまった PR として出る
- **nixpkgs 側（Go / Bun / `oapi-codegen` / `playwright-driver`）は手のまま。**
  このリポジトリでいちばん連動が多いのがそこなので、穴はここに残る。
  `nix flake update` を回したあとに `@playwright/test` を揃える手順は
  `web/CLAUDE.md` のまま変わらない
- 脆弱性由来の更新（Dependabot alerts / security updates）は、ここで決めた
  version updates の出し方とは別に効く。**ただし完全に独立ではない。**
  `ignore` に依存名だけを書いた依存は security updates も出なくなるので、
  外すときは `update-types` で version updates に限定する（上記）
- **`flake.lock` は security updates の対象にならない。** nix のエコシステムを
  有効にしても version updates しか出ないので、そこは有効にするかどうかに
  関わらず手で見るしかない
