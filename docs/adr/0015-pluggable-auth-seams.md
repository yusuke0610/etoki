# 0015. 認証は 3 つの継ぎ目で受け、GitHub App を既定実装にする

- ステータス: 採用
- 日付: 2026-08-08

## 文脈

GitHub の資格情報は `ETOKI_GITHUB_TOKEN` の PAT 1 本で、利用者が手で貼っていた。
これを GitHub の画面でログインさせる形に変えたい。

素直に作ると「etoki は GitHub でログインするツール」になる。しかし etoki の
公開面は、外部リポジトリが `port` を実装して差し込めることを前提に切ってある
（ADR 0001 / 0005 / 0008）。認証だけを固定すると、その前提がここで途切れる。

継ぎ目の切り方には落とし穴がある。**「認証」を 1 つのインターフェースに
まとめると、GitHub の形しか差せなくなる。** 認証基盤を Okta に替えたい人は、
同時に GitHub を叩く手段まで失う。この 2 つは別の関心事である。

- **誰であるか**を決めるのは認証基盤（GitHub / OIDC / SAML / リバースプロキシ）
- **GitHub をどのトークンで叩くか**は、その基盤とは独立に決まる

「Okta で認証し、GitHub へはサービス用の資格情報で書く」は現実的な構成であり、
1 つにまとめると表現できない。

もう 1 つの分岐は、GitHub 側で OAuth App と GitHub App のどちらを使うか。

| | OAuth App | GitHub App |
| --- | --- | --- |
| 権限の粒度 | 粗い。リポジトリ一覧に `repo`（全リポジトリの読み書き）が要る | リポジトリ単位・権限単位。`Metadata: Read-only` + `Projects: Read and write` で足りる |
| トークン | 無期限 | 既定で 8 時間、`refresh_token` で更新 |
| 「使えるリポジトリ」 | 利用者が見えるもの全部 | アプリをインストールしたものだけ |

## 決定

### 継ぎ目は 3 つに割る

| 何を差し替えるか | 継ぎ目 | 既定実装 |
| --- | --- | --- |
| 誰であるかを決める基盤 | `port.IdentityProvider` | GitHub App の Web Flow |
| GitHub を叩くトークンの出どころ | `port.GitHubTokenSource` | セッションの利用者のトークン |
| セッションの置き場所 | `port.SessionRepository` | SQLite |

`etoki.Options` に足すのは `Auth` の 1 つだけで、**nil でよい。** nil なら
認証しない。LLM と GitHub が nil でも起動するのと同じ扱い（ADR 0008）。

`IdentityProvider` と `SessionRepository` を直接受け取らず、組み立て済みの
`Authenticator` を受け取るのは、**同じものを GitHub クライアントの
`TokenSource` にも渡す必要があるため。** etoki.New の中で作ると呼び出し側から
取れず、2 つ作るとトークン更新の直列化（後述）が効かなくなる。

リバースプロキシや IAP のように、ヘッダだけで利用者が決まる基盤は
`IdentityProvider` の 2 段（送り出し → 引き換え）に当てはまらない。**いまは
実装しない。** 必要になったら別のインターフェースとして足す。継ぎ目を 3 つに
割ってあるので、そのとき触るのは「誰であるか」の 1 つだけで済み、GitHub を
叩く側は変わらない。

使う当てのないインターフェースを先に置くことはしない。**どこからも型アサート
されないインターフェースは、実装しても何も起きない。** 守れない約束を公開面に
出すことになる。

**利用者は `context.Context` に載せて運ぶ。** `port.GitHubClient` の
シグネチャを 1 つも変えずに済む。出入口（`port.ContextWithUserID` /
`port.UserIDFromContext`）を `port/` に置くのは、外部リポジトリが
`GitHubTokenSource` を自前実装するときに読む必要があり、`internal/` は
import できないため。

**載せるのは etoki 側の利用者 ID だけ。** `Identity` ごと載せる案は採らない。
`Identity.Subject` は認証基盤の ID であって `users.id` ではなく、トークンを
引くのに要るのは後者。表示名まで運ぶ出入口を公開面に置いても、読む側が現れない
（上と同じ理由）。HTTP 層で表示名が要る場面は gin の context で足りている。

