#!/usr/bin/env bash
# main で消えた・名前が変わった識別子を、PR が使っていないかを探す（/sync-prs）。
# 文字の衝突が無くても、片方が消した関数をもう片方が呼んでいることがある。
# これは取り込みが要るかを決めるための手掛かりで、確定は取り込んだあとの
# make lint / make test が行う。
#
# 探す範囲は意図して狭い。
#   - 消えた識別子: PR の分岐点から origin/main までの差分で、定義の行
#     （Go の func / type、TypeScript の export）が消え、origin/main のどこにも
#     定義が残っていない名前
#   - 使っているか: PR の差分で足された行に、その名前が単語として出てくるか
# 定義の書き換えで引数が変わったもの、PR が以前からあった行で使っているものは
# 拾わない。そちらは検査に任せる。
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
		-e 's/^export (default )?(async )?(function|const|let|class|type|interface|enum) ([A-Za-z_$][A-Za-z0-9_$]*).*/\4/p'
}

removed="$(git diff "$base" origin/main -- '*.go' '*.ts' '*.tsx' |
	sed -nE 's/^-([^-].*)/\1/p' | defs | sort -u)"

found=0
while read -r name; do
	[ -z "$name" ] && continue
	# 別の場所に定義が残っている（移動しただけ）なら消えていない。
	if git grep -qE "^(func (\([^)]*\) )?|type |export (default )?(async )?(function|const|let|class|type|interface|enum) )${name}\b" \
		origin/main -- '*.go' '*.ts' '*.tsx'; then
		continue
	fi
	uses="$(git diff "$base" "origin/$branch" -- '*.go' '*.ts' '*.tsx' |
		grep -E '^\+[^+]' | grep -wF -- "$name" || true)"
	if [ -n "$uses" ]; then
		found=1
		echo "== $name（main で定義が消えた）"
		echo "$uses" | head -5
	fi
done <<<"$removed"

[ "$found" = 0 ] && echo "main で消えた識別子を、$branch の足した行は使っていない"
exit 0
