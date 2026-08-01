SHELL := bash
.DEFAULT_GOAL := help

BIN_DIR := bin
BINARY  := $(BIN_DIR)/etoki
WEB_DIR := web
DB_PATH ?= etoki.db

.PHONY: help setup dev dev-api dev-web build build-api build-web \
        test test-go test-web lint lint-go lint-web fmt migrate clean

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
	air

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

migrate: ## マイグレーションを適用する
	ETOKI_DB_PATH=$(DB_PATH) go run ./cmd/etoki migrate

clean: ## 生成物を削除する
	rm -rf $(BIN_DIR) $(WEB_DIR)/dist $(WEB_DIR)/node_modules
	rm -f $(DB_PATH) $(DB_PATH)-shm $(DB_PATH)-wal