`github.Client` は `Config.TokenSource` で受ける。ADR 0008 が LLM 側で採った
RoundTripper 方式は採らない。**トークンの出どころが型に出るぶん追いやすい。**
LLM 側は「wire format は同じで認証だけ違う基盤に載せ替える」ための継ぎ目で、
リクエストごとに主体が変わる話ではなかった。ここは要件が違う。

### GitHub 側は GitHub App にする

**PAT に求めている粒度（repo の read と Projects の read/write）を、そのまま
要求できるのは GitHub App だけ。** OAuth App では `repo` を取ることになり、
利用者の全リポジトリへの書き込み権を etoki が預かる。ローカル単一ユーザーなら
許容範囲でも、マルチユーザー化（ADR 0016 予定）でそのまま破綻する。

トークンの失効は受け入れる。`refresh_token` を保存し、失効間際に更新する。

### OAuth を設定したら PAT は無視する

`ETOKI_GITHUB_TOKEN` は残すが、**OAuth を設定しているときは使わない。**
未ログインは 401 にする。

フォールバックにすると、作成の主体がリクエストごとに変わる。「この draft issue
は誰のトークンで作られたか」が追えなくなり、`sync_runs` の記録の意味が薄れる。
マルチユーザー化すれば成立しなくなる規則を、いま入れる理由がない。

### 資格情報は暗号化して保存する

`ETOKI_TOKEN_ENCRYPTION_KEY`（32 バイト）で AES-256-GCM。**鍵が無ければ
起動時に落とす。** 黙って平文に落ちない。

セッション token は cookie に平文、DB には SHA-256 だけ置く。DB が漏れても
生きたセッションにはならない。

## 結果

- 認証基盤を替えても GitHub は叩ける。逆も同じ。
- **etoki は認証方式の一覧を持たない。** 新しい基盤が増えても etoki は変わらない
  （ADR 0008 と同じ性質）。
- **リポジトリ一覧の実装が変わる。** GitHub App の user-to-server トークンで
  `viewer.repositories`（GraphQL）が返す範囲は保証されない。インストール経由の
  REST（`/user/installations/{id}/repositories`）に切り替える。回避策ではなく、
  GitHub App における「使えるリポジトリ」の定義そのもの。PAT モードは GraphQL の
  ままなので、アダプタは `Config.Mode` で分岐する。**判断するのは `cmd/etoki`**
  であり、アダプタに推測させない。
  - 副産物として画面が正しくなる。候補が「利用者が etoki に許可したリポジトリ」
    だけになり、選んだのに Projects を作れない、が起きない。
- **トークン更新を利用者ごとに直列化する必要がある。** GitHub は refresh token を
  使い捨てにするので、並行する 2 本が同時に更新すると片方が無効なトークンを掴む。
- 「Expire user authorization tokens」を切った App では `expires_in` /
  `refresh_token` が返らない。**両方扱う。** 失効情報が無ければ更新しない。
- **ADR 0013 の前提を 1 つ崩す。** 詳細は下記。

### ADR 0013（Origin ガード）との関係

`internal/httpapi/origin.go` は「etoki の GET はすべて副作用を持たない」ことを
根拠に、Origin の無い GET を通していた。**OAuth のコールバックは副作用を持つ
GET** であり、これを初めて破る。

| ルート | メソッド | 何が守るか |
| --- | --- | --- |
| `POST /api/auth/login` | POST | state の発行は書き込みなので **GET にしない**。Origin ガードが効く |
| `GET /api/auth/callback` | GET | 認証基盤からのトップレベル遷移なので GET 以外にできない。**`state` が守る**。サーバー発行・単回使用・期限つきで、攻撃者は有効な値を用意できない |

コールバックが失敗したとき（state が古い、code を使い切った）は **JSON では
なく画面へリダイレクトする。** ブラウザのトップレベル遷移に JSON を返すと、
利用者は生のエラー本文を見ることになる。戻せばログイン画面が出て、やり直しの
導線がそのまま繋がる。`code` / `state` が付いていない場合だけは 400 にする。
それは認可からの遷移ではなく、誰かが直接叩いた場合だから。

**Origin ガードの実装は変えない。** コールバックには `Origin` が付かず、`Host` は
自分自身なので、いまのままで通る。変えるのは `origin.go` の doc コメントで、
「GET に副作用は無い」という記述を「コールバックだけが例外で、そこは state が
守っている」に訂正する。偽になった前提を残すと、次に読む人が誤って判断する。
