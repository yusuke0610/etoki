#!/usr/bin/env bash
# 開いている PR が main に追従できているかを 1 PR 1 行で出す（/sync-prs）。
# GitHub には何も書かない。手元で書き換えるのは origin/* だけ（git fetch）。
#
# 取り込みは head を変え、full review（1 時間に 1 本）をやり直させる。要る
# 取り込みと要らない取り込みを見分けるための材料を出す。
#
# 使い方: .claude/skills/sync-prs/list.sh
set -euo pipefail

# gh pr list は GraphQL で、クラウドのセッションでは塞がれている。REST だけで引く。
nwo="$(gh api 'repos/{owner}/{repo}' --jq .full_name)"

git fetch -q origin

# 他所の worktree で開いている枝。そこで作業中の枝を、黙って取り込まない。
worktrees="$(git worktree list --porcelain | awk '/^branch / { sub("refs/heads/", "", $2); print $2 }')"

# 番号・head の枝・base を先に全部引く。積みの下の段を、base の枝名から引き当てる。
pulls="$(gh api --paginate "repos/$nwo/pulls?state=open&per_page=100" \
	--jq '.[] | "\(.number)\t\(.head.ref)\t\(.base.ref)\t\(.user.login)"' | sort -n)"

while IFS=$'\t' read -r pr head base author; do
	# mergeable は一覧には載らず、個別に引いたときに計算される。null は計算中。
	read -r mergeable mstate < <(gh api "repos/$nwo/pulls/$pr" --jq '"\(.mergeable) \(.mergeable_state)"')

	stack="-"
	if [ "$base" != "main" ]; then
		lower="$(awk -F'\t' -v b="$base" '$2 == b { print "#" $1 }' <<<"$pulls")"
		stack="上の段(下=${lower:-PR の無い枝 $base})"
	fi
	upper="$(awk -F'\t' -v h="$head" '$3 == h { printf "#%s ", $1 }' <<<"$pulls")"
	[ -n "$upper" ] && stack="${stack/-/}下の段(上=${upper% })"

	# main にあって head に無いコミットの数。0 なら取り込むものが無い。
	behind="$(gh api "repos/$nwo/compare/$head...main" --jq .ahead_by 2>/dev/null || echo "?")"

	wt="-"
	grep -qxF -- "$head" <<<"$worktrees" && wt="この手元の worktree で開いている"

	printf '#%s mergeable=%s/%s base=%s main から遅れ=%s 積み=%s worktree=%s author=%s %s\n' \
		"$pr" "$mergeable" "$mstate" "$base" "$behind" "$stack" "$wt" "$author" "$head"
done <<<"$pulls"
