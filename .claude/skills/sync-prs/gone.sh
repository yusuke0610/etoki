#!/usr/bin/env bash
# main で消えた・名前が変わった識別子を、PR が使っていないかを探す（/sync-prs）。
# 文字の衝突が無くても、片方が消した関数をもう片方が呼んでいることがある。
# これは取り込みが要るかを決めるための手掛かり。取り込んだあとの make lint /
# make test で見つかるのも、型とテストが見ている範囲だけ。
#
# 探す範囲は意図して狭い。
#   - 消えた識別子: PR の分岐点から origin/main までの差分で、定義の行
#     （Go の func / type、TypeScript の export）が消え、origin/main のどこにも
#     定義が残っていない名前
#   - 使っているか: PR の差分で足された行に、その名前が単語として出てくるか
# 定義の書き換えで引数が変わったもの、PR が以前からあった行で使っているもの、
# 名前を並べた export（`export { A, B }`）から消えたものは拾わない。そちらは検査に
# 任せる（消えた export を import していれば、make lint の型検査で落ちる）。
# 名前を並べた export は、このリポジトリの TypeScript では使っていない。
#
# 使い方: .claude/skills/sync-prs/gone.sh <PR の枝>
set -euo pipefail

branch="${1:?PR の枝の名前を渡す}"
git fetch -q origin main "$branch"

base="$(git merge-base "origin/main" "origin/$branch")"

# 定義の行から名前を抜く。Go はメソッドのレシーバも許す。
defs() {
	sed -nE \
		-e 's/^func (\([^)]*\) )?([A-Za-z_][A-Za-z0-9_]*).*/\2/p' \
		-e 's/^type ([A-Za-z_][A-Za-z0-9_]*).*/\1/p' \
		-e 's/^export (default )?(async |abstract )?(function|const|let|class|type|interface|enum) ([A-Za-z_$][A-Za-z0-9_$]*).*/\4/p'
}

removed="$(git diff "$base" origin/main -- '*.go' '*.ts' '*.tsx' |
	sed -nE 's/^-([^-].*)/\1/p' | defs | sort -u)"

found=0
while read -r name; do
	[ -z "$name" ] && continue
	# 別の場所に定義が残っている（移動しただけ）なら消えていない。名前は正規表現
	# としてではなく文字どおりに比べる（TypeScript の名前は $ を含みうる）。名前の
	# 終わりも \b ではなく「識別子に使える文字が続かない」で見る（$ の後ろでは
	# \b が効かない）。
	lit="$(printf '%s' "$name" | sed 's/[$]/\\$/g')"
	if git grep -qE "^(func (\([^)]*\) )?|type |export (default )?(async |abstract )?(function|const|let|class|type|interface|enum) )${lit}([^A-Za-z0-9_$]|$)" \
		origin/main -- '*.go' '*.ts' '*.tsx'; then
		continue
	fi
	uses="$(git diff "$base" "origin/$branch" -- '*.go' '*.ts' '*.tsx' |
		grep -E '^\+[^+]' | grep -wF -- "$name" || true)"
	if [ -n "$uses" ]; then
		found=1
		echo "== $name（main で定義が消えた）"
		# head は 5 行で読むのをやめ、echo が SIGPIPE で落ちて pipefail で止まりうる。
		# sed は最後まで読む。
		echo "$uses" | sed -n '1,5p'
	fi
done <<<"$removed"

# 出なかったときも、見た範囲だけの話と分かるように出す。範囲の外（TypeScript の
# クラスのメソッド、名前を並べた export、引数の変わった関数、PR が以前からあった
# 行での使用）で消えたものは拾わない。
if [ "$found" = 0 ]; then
	echo "main で定義が消えた名前（Go の func / type、TypeScript の export）を、$branch の足した行は使っていない"
	echo "ここに出ない範囲（クラスのメソッド、名前を並べた export、引数の変化、以前からあった行）は見ていない。取り込むかは make lint / make test と差分を読んで決める"
fi
exit 0
