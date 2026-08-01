# etoki（絵解き）

**ブレストの絵を解いて、GitHub の設計に落とす。**

ホワイトボード上の自由なブレスト内容を Vision LLM に解釈させ、GitHub Projects v2 の
draft issue に変換するツールです。

## 中核となる思想

1. **ユーザーはブレスト中に構造を意識してはならない。** issue / epic といった概念を
   ブレストフェーズに持ち込まない。
2. **構造は座標から推測せず、LLM に解釈させる。** 付箋の空間的近接やコネクタから
   構造をルールベースで推測しない。
3. **システムは自動で判断せず、状態を見せて開発者に選ばせる。** 自動同期・自動更新・
   自動作成は行わない。

## 開発

[Nix](https://nixos.org/) があれば他に何もインストールする必要はありません。

```sh
nix develop      # 開発用シェルに入る（Go / Bun / SQLite / lint などが揃う）
make setup       # 依存関係の取得と DB 初期化
make dev         # バックエンドとフロントエンドを同時に起動
make help        # ターゲット一覧
```

### direnv（任意）

[direnv](https://direnv.net/) を使っているなら、一度許可するだけで
このディレクトリに `cd` したときに開発用シェルが有効になります。

```sh
direnv allow
```

キャッシュ用の nix-direnv は `.envrc` が flake から取り出すので、
別途インストールする必要はありません。direnv を使わない場合は
`nix develop` のままで構いません。

## ドキュメント

設計判断の記録は [`docs/adr/`](docs/adr/) にあります。

## ライセンス

[MIT](LICENSE)
