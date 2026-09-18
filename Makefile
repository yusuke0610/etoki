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

# make try-fake で偽の GitHub / LLM を立てる先。
FAKE_ADDR ?= 127.0.0.1:8090

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

.PHONY: help setup dev dev-api dev-web build build-api build-web start try-fake \
        test test-go test-web test-scripts test-e2e lint lint-go lint-web lint-docs lint-fmt \
        lint-nix lint-actions lint-sh fmt \
        codegen codegen-go codegen-web migrate token-report clean reset-db

help: ## ターゲット一覧を表示する
	@echo "使い方: make <target>"
	@echo
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-13s\033[0m %s\n", $$1, $$2}'

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
	@# 鍵が要るのはサーバーだけなので、$(ENV_FILE) を読むのもサーバーを起動する
	@# ターゲット（dev-api と start）だけにする。migrate は ETOKI_DB_PATH しか
	@# 要らず、それは DB_PATH で渡している。
	$(LOAD_ENV) air

dev-web: ## フロントエンドのみ起動する（ホットリロード有効）
	cd $(WEB_DIR) && bun run dev

build: build-api build-web ## バイナリとフロントエンドのビルド成果物を生成する

start: build ## ビルド済みの成果物で起動する（dev サーバーを使わない）
	@# 配布する構成と同じ形で動かす。ETOKI_WEB_DIR を渡さないと画面を配らない
	@# ので（ADR 0032）、ここで渡す。ブラウザは :8080 側にいることになるため、
	@# GitHub App の Callback URL と ETOKI_PUBLIC_URL もそちらに合わせる。
	$(LOAD_ENV) ETOKI_WEB_DIR=$(WEB_DIR)/dist $(BINARY)

try-fake: build ## 偽の GitHub / LLM に向けて、ビルド済みの etoki を通しで動かす
	@# 手元の確認用で、make test にも CI にも入れない（ADR 0050）。
	@# DB は毎回作り直す。偽物の状態はメモリにしか無いので、DB を使い回すと
	@# 記録が指す draft issue が偽物の側に無く、更新が必ず失敗する。
	@# $(LOAD_ENV) は通さず、認証と鍵の変数は外す。.env や direnv の本物の鍵を
	@# 偽物へ送らないためと、App を設定していると認証の構成に入り、偽物では
	@# ログインできないため。
	@go build -o $(BIN_DIR)/etoki-fakeupstream ./cmd/etoki-fakeupstream
	@# 片付けを kill 0 より先に書く。kill 0 はこのシェル自身も落とす。
	@tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"; kill 0' EXIT INT TERM; \
	ETOKI_DB_PATH="$$tmp/etoki.db" $(BINARY) migrate || exit 1; \
	FAKE_ADDR="$(FAKE_ADDR)" $(BIN_DIR)/etoki-fakeupstream & \
	env -u ETOKI_GITHUB_APP_CLIENT_ID -u ETOKI_GITHUB_APP_CLIENT_SECRET \
		-u ETOKI_TOKEN_ENCRYPTION_KEY -u ETOKI_PUBLIC_URL -u ETOKI_LLM_API_KEY \
		ETOKI_DB_PATH="$$tmp/etoki.db" \
		ETOKI_WEB_DIR=$(WEB_DIR)/dist \
		ETOKI_LLM_BASE_URL="http://$(FAKE_ADDR)" \
		ETOKI_GITHUB_BASE_URL="http://$(FAKE_ADDR)" \
		ETOKI_GITHUB_TOKEN=fake \
		$(BINARY) & \
	wait

build-api:
	go build -o $(BINARY) ./cmd/etoki

build-web:
	cd $(WEB_DIR) && bun run build

test: test-go test-web test-scripts ## Go / フロントエンド / scripts のテストを実行する

test-go: ## Go のテストのみ実行する
	go test ./...

test-web: ## フロントエンドのテストのみ実行する
	cd $(WEB_DIR) && bun run test

test-scripts: ## scripts/ のテストのみ実行する
	@# vitest ではなく bun test を使う。scripts/ は web/ の外にあり、web の
	@# devDependencies（vitest）を前提にできない。bun は devShell に既にある。
	bun test scripts

test-e2e: ## Playwright で E2E テストを実行する（test には含めない）
	@# 実行のたびに web/e2e-output/screenshots/ が作り直される。UI を変えたときは
	@# ここの画像を報告に添える（CLAUDE.md の「報告にスクリーンショットを添える」）。
	cd $(WEB_DIR) && bun run test:e2e

lint: lint-go lint-web lint-docs lint-fmt lint-nix lint-actions lint-sh ## Go / フロントエンド / Markdown / Nix / Actions / シェルと整形を検査する

lint-go:
	golangci-lint run
	@# gofmt / goimports は formatters に登録してあり run では検査されない。
	@# 整形崩れが緑のまま通らないよう、差分が出たら落とす。
	golangci-lint fmt --diff

lint-web:
	cd $(WEB_DIR) && bun run lint

lint-docs:
	@# 対象と規則は .markdownlint-cli2.yaml にある。引数で glob を渡さないのは、
	@# 検査対象の定義がここと設定ファイルの 2 箇所に散るのを避けるため。
	@# 対象は prettier と同じリポジトリ全体。あちらが整形を、こちらが構造を見る。
	markdownlint-cli2

lint-fmt:
	@# リポジトリ全体を見る。web/ の中から呼ぶと docs/adr やルートの Markdown が
	@# 対象から外れる。除外は .prettierignore と .gitignore が決める。
	prettier --check .

lint-nix:
	@# nix flake check も同じ検査をするが、そちらは CI でしか回らない。手元で
	@# make lint だけ通したときに Nix の整形崩れを見落とさないよう、ここにも置く。
	nixfmt --check flake.nix

lint-actions:
	@# ワークフロー固有の検証。YAML の構文エラーと重複キーは prettier が落とす
	@# ので、ここが見るのは式やコンテキストの誤りと、run: の中のシェル。
	@# シェルを見るのは devShell の shellcheck で、actionlint が自動で拾う。
	actionlint

lint-sh:
	@# actionlint が拾うのはワークフローの run: だけ。スキルが持つスクリプトは
	@# make のどのターゲットからも呼ばれないので、ここで見ないと実行するまで
	@# 壊れたことに気づけない。shellcheck は actionlint のために devShell へ
	@# 既に入っている。
	shellcheck .claude/skills/*/*.sh

fmt: ## Go / フロントエンド / Markdown / Nix を整形する
	golangci-lint fmt
	nixfmt flake.nix
	prettier --write .

codegen: codegen-go codegen-web ## api/openapi.yaml から Go / TypeScript の型を再生成する

codegen-go:
	cd api && oapi-codegen --config oapi-codegen.yaml openapi.yaml

codegen-web:
	cd $(WEB_DIR) && bun run codegen

migrate: ## マイグレーションを適用する
	ETOKI_DB_PATH="$(DB_PATH)" go run ./cmd/etoki migrate

token-report: ## 直近のセッションのトークン消費の内訳を出す（docs/token-budget.md）
	@# lint には入れない。読むのは Claude Code が手元に残す transcript で、CI には
	@# 存在しない。検査ではなく、削る先を決めるための道具（#125）。
	@# SESSION はクォートして 1 個の引数として渡す。transcript のパスは
	@# ~/.claude/projects/<作業ディレクトリ> の下にあり、空白を含みうる。
	bun scripts/token-report.ts $(if $(SESSION),"$(SESSION)")

clean: ## 生成物を削除する（etoki.db には触らない）
	@# DB は生成物ではなく利用者のデータ。sync_runs / sync_items は作成の瞬間に
	@# 控えたもので取り直せない（ADR 0023）。ビルドをやり直すつもりの 1 コマンドで
	@# 消えないよう、消すのは reset-db に分けてある。
	rm -rf $(BIN_DIR) $(WEB_DIR)/dist $(WEB_DIR)/node_modules $(WEB_DIR)/e2e-output

reset-db: ## ボードと作成の記録（etoki.db）を消す。etoki を止めて CONFIRM=1 を付ける
	@# 名前と説明で「データを消す」と分かるだけでは足りない。補完や履歴から
	@# 呼ばれても消えないよう、明示の変数を要求する。
	@if [ -z "$(DB_PATH)" ]; then echo "DB_PATH が空です"; exit 1; fi
	@if [ "$(CONFIRM)" != "1" ]; then \
		echo "reset-db は $(DB_PATH) を消します。ボード・メンバー・作成の記録を含み、元に戻せません。"; \
		echo "GitHub に作った draft issue は残りますが、etoki のどこから作ったかは失われます。"; \
		echo "消すなら、etoki を止めてから: make reset-db CONFIRM=1"; \
		exit 1; \
	fi
	@# 接続が残っているうちは消さない。開いている側は消えたファイルを読み書きし
	@# 続け、そのあいだの書き込み（作成の記録を含む）は閉じた時点で失われる。
	@# WAL の接続は DB を一度読むと閉じるまで共有ロックを持ち続けるので、排他で
	@# 開けるかで残りが分かる（SQLite が最後の接続を閉じるときの判定と同じ）。
	@# 止めるのは、ロックで開けないときと、sqlite3 が動かず確かめられないとき
	@# （終了コード 2 以上）。SQL のエラー（1）では止めない。壊れた DB で止めると
	@# reset-db で消せなくなる。無いファイルを渡すと空の DB ができるので、ある
	@# ときだけ試す。確かめてから消すまでのあいだに開かれた接続は防げない。
	@# etoki を止めてから呼ぶ前提（README）の上での、取り違えへの歯止め。
	@if [ -e "$(DB_PATH)" ]; then \
		out=$$(sqlite3 -- "$(DB_PATH)" \
			'PRAGMA locking_mode=EXCLUSIVE; SELECT count(*) FROM sqlite_master;' 2>&1 >/dev/null); \
		rc=$$?; \
		if printf '%s\n' "$$out" | grep -q 'database is locked'; then \
			echo "$(DB_PATH) を開いている接続があるので、消さずに止めました。"; \
			echo "etoki（make dev / make start）や sqlite3 を止めてから、もう一度実行してください。"; \
			exit 1; \
		fi; \
		if [ "$$rc" -gt 1 ]; then \
			echo "sqlite3 で $(DB_PATH) を確かめられないので、消さずに止めました（終了コード $${rc}）。"; \
			[ -z "$$out" ] || printf '%s\n' "$$out"; \
			exit 1; \
		fi; \
	fi
	@# DB_PATH は上書きできるので、空白や glob を含んでも 1 つのパスとして渡す。
	@# 分割や展開を許すと、CONFIRM=1 で認めた範囲より広く消える。
	rm -f -- "$(DB_PATH)" "$(DB_PATH)-shm" "$(DB_PATH)-wal"

endif
