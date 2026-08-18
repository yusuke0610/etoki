{
  description = "etoki — ブレストの絵を解いて、GitHub の設計に落とす";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      # flake-utils を入力に足さずに済ませるための最小ヘルパー。
      # 依存を増やさない方が flake.lock の更新も追いやすい。
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          name = "etoki";

          # Makefile が「devShell の中か」を判定するための印。shellHook ではなく
          # derivation の環境に置く。nix develop も nix-direnv も derivation の
          # 環境変数は必ず渡すが、shellHook が走るかは経路によって変わるため。
          ETOKI_DEVSHELL = "1";

          # バージョンは二重に固定している:
          #   - どの Go / Bun が入るかは flake.lock の nixpkgs リビジョンが決める
          #   - Go は go_1_26 と系列を明示し、nix flake update が想定外の
          #     メジャー移行を静かに持ち込まないようにする
          packages = with pkgs; [
            go_1_26
            gopls
            golangci-lint
            air
            bun
            sqlite
            gnumake
            # api/openapi.yaml から Go の型を生成する。go.mod の tool ディレクティブ
            # ではなくここに置くのは、生成器を require に足すと kin-openapi 一式が
            # アプリの依存グラフに乗り、x/net などの共有依存まで引き上げられて
            # しまうため。go.mod は動かすものの依存だけに保つ（ADR 0011）。
            oapi-codegen
            # Markdown の検査と、リポジトリ全体の整形。web/package.json では
            # なくここに置くのは、対象が docs/adr やルートの CLAUDE.md まで及ぶ
            # ので、web/ の依存として持つと「フロントエンドを触らない変更」で
            # 道具が入らないことになる。
            markdownlint-cli2
            prettier
          ];

          shellHook = ''
            # go.mod の go ディレクティブが nixpkgs の Go より新しいと、Go は
            # ツールチェーンを勝手にダウンロードしようとする。それをすると
            # Nix によるバージョン固定が意味を失うので、必ず local に固定する。
            export GOTOOLCHAIN=local

            export ETOKI_DB_PATH="''${ETOKI_DB_PATH:-$PWD/etoki.db}"

            # Playwright にブラウザを自前でダウンロードさせない。npm 側の取得は
            # flake.lock の外側で起きるため、固定が効かなくなる。web の
            # @playwright/test は、ここで渡すブラウザ（playwright-driver）と
            # 同じバージョンに固定しておく必要がある（ADR 0012）。
            export PLAYWRIGHT_BROWSERS_PATH="${pkgs.playwright-driver.browsers}"
            export PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1

            # 対話的に入ったときだけ挨拶する。Makefile は devShell の外から
            # 呼ばれるたびに nix develop --command で包み直すので、無条件に
            # 出すと make のたびにこの 1 行が混ざる。
            if [[ $- == *i* ]]; then
              echo "etoki devShell: $(go version | cut -d' ' -f3), bun $(bun --version)"
            fi
          '';
        };
      });

      # .envrc から参照する。nix-direnv を別途グローバルに入れさせるのではなく
      # ここから取り出すことで、バージョンが flake.lock に固定される。
      packages = forAllSystems (pkgs: {
        inherit (pkgs) nix-direnv;
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt);

      # checks は意図的に Nix コードのフォーマット検査だけに絞っている。
      # Go / フロントエンドのビルドを derivation 化すると vendorHash と
      # Bun のオフライン取得の保守コストが高くつくため、そちらは Makefile と
      # CI に任せる。判断の経緯は docs/adr/0002-toolchain.md を参照。
      checks = forAllSystems (pkgs: {
        nix-fmt =
          pkgs.runCommand "check-nix-fmt"
            {
              nativeBuildInputs = [ pkgs.nixfmt ];
            }
            ''
              nixfmt --check ${./flake.nix}
              touch $out
            '';
      });
    };
}
