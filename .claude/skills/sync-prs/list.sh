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
# fork の PR の head は origin に無い（同じ名前の枝が origin にあれば、それは別の枝）。
# 取り込んでも push する先が無いので、印を付けて対象から外す。
pulls="$(gh api --paginate "repos/$nwo/pulls?state=open&per_page=100" \
	--jq '.[] | "\(.number)\t\(.head.ref)\t\(.base.ref)\t\(.user.login)\t\(.head.repo.full_name // "")"' | sort -n)"

# 開いている PR が無ければ、空の 1 行を回さずに終える。
[ -z "$pulls" ] && exit 0

# 下の段が squash で入り、上の段の base が main に付け替わった積みを見つける。
# 枝が消えると GitHub が base を main に移すので、base だけでは積みに見えない。
# 衝突していなくても、下の段の最後の head → main の順に取り込まないと、上の段が
# 下の段の変更を戻した箇所がマージで黙って戻る（SKILL.md「取り込む」）。
#
# 下の段の候補は、上の段にあって main に無いコミットのうち最も古いものより後に
# マージされた PR。そのうち「最初のコミットが上の段にあり main に無い」（上の段が
# その上に積まれていた）かつ「squash のコミットが上の段に無い」（まだ main を
# 取り込んでいない）ものを出す。取り込みを済ませた積みは出ない。
# 下の段は main に入ったものだけ（検索を base:main に絞り、squash のコミットが
# origin/main にあることも見る）。別の枝に入ったものを下の段とすると、上の段が
# 分かれたあとにその枝へ足された変更まで取り込む。
# 出せないとき（検索が 100 件を超えた、検索が途中で打ち切られた、など）は「?」を
# 出し、無いとは言わない。
# 候補の PR ごとの「最初のコミット」と「squash のコミット」は、開いている PR を
# またいで使い回す（同じ候補を PR ごとに取り直すと、候補の数 × PR の数だけ API を
# 呼ぶ）。結果はサブシェルを通さずに lower_result へ入れる。$(…) で受けると
# キャッシュがサブシェルの中で捨てられる。mapfile と同じく bash 4 以上が要る。
declare -A cand_first=() cand_merge=()
merged_lower() {
	local ref="$1" since res incomplete total nums found="" m first merge
	lower_result="?"
	# 最も古いコミットは末尾に来る。head で切ると git log が SIGPIPE で落ち、
	# pipefail で取れなかった扱いになるので tail で取る。日付は UTC で出す
	# （検索の merged: は UTC で比べる。%cs はコミットのタイムゾーンの日付で、
	# +09:00 の朝のコミットは UTC では前の日になる）。
	since="$(TZ=UTC git log --date=format-local:%Y-%m-%d --format=%cd "origin/main..$ref" | tail -n 1)" || return 0
	[ -z "$since" ] && { lower_result="-"; return 0; }
	# 総件数と候補の番号は 1 回の検索で取る。2 回に分けると、2 回目だけが落ちた
	# ときに候補が空になり、「無い」と読める。
	# 検索は時間切れでも成功で返り、incomplete_results で打ち切ったことを知らせる。
	res="$(gh api -X GET search/issues -f q="repo:$nwo is:pr is:merged base:main merged:>=$since" -f per_page=100 \
		--jq '"\(.incomplete_results) \(.total_count) \([.items[].number] | join(" "))"')" || return 0
	read -r incomplete total nums <<<"$res"
	[ "$incomplete" != "false" ] && return 0
	[ "$total" -gt 100 ] && return 0
	for m in $nums; do
		if [ -z "${cand_first[$m]:-}" ]; then
			first="$(gh api "repos/$nwo/pulls/$m/commits?per_page=1" --jq '.[0].sha')" || return 0
			merge="$(gh api "repos/$nwo/pulls/$m" --jq '.merge_commit_sha')" || return 0
			cand_first[$m]="$first"
			cand_merge[$m]="$merge"
		fi
		first="${cand_first[$m]}"
		merge="${cand_merge[$m]}"
		# base:main に入った PR の squash は main にあるはず。手元の origin/main に
		# 無ければ、冒頭の fetch のあとにマージされた。判定できないので「?」。
		git merge-base --is-ancestor "$merge" origin/main 2>/dev/null || return 0
		if git merge-base --is-ancestor "$first" "$ref" 2>/dev/null &&
			! git merge-base --is-ancestor "$first" origin/main 2>/dev/null &&
			! git merge-base --is-ancestor "$merge" "$ref" 2>/dev/null; then
			found="${found:+$found,}#$m"
		fi
	done
	lower_result="${found:--}"
}

while IFS=$'\t' read -r pr head base author repo; do
	if [ "$repo" != "$nwo" ]; then
		printf '#%s fork(%s) 対象外 author=%s %s\n' "$pr" "${repo:-消えたリポジトリ}" "$author" "$head"
		continue
	fi
	# mergeable は一覧には載らず、個別に引いたときに計算される。null は計算中。
	read -r mergeable mstate < <(gh api "repos/$nwo/pulls/$pr" --jq '"\(.mergeable) \(.mergeable_state)"')

	# 積みの上下も、このリポジトリの PR だけで引き当てる。fork の PR の head が
	# origin の枝と同じ名前でも、それは別の枝。
	stack="-"
	if [ "$base" != "main" ]; then
		lower="$(awk -F'\t' -v b="$base" -v n="$nwo" '$2 == b && $5 == n { print "#" $1 }' <<<"$pulls")"
		if [ -n "$lower" ]; then
			stack="上の段(下=$lower)"
		# 開いている下の段が無ければ、同じ枝から出てマージされた PR を探す。下の段を
		# squash しても枝を消さなければ、base はその枝を指したまま残る。
		# 「マージ済み」と言えるのは、main に入った（base が main で、squash の
		# コミットが origin/main にある）ものだけ。別の枝に入ったものや、手元の
		# origin/main でたどれないものは判定できない。
		# 枝の名前はクエリの値として渡す（-f）。URL に直に書くと、名前の & や # で
		# クエリが切れる。
		elif ! merged="$(gh api -X GET "repos/$nwo/pulls" -f state=closed -f head="${nwo%%/*}:$base" -f per_page=100 \
			--jq '.[] | select(.merged_at != null) | "\(.number) \(.base.ref) \(.merge_commit_sha)"')"; then
			stack="判定できない(?)。base の枝 $base の PR を確かめるまで取り込まない"
		elif [ -n "$merged" ]; then
			into_main="" undecided=""
			while read -r m mbase msha; do
				if [ "$mbase" = "main" ] && git merge-base --is-ancestor "$msha" origin/main 2>/dev/null; then
					into_main="${into_main:+$into_main,}#$m"
				else
					undecided="${undecided:+$undecided,}#$m"
				fi
			done <<<"$merged"
			if [ -n "$undecided" ]; then
				stack="判定できない(?)。base の枝 $base から出た $undecided が main に入ったか確かめるまで取り込まない"
			else
				stack="上の段(下=$into_main はマージ済みで、枝 $base が残っている。下の段の最後の head → main の順に取り込む)"
			fi
		else
			stack="上の段(下=PR の無い枝 $base)"
		fi
	else
		merged_lower "origin/$head"
		gone="$lower_result"
		case "$gone" in
		-) ;;
		"?") stack="判定できない(?)。マージ済みの下の段があるかを確かめるまで取り込まない" ;;
		*) stack="上の段(下=$gone はマージ済み。下の段の最後の head → main の順に取り込む)" ;;
		esac
	fi
	upper="$(awk -F'\t' -v h="$head" -v n="$nwo" '$3 == h && $5 == n { printf "#%s ", $1 }' <<<"$pulls")"
	[ -n "$upper" ] && stack="${stack/-/}下の段(上=${upper% })"

	# main にあって head に無いコミットの数。0 なら取り込むものが無い。
	behind="$(gh api "repos/$nwo/compare/$head...main" --jq .ahead_by 2>/dev/null || echo "?")"

	wt="-"
	grep -qxF -- "$head" <<<"$worktrees" && wt="この手元の worktree で開いている"

	printf '#%s mergeable=%s/%s base=%s main から遅れ=%s 積み=%s worktree=%s author=%s %s\n' \
		"$pr" "$mergeable" "$mstate" "$base" "$behind" "$stack" "$wt" "$author" "$head"
done <<<"$pulls"
