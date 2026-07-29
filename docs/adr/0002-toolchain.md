# 0002. 開発ツールチェーンは flake.nix と Makefile に限定する

- ステータス: 採用
- 日付: 2026-07-29

## 文脈

「グローバルに何かをインストールする手順を README に書かない」ことが要件である。
また `nix flake check` が通る状態を保つことも要件に含まれる。

一方で、`checks` に Go とフロントエンドのビルドを含めようとすると次の負担が生じる。

- Go: `buildGoModule` の `vendorHash` を依存更新のたびに手で更新する必要がある。
- Bun: Nix のビルドサンドボックスはネットワークを遮断するため、`bun install` を
  そのままは実行できず、依存を固定入力として取り込む仕組みを別途作る必要がある。

## 決定

- `nix develop` の devShell に開発に必要なツールをすべて入れる。
- `checks` は Nix コードのフォーマット検査だけに絞る。Go とフロントエンドの
  lint / test / build は Makefile 経由で実行し、CI でも同じ Makefile を叩く。
- バージョン固定は二重に行う。
  - 「どのバージョンが入るか」は `flake.lock` の nixpkgs リビジョンが決める。
  - Go は `go_1_26` と系列を明示し、`nix flake update` が想定外のメジャー移行を
    静かに持ち込まないようにする。Bun は nixpkgs 側にバージョン別 attribute が
    ないため、`flake.lock` による固定のみとなる。
- devShell で `GOTOOLCHAIN=local` を設定する。`go.mod` の go ディレクティブが
  nixpkgs の Go より新しいと Go はツールチェーンを自動ダウンロードしようとし、
  Nix によるバージョン固定が意味を失うため。

## 結果

- `nix flake check` は速く、壊れにくい。ただし「Nix だけでプロジェクト全体が
  ビルドできる」性質は得られない。成果物を Nix パッケージとして配布したくなった
  時点で、この判断を見直す必要がある。
- CI はローカルとまったく同じ `make` ターゲットを通る。CI 専用のセットアップ手順を
  持たないため、環境差による失敗が devShell の内側に閉じる。
