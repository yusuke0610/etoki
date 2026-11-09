#!/usr/bin/env bash
# 偽の GitHub / LLM と、それに向けたビルド済みの etoki を一時 DB で起動する。
#
# 呼ぶのは Makefile の fake-stack だけで、そこを make try-fake（人が画面を触る、
# ADR 0050）と make test-e2e-stack の Playwright（web/playwright.stack.config.ts、
# ADR 0097）が使う。**起動の手順をここ 1 か所に置く。** 2 か所に持つと、下の
# env -u で外す変数の一覧がずれ、片方だけ本物の鍵を偽物へ送る。
#
# 受け取るもの（どれも Makefile が渡す。既定を持たないのは、置き場所とポートの
# 正本を Makefile に残すため）:
#   BIN_DIR      etoki と etoki-fakeupstream のバイナリがあるディレクトリ
#   WEB_DIST     ビルド済みの画面（ETOKI_WEB_DIR に渡す）
#   ETOKI_ADDR   etoki を立てるアドレス（host:port）。etoki 自身もこれを読む
#   FAKE_ADDR    偽の上流を立てるアドレス（host:port）
set -uo pipefail

: "${BIN_DIR:?BIN_DIR が要る}" "${WEB_DIST:?WEB_DIST が要る}"
: "${ETOKI_ADDR:?ETOKI_ADDR が要る}" "${FAKE_ADDR:?FAKE_ADDR が要る}"

for f in "$BIN_DIR/etoki" "$BIN_DIR/etoki-fakeupstream" "$WEB_DIST/index.html"; do
	if [ ! -e "$f" ]; then
		echo "fake-stack: $f がありません。make build build-fakeupstream を先に実行してください" >&2
		exit 1
	fi
done

# host:port に何かが応答しているか。bash の /dev/tcp で繋いでみるだけで、外の道具に
# 頼らない（macOS と CI で同じに動く）。host が空（:8080）ならループバックを見る。
responds() {
	local host="${1%:*}" port="${1##*:}"
	(exec 3<>"/dev/tcp/${host:-127.0.0.1}/$port") 2>/dev/null
}

# **既に何かが応答しているポートには立てない**（#383 と同じ理由）。立てずに進むと、
# 下の待ちがそちら（別のチェックアウトで動いている etoki や偽物）の応答で済んで
# しまい、自分のコードではないものを検査する。etoki は「listening」をポートを掴む
# 前に出すので、ログを目印にしても防げない。
for addr in "$ETOKI_ADDR" "$FAKE_ADDR"; do
	if responds "$addr"; then
		echo "fake-stack: $addr は使用中です。止めるか、ETOKI_ADDR / FAKE_ADDR で別のアドレスを渡してください" >&2
		exit 1
	fi
done

# **DB は毎回作り直す。** 偽物の状態はメモリにしか無いので、DB を使い回すと
# 記録が指す draft issue が偽物の側に無く、更新が必ず失敗する（ADR 0050）。
tmp=$(mktemp -d)

# **どちらかが落ちたら両方止める。** 素の wait は全部の終了を待つので、偽物が
# 起動に失敗しても etoki だけが残って待ち続け、EXIT の trap も走らない。wait -n で
# 先に落ちたほうを拾い、その終了状態で抜ける。片付けを kill より先に書くのは、
# kill 0 がこのシェル自身も落とすため（.claude/rules/make-targets.md）。
pids=()
# trap からだけ呼ぶので、shellcheck には呼び出しが見えない。
# shellcheck disable=SC2329
cleanup() {
	status=$?
	trap - EXIT INT TERM
	# 起動前に落ちたときは空。素の "${pids[@]}" は set -u の古い bash で落ちる。
	kill ${pids[@]+"${pids[@]}"} 2>/dev/null || :
	wait ${pids[@]+"${pids[@]}"} 2>/dev/null || :
	rm -rf "$tmp"
	exit "$status"
}
trap cleanup EXIT
# 止められたのは失敗ではない（Ctrl-C や、Playwright が終わりに送る SIGTERM）。
# シグナルの終了状態のまま抜けると、make が毎回「Error 143」を出して、本当の
# 失敗と見分けが付かない。
trap 'exit 0' INT TERM

ETOKI_DB_PATH="$tmp/etoki.db" "$BIN_DIR/etoki" migrate || exit 1

FAKE_ADDR="$FAKE_ADDR" "$BIN_DIR/etoki-fakeupstream" &
fake_pid=$!
pids+=("$fake_pid")

# **継いだ環境変数のうち、向き先と食い違うものは外す。**
# - 認証と鍵: .env や direnv の本物の鍵を偽物へ送らないためと、App を設定して
#   いると認証の構成に入り、偽物ではログインできないため。
# - フィールド名の 2 つ: 偽の GitHub は Kind / Parent 固定なので、手元で別名に
#   設定している人は継いだ名前で引いて見つからず、作成が ErrProjectFieldMissing
#   で止まる。
env -u ETOKI_GITHUB_APP_CLIENT_ID -u ETOKI_GITHUB_APP_CLIENT_SECRET \
	-u ETOKI_TOKEN_ENCRYPTION_KEY -u ETOKI_PUBLIC_URL -u ETOKI_LLM_API_KEY \
	-u ETOKI_GITHUB_KIND_FIELD -u ETOKI_GITHUB_PARENT_FIELD \
	ETOKI_DB_PATH="$tmp/etoki.db" \
	ETOKI_WEB_DIR="$WEB_DIST" \
	ETOKI_LLM_BASE_URL="http://$FAKE_ADDR" \
	ETOKI_GITHUB_BASE_URL="http://$FAKE_ADDR" \
	ETOKI_GITHUB_TOKEN=fake \
	"$BIN_DIR/etoki" &
etoki_pid=$!
pids+=("$etoki_pid")

# 両方が応答するまで待ってから「ready」を出す。Playwright はこの行を待って
# テストを始め、**始めたあとに落ちても気づかない**（webServer は待ち終えた
# プロセスの終了を見ない）。だから出す前に、応答しているのが自分の 2 つである
# ことを確かめる: 上で空いていたポートが応答し、自分の 2 つがまだ生きている。
# 残る隙は、同じポートで同時に立ち上げた別の回が先に掴んだときだけ。
for _ in $(seq 300); do
	if ! kill -0 "$fake_pid" 2>/dev/null || ! kill -0 "$etoki_pid" 2>/dev/null; then
		echo "fake-stack: 起動に失敗しました（上のログを見てください）" >&2
		exit 1
	fi
	if responds "$ETOKI_ADDR" && responds "$FAKE_ADDR"; then
		echo "fake-stack: ready http://$ETOKI_ADDR （偽の GitHub に作られたもの: http://$FAKE_ADDR/_fake/items）"
		wait -n
		exit
	fi
	sleep 0.1
done
echo "fake-stack: 30 秒待っても応答しません" >&2
exit 1
