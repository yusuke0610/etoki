# api/ の規約

HTTP 契約を触るときの約束。全体の規約はリポジトリルートの `CLAUDE.md`。

## OpenAPI が正本

**`api/openapi.yaml` が境界の DTO の唯一の定義。Go も TypeScript も生成物。**
手で型を足すと、そこだけ二重定義に戻る（ADR 0011）。

| 生成物 | 生成元 | 使う側 |
| --- | --- | --- |
| `internal/httpapi/apitypes/types.gen.go` | `oapi-codegen` | Gin ハンドラ |
| `web/src/api/generated.ts` | `openapi-typescript` | `web/src/api/types.ts` 経由でフロント全体 |

- **契約を変えたら `make codegen` を実行し、生成物を同じコミットに含める。**
  忘れると CI の codegen drift で落ちる。
- 生成物は手で編集しない。次の生成で消える。
- エラー本文も `ErrorResponse` に揃える。ハンドラに `gin.H{"error": ...}` を
  直に書かない。
- フロントは `web/src/api/types.ts` の名前を import する。**独自の別名を
  付けない。** 名前が食い違うと、契約を直したときに追随先を機械的に辿れなくなる。
- E2E のモック応答も生成型で縛ってある（`web/e2e/helpers/api.ts`）。
  モックだけが古い契約のまま緑になるのを防ぐため。

## 生成器のバージョン

- **生成器のバージョンで生成物の形が変わる。** `oapi-codegen` は `flake.lock`
  が、`openapi-typescript` は `bun.lock` が握っている。`nix flake update` や
  `bun update` のコミットには `make codegen` の結果も含める。
- **`make codegen` は必ず devShell の中で実行する。** `types.gen.go` の冒頭
  バナーには生成器のバージョン文字列が埋まる。しかもこれは「どのバージョンか」
  ではなく「どうビルドされたか」で変わる。nixpkgs は
  `-X main.noVCSVersionOverride=2.5.1` を渡すので `2.5.1` になるが、
  `go run ...@v2.5.1` で入れた同じバージョンは `v2.5.1` と出る。devShell の外で
  生成すると、中身が同じでも codegen drift で落ちる。`make` は devShell の外から
  呼ばれると自分を包み直すので、`make codegen` で呼ぶかぎりは満たされる。
