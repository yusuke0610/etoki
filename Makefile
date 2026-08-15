SHELL := bash
.DEFAULT_GOAL := help

# devShell の外から呼ばれたら、nix develop の中で make をやり直す。
#
# devShell に入り忘れても道具の無いまま走らないようにするため。README と
# CLAUDE.md の「すべて nix develop の中で実行する」を、書き手の記憶ではなく
# Makefile 側で満たす。
#
# 印は flake.nix の devShell が渡す ETOKI_DEVSHELL を見る。IN_NIX_SHELL では
# 判定しない。あれは「何かの nix shell の中」としか言わないので、別プロジェクト
# の shell から呼ぶと包み直しを飛ばしてしまう。direnv を使っているなら
# devShell がすでに有効なので、この分岐には来ない。
ifndef ETOKI_DEVSHELL

# 目標はまとめて 1 回だけ包む。ターゲットごとに包むと、dev のように $(MAKE) を
# 呼ぶターゲットで nix develop が入れ子になる。
NIX_GOALS := $(or $(MAKECMDGOALS),$(.DEFAULT_GOAL))

.PHONY: $(NIX_GOALS) nix-develop

$(NIX_GOALS): nix-develop
	@:

nix-develop:
	@command -v nix >/dev/null 2>&1 || { \
		echo "nix が見つかりません。https://nixos.org/download を参照してください。" >&2; \
		exit 1; \
	}
	@# 先頭の + は -n でもこの行を実行させる指定。飛ばすと make -n が
	@# 「nix develop を呼ぶ」としか出さず、中で何が走るのか見えない。-n 自体は
	@# MAKEFLAGS で中の make に渡るので、実際に走るのは包み直しまで。
	@# warn-dirty は切る。作業中は常に uncommitted な木で走るので、残すと make の
	@# たびに同じ警告が出て本当の警告が埋もれる。--no-print-directory は、
	@# MAKELEVEL が上がって Entering/Leaving directory が出るのを止める。
	@# $(MAKE) とは書かない。外側の make（macOS なら 3.81）を呼び直すことになり、
	@# devShell が固定している gnumake が使われない。make migrate DB_PATH=… の
	@# ような指定は MAKEFLAGS が環境変数として引き継がれるので書き足さずに届く。
	+nix develop --option warn-dirty false \
		--command make --no-print-directory $(NIX_GOALS)

else

BIN_DIR := bin
BINARY  := $(BIN_DIR)/etoki
WEB_DIR := web
DB_PATH ?= etoki.db

# ローカルで動かすときの鍵の置き場（gitignore 済み）。無くてもよい。
#
# make の include ではなく shell の `.` で読む。include すると `$` を含む値が
# make の変数展開に食われ、書き手側から逃がす手段が無い。shell なら
# クォートの規則が普段 export を書くときと同じになる。
#
# 読むのは recipe の中だけで、Go 側は環境変数しか見ない。バイナリに設定
# ファイルを読ませると、main を写して使う経路（cmd/etoki の冒頭）にその
# 前提まで付いていく。
ENV_FILE := .env
LOAD_ENV := set -a; [ -f $(ENV_FILE) ] && . ./$(ENV_FILE); set +a;

.PHONY: help setup dev dev-api dev-web build build-api build-web \
        test test-go test-web test-e2e lint lint-go lint-web fmt \
        codegen codegen-go codegen-web migrate clean

help: ## ターゲット一覧を表示する
	@echo "使い方: make <target>"
	@echo
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-11s\033[0m %s\n", $$1, $$2}'

setup: ## 依存関係を取得し DB を初期化する
	go mod download
	cd $(WEB_DIR) && bun install
	$(MAKE) migrate

dev: ## バックエンドとフロントエンドを同時に起動する
	@# `kill 0` はプロセスグループ全体を落とす。Ctrl-C や異常終了で
	@# air と bun のどちらかが取り残されるのを防ぐために trap で囲んでいる。
	@trap 'kill 0' EXIT INT TERM; \
	$(MAKE) dev-api & \
	$(MAKE) dev-web & \
	wait

dev-api: ## バックエンドのみ起動する（ホットリロード有効）
	@# 鍵が要るのはサーバーだけなので、$(ENV_FILE) を読むのもここだけにする。
	@# migrate は ETOKI_DB_PATH しか要らず、それは DB_PATH で渡している。
	$(LOAD_ENV) air

dev-web: ## フロントエンドのみ起動する（ホットリロード有効）
	cd $(WEB_DIR) && bun run dev

build: build-api build-web ## バイナリとフロントエンドのビルド成果物を生成する

build-api:
	go build -o $(BINARY) ./cmd/etoki

build-web:
	cd $(WEB_DIR) && bun run build

test: test-go test-web ## Go とフロントエンドのテストを実行する

test-go: ## Go のテストのみ実行する
	go test ./...

test-web: ## フロントエンドのテストのみ実行する
	cd $(WEB_DIR) && bun run test

test-e2e: ## Playwright で E2E テストを実行する（test には含めない）
	@# 実行のたびに web/e2e-output/screenshots/ が作り直される。UI を変えたときは
	@# ここの画像を報告に添える（CLAUDE.md の「報告にスクリーンショットを添える」）。
	cd $(WEB_DIR) && bun run test:e2e

lint: lint-go lint-web ## golangci-lint とフロントエンドの lint / 整形検査を実行する

lint-go:
	golangci-lint run
	@# gofmt / goimports は formatters に登録してあり run では検査されない。
	@# 整形崩れが緑のまま通らないよう、差分が出たら落とす。
	golangci-lint fmt --diff

lint-web:
	cd $(WEB_DIR) && bun run lint

fmt: ## Go / Nix / フロントエンドをフォーマットする
	golangci-lint fmt
	nix fmt -- flake.nix
	cd $(WEB_DIR) && bun run fmt

codegen: codegen-go codegen-web ## api/openapi.yaml から Go / TypeScript の型を再生成する

codegen-go:
	cd api && oapi-codegen --config oapi-codegen.yaml openapi.yaml

codegen-web:
	cd $(WEB_DIR) && bun run codegen

migrate: ## マイグレーションを適用する
	ETOKI_DB_PATH=$(DB_PATH) go run ./cmd/etoki migrate

clean: ## 生成物を削除する
	rm -rf $(BIN_DIR) $(WEB_DIR)/dist $(WEB_DIR)/node_modules $(WEB_DIR)/e2e-output
	rm -f $(DB_PATH) $(DB_PATH)-shm $(DB_PATH)-wal

endif
