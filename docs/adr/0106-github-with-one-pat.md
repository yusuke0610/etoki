# 0106. GitHub は PAT 1 本で叩き、ログインは誰であるかを決めるだけにする

- 状態: 採用
- 日付: 2026-11-27
- ADR 0015 のうち、`GitHubTokenSource`・「OAuth を設定したら PAT は無視する」・
  資格情報の暗号化保存・`Config.Mode` による分岐を置き換える

## 背景

ADR 0015 は、GitHub App でログインした人ごとのトークン（user-to-server）で GitHub を
叩くと決めた。理由は 2 つあった。

- PAT に求めている粒度をそのまま要求できるのは GitHub App だけで、マルチユーザー化
  すると PAT 1 本では「誰のトークンで作ったか」が追えなくなる。
- 認証基盤を差し替えても GitHub は叩けるように、トークンの出どころを
  `port.GitHubTokenSource` という継ぎ目に切り出す。

2026-11-25 に人に出すための開発を終え、作者 1 人が Cloud Run で動かす形に切り替えた
（#491）。ログインできるのも 1 人に絞った（ADR 0105）。そこで次のことが分かった。

- **利用者が 1 人なら、作成の主体は最初から 1 人で決まっている。** 利用者ごとに
  トークンを分ける理由（ADR 0015 の「OAuth を設定したら PAT は無視する」）が
  成り立たない。
- **個人アカウントの Projects v2 に書けない見込み。** GitHub App の権限には
  Projects v2 をユーザー所有のスコープで許す項目が無く、Repository と Organization の
  permissions にしか `Projects` が無い。作者が使いたい Project が個人のものでも
  作れるようにしたい。classic PAT の `project` スコープなら扱える。
- **利用者ごとのトークンのために抱えているものが重い。** 暗号化の鍵
  （`ETOKI_TOKEN_ENCRYPTION_KEY`）、失効前の更新、GitHub が使い捨てにする refresh
  token のための利用者ごとの直列化、`Authenticator` を GitHub クライアントと
  `etoki.Options` の両方に同じものを渡す配線の約束、App のインストール経由で作成先の
  候補を引く別経路（`ModeApp`）。

## 決定

### GitHub は設定した classic PAT 1 本で叩く

- **ログインを設定しても、GitHub への読み書きは `ETOKI_GITHUB_TOKEN` で行う。** 認証の
  有無で分岐しない。PAT が無ければ、これまでどおり GitHub を使う機能だけが「設定され
  ていない」を返す（ADR 0008）。
- **classic PAT を勧める**（scope は `repo` と `project`）。個人アカウント所有の
  Projects v2 を扱えるのは classic だけ。org / リポジトリ所有の Project だけなら
  fine-grained PAT でも動くので、禁じはしない。

### ログインは誰であるかを決めるだけにする

- **`port.IdentityProvider.Exchange` は利用者だけを返す。** GitHub App の Web Flow で
  得たトークンは、持ち主を引く（`GET /user`）のに 1 回使って捨てる。`Refresh` と
  `port.Credentials` は外した。
- **トークンを保存しない。** `github_tokens` はマイグレーションで落とす。中身は
  使わなくなった生きたトークンで、残す理由が無い。戻せないが、要ればログインし直せば
  取れる。暗号化の鍵と `internal/secret` も外した。
- **セッションは今までどおり etoki が発行し、DB にはハッシュだけを置く**（ADR 0015 の
  この節は生きている）。認証基盤のトークンの寿命とは結びつけない。

### 継ぎ目は 2 つにする

`port.GitHubTokenSource` を外し、継ぎ目は「誰であるかを決める基盤」
（`IdentityProvider`）と「セッションの置き場所」（`SessionRepository`）の 2 つにする。
GitHub クライアントは固定の `Token` だけを受ける。

- **作成先の候補は PAT の持ち主が見えるリポジトリ（GraphQL）の 1 本にする。**
  `Config.Mode` と、インストール経由の REST（`ModeApp`）は外した。

## 採らなかった案

- **継ぎ目（`GitHubTokenSource`）だけ残し、実装を PAT 1 つにする。** 差分は小さいが、
  自分用の構成では差し替える先が無い。ADR 0015 自身が「どこからも型アサートされない
  インターフェースは、実装しても何も起きない」と書いた形になる。公開 API 面の約束を
  外す流れ（#493）とも逆向き。取り直しの要るトークン（App のインストールトークン
  など）が要る日に、そのとき戻す。
- **`ModeApp` を残して、App に戻す日に備える。** 戻すとしても自分用なら App として
  書くインストールトークンになり、候補の引き方（`/installation/repositories`）が
  今の `/user/installations` と違う。残しても備えにならない。
- **今のまま App のトークンで叩き、作成先を org の Project に限る。** コードは
  変えずに済むが、個人の Project に作れないまま、上に挙げた重いものを抱え続ける。

## 結果

- 個人アカウントの Projects v2 にも作れる（classic PAT の `project` スコープで）。
- 鍵の管理と入れ替え、失効前の更新、利用者ごとの直列化が無くなる。
- **作った draft issue は、誰がログインしていても PAT の持ち主の名前で作られる。**
  1 人で使う前提なので困らない。共有（ADR 0017）で招いた人も PAT の権限で作る
  ことになるが、共有は 2 人目がログインできない時点で使えず（ADR 0105）、外すのは
  #495。
- **ADR 0017 の「GitHub 側の権限を etoki に複製しない」は残る。** GitHub が作成の
  ときに見るのが、実行者の権限から PAT の権限に変わるだけで、手前にずれる判定を
  置かない理由は同じ。
- GitHub App は、ログインのためだけに作る。Projects の権限は要らない。
