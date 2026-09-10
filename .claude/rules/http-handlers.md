---
paths:
  - "internal/httpapi/**"
---

# Gin ハンドラと境界の防御

`internal/httpapi/` を触るときの約束。3 状態判定などコアのデータフローは
`internal/CLAUDE.md` にあり、ここには書かない。

## 境界の DTO とエラー

- **境界の DTO は `internal/httpapi/apitypes/types.gen.go` の生成型を使う。**
  手で型を足さない。正本は `api/openapi.yaml`（ADR 0011、詳細は
  `api/CLAUDE.md`）。
- **エラー本文も `ErrorResponse` に揃える。** `gin.H{"error": ...}` を直に
  書かない。
- **sentinel → (status, code) の写し替えは `internal/httpapi/errors.go` の表
  1 つだけ**（ADR 0034）。`fail*` は表を引くだけにする。**ハンドラごとに分岐を
  戻さないこと。** エンドポイントごとに違ってよいのは、表に無かったときの既定
  （GitHub を叩く経路は 502、それ以外は 500）とログの出し分けだけ。
- **`code` は `errorJSON` の必須引数。** 任意にすると、足し忘れた経路だけが
  画面の対応表から漏れて静かに既定の文言に落ちる。sentinel を足したら表にも
  足す。`errors_test.go` が `usecase` と `port` のソースから `Err*` を数え直し、
  表に無ければ落とす。境界に出てこないものは同ファイルの `notMapped` に理由を
  書く。
- **`error` 本文は手掛かりであって利用者向けの文言ではない。** Go の内部文言
  （`etoki: ...`）を画面向けに書き換えない。画面は code から日本語を引く。
- **`GET /api/capabilities` は `Deps` の nil をそのまま返す**（ADR 0030）。
  押す前に「いまできないこと」を見せるための口で、**判定材料は各エンドポイントと
  同じ nil**。別の材料で組み立てると、案内と 503 が食い違う。
  `TestGetCapabilities_MatchesUnavailableEndpoints` がそこを見ている。
  **利用者ごとの権限は返さない。** そちらはボード単位（ADR 0017、
  `internal/CLAUDE.md`）。
- **`*_not_configured` を名乗れるのは、capabilities が材料にしている nil だけ。**
  `etoki.New` が必ず渡す依存（`Access`）が nil なのは設定の不足ではなく配線の
  不具合なので、ログを残して 500 に落とす。ここで共有未設定を名乗ると、
  「共有は使える」と案内した直後に「共有は未設定」と返る組み合わせができる。
- **ハンドラのテストは、リクエストに Host を入れてから投げる。**
  `httptest.NewRequest` の既定は `example.com` なので、入れ忘れると Origin 検証で
  403 になる。**入れる場所はリクエストを組み立てた場所。** `do`
  （`router_test.go`）を通す経路はヘルパーが入れているので、呼び出し側で重ねない。
  リクエストを直に組む経路（`auth_test.go` / `member_test.go`）は、その場で
  `req.Host = loopbackHost` を書く。
- **画面の配信は `NoRoute` に置く**（ADR 0032）。ルートに当たらなかったものだけを
  見る位置なので、静的ファイルが API のパスを覆わない。**`/api` と `/healthz` は
  そこで最初に弾く。** 通すと API の打ち間違いが 404 ではなく 200 + HTML で返り、
  フロント側では「JSON のはずが HTML」という読みにくい失敗になる。
  **未知のパスに `index.html` を返す SPA の fallback は持たない。** フロントに
  client-side routing が無く、置くと打ち間違えた URL がすべて 200 になる。
  配信は `requireAuth` の**外**に置く。ログイン画面そのものがここから配られる。

## cross-site リクエストの拒否（ADR 0013）

**バインド先を絞るだけでは足りない。** ループバックにバインドしていても、
ブラウザ経由なら外部サイトのページから API を叩ける（CSRF / DNS
リバインディング）。`internal/httpapi/origin.go` が Host と Origin を検証して
弾いている。ここを触るときの約束:

- **`Origin` が無いリクエストは通す。** curl やスクリプトが該当する。ブラウザは
  cross-origin の POST に必ず `Origin` を付け、攻撃者はそれを省略・偽装できない。
  ここを塞いでも守れるものは増えず、CLI からの利用だけが壊れる。
- **副作用を持つ GET は `/api/auth/callback` だけ。** これは例外で、Origin では
  なく `state` が守っている。**これ以上増やさないこと。** 増やすなら ADR 0013 の
  判断ごと見直す。送り出し側の `/api/auth/login` を POST にしてあるのも、
  state の発行を Origin 検証の内側に置くため。
- **ループバック判定でポートを見ない。** `make dev` では Vite の dev サーバーが
  `Host` と `Origin` を書き換えずに転送するため、`:8080` ではなく `:5173` が
  届く。リッスンポートに絞ると開発時に落ちる。
