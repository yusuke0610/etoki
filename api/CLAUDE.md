# api/ の規約

HTTP 契約を触るときの約束。全体の規約はリポジトリルートの `CLAUDE.md`。

## OpenAPI が正本

正本であること、契約を変えたら `make codegen` の生成物を同じコミットに含める
こと、生成物を手で編集しないことは、ルートの `CLAUDE.md`「HTTP 契約は OpenAPI が
正本」（ADR 0011）。ここに置くのは、生成物がどこに出てどう使われるかと、生成器の
扱い。

| 生成物                                   | 生成元               | 使う側                                    |
| ---------------------------------------- | -------------------- | ----------------------------------------- |
| `internal/httpapi/apitypes/types.gen.go` | `oapi-codegen`       | Gin ハンドラ                              |
| `web/src/api/generated.ts`               | `openapi-typescript` | `web/src/api/types.ts` 経由でフロント全体 |

生成型の使い方の約束は、使う側を触るときに読まれる場所にある。

- Go のハンドラ（`ErrorResponse` に揃える、`gin.H` を書かない）:
  `.claude/rules/http-handlers.md`
- フロント（`web/src/api/types.ts` の名前を import する）と E2E のモック
  （`Reply<T>`）: `web/CLAUDE.md`
- 契約の面をまたいで追いつかない形: `.claude/rules/api-contract.md`

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
