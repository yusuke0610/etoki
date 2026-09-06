---
paths:
  - "api/openapi.yaml"
  - "internal/httpapi/**"
  - "web/src/api/**"
---

# 契約の面をまたぐ一貫性

`api/openapi.yaml` が正本で Go と TypeScript は生成物、という分担は
`api/CLAUDE.md`（ADR 0011）。ここに集めたのは、**正本を直したのに面のどこかが
追いつかず、足したはずの経路が生成型から見えないまま緑になる**形。

## エラーの応答も本文を持つ

- **エラーの応答に `application/json` の `ErrorResponse` を必ず置く。** 本文の
  無い応答は生成型で `content?: never` になり、**フロントは `code` を読めない。**
  画面は `code` で日本語を引く（ADR 0034）ので、code を足したのに契約に本文が
  無いと、その分岐だけどこからも到達できない状態が残る（#108）。
- **歯止めに当たった失敗も、契約の code に写す。** `http.MaxBytesReader` が返す
  `*http.MaxBytesError` は `ShouldBindJSON` から来るので、`errors.As` で拾って
  対応する sentinel に写す。写さないと、**同じ「大きすぎる」がボディの大きさ
  しだいで 400 と 413 に割れ**、画面が同じ原因を 2 通りに案内する（#108）。
  歯止めの倍率の取り方は `validation-boundaries.md`。

## やらないこと

- **既知の `code` を返す経路で、サーバーの `error` 本文を固定文に差し替えない。**
  「内部の数値が画面に出る」という指摘で提案されるが、画面が前に出すのは
  `ERROR_MESSAGES` から引いた日本語で、サーバーの本文は `detail` に畳まれる
  （ADR 0034）。本文が前に出るのは**知らない `code` のときだけ**。差し替えると、
  同じ形で本文を返している既存の経路と割れる（#108）。
