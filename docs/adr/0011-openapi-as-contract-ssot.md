# 0011: HTTP 契約の正本を OpenAPI に置き、Go と TypeScript の型を生成する

ステータス: 採用

## 文脈

境界の DTO が 2 箇所に手書きされていた。`internal/httpapi` の Go の struct と、
`web/src/api/boards.ts` の TypeScript の type である。両者は同じ JSON を表すが、
一致を保っているのは規律だけだった。

片方だけ直しても Go も TypeScript も型検査を通り、テストも緑のまま通る。実際に
壊れるのはブラウザで叩いたときで、そこまで誰も気づけない。注釈の判定規則が Go と
TypeScript の 2 箇所にあるのと同じ形の危険が、境界の DTO にもあった。

DevForge では FastAPI が実行時に OpenAPI を吐くため、backend のスキーマを正本に
できる。Gin にはこれに相当する仕組みが無い。Go の struct から仕様を起こすには
コメント注釈を足す道具（swaggo など）が要り、注釈と実装がまた二重になる。

## 決定

**`api/openapi.yaml` を HTTP 契約の正本とし、Go と TypeScript の両方をそこから
生成する。** どちらか一方を正本にはしない。Gin 側を正本にできない以上、片方だけ
生成物にすると、生成されない側が結局は手書きの二重定義として残る。

- Go: `oapi-codegen` で `internal/httpapi/apitypes/types.gen.go` を生成し、
  ハンドラはこの型だけを読み書きする。エラー本文も `gin.H` をやめて
  `ErrorResponse` に揃える。契約に無いキーが混ざったことに気づけないため。
- TypeScript: `openapi-typescript` で `web/src/api/generated.ts` を生成し、
  `web/src/api/types.ts` がスキーマ名のまま再エクスポートする。呼び出し側は
  この名前を使い、独自の別名を付けない。名前が食い違うと、契約を直したときに
  追随すべき箇所を機械的に辿れなくなる。
- 生成物は両方ともコミットする。`make codegen` で再生成し、CI が再生成して
  差分が出たら落とす。

生成するのはスキーマの型だけで、サーバーの雛形は生成しない。ルーティングは
いま `router.go` を読めば分かる。潰したいのは契約の二重管理であって、
ルーティングの記述ではない。

生成器のバージョンは devShell（`flake.lock`）が固定する。`go.mod` の `tool`
ディレクティブでも固定できるが、`oapi-codegen` を require に足すと kin-openapi
一式がアプリの依存グラフに乗り、`x/net` や `x/crypto` といった共有依存まで
引き上げられる。動かすものの依存だけを `go.mod` に置くという方針を優先した
（ADR 0002 の「devShell に開発に必要なツールをすべて入れる」に沿う）。

仕様は OpenAPI 3.0.3 で書く。3.1 でも生成はできるが `oapi-codegen` が未対応の
旨を警告する。警告を常時出しておくと、本当に見るべき警告が埋もれる。

## 結果

- 契約を変えるときは `api/openapi.yaml` を直し、`make codegen` を実行して生成物を
  同じコミットに含める。忘れると CI の codegen drift で落ちる。
- フロントが古い形のレスポンスを期待していると、`tsc` が落ちるようになった。
  以前はブラウザで踏むまで分からなかった。
- 生成物の形は生成器のバージョンに依存する。`nix flake update` が
  `oapi-codegen` や `openapi-typescript` を動かすと差分が出るため、更新の
  コミットには再生成も含める必要がある。
