---
paths:
  # 配線そのもの。
  - "cmd/etoki/**"
  # NewAuthenticator と Options.Auth。etoki.New の中で作り始めると
  # 「1 つだけ」が崩れる。
  - "etoki.go"
  # 更新の直列化の実体（利用者ごとの Mutex）。AuthService のフィールドなので、
  # 「1 つだけ作る」はここの作りに依っている。
  - "internal/usecase/auth.go"
  # TokenSource と Mode を受ける側。Mode をアダプタの中で決め始めると
  # 「判断するのは cmd/etoki だけ」が崩れる。
  - "internal/adapter/github/**"
---

# 認証の配線

認証の継ぎ目と前提はルートの `CLAUDE.md` の「認証について」。ここにあるのは、
それを `cmd/etoki` で配線するときの約束。**破れるのは配線の側だけではない**ので、
受け取る側（`etoki.go`・`AuthService`・GitHub アダプタ）を触るときにも読まれる
ようにしてある。

- **`Authenticator` は 1 つだけ作る。** GitHub クライアントの `TokenSource` と
  `etoki.Options.Auth` に同じものを渡す。2 つ作るとトークン更新の直列化が効かず、
  GitHub が使い捨てにした refresh token を掴む。
- **リポジトリ一覧は `github.Config.Mode` で分岐する。** GitHub App では
  インストール経由の REST、PAT では GraphQL。「使えるリポジトリ」の定義が
  GitHub 側で違うため。判断するのは `cmd/etoki` だけ。
- **OAuth を設定したら PAT は無視する。** フォールバックにすると作成の主体が
  リクエストごとに変わり、誰が作ったのか追えなくなる。
