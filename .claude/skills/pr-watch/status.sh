#!/usr/bin/env bash
# 開いている PR の、マージの条件に関わる状態を 1 PR 1 行で出す（/pr-watch）。
# GitHub には何も書かない。
#
# 見回りのたびに組み立て直していた問い合わせを 1 つにした。いちばん間違えやすい
# のは full review の読み方で、ここで判定まで出す（読み方は SKILL.md）。
#
# 使い方: .claude/skills/pr-watch/status.sh [PR 番号...]
#         番号を渡さなければ、開いている PR をすべて出す。
set -euo pipefail

# 一覧も含めて REST だけで引く。gh repo view / gh pr list は GraphQL で、
# クラウドのセッションでは塞がれている。{owner}/{repo} は gh が今のリポジトリで埋める。
nwo="$(gh api 'repos/{owner}/{repo}' --jq .full_name)"

if [ "$#" -gt 0 ]; then
	prs=("$@")
else
	mapfile -t prs < <(gh api --paginate "repos/$nwo/pulls?state=open&per_page=100" --jq '.[].number' | sort -n)
fi

# 未解決のスレッドの数。findings.sh と同じく GraphQL で CodeRabbit のものだけを
# 数える。クラウドのセッションでは GraphQL が塞がれていて、代わりにプロキシの
# REST（ccr/review_threads）がある。そちらは書き手で絞れないので、人のスレッドも
# 数える。どちらも取れなければ「?」を出し、0 件とは言わない。
# $owner / $name / $pr / $endCursor は GraphQL の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
threads_query='
query($owner:String!,$name:String!,$pr:Int!,$endCursor:String){
  repository(owner:$owner,name:$name){
    pullRequest(number:$pr){
      reviewThreads(first:100, after:$endCursor){
        pageInfo{ hasNextPage endCursor }
        nodes{ isResolved comments(first:1){ nodes{ author{login} } } }
      }
    }
  }
}'

unresolved_threads() {
	local pr="$1" counts
	if counts="$(gh api graphql --paginate -f query="$threads_query" \
		-F owner="${nwo%%/*}" -F name="${nwo##*/}" -F pr="$pr" \
		--jq '[.data.repository.pullRequest.reviewThreads.nodes[]
		       | select(.isResolved | not)
		       | select((.comments.nodes[0].author.login // "") | test("coderabbit"))] | length' 2>/dev/null)"; then
		awk '{ s += $1 } END { print s + 0 }' <<<"$counts"
	elif counts="$(gh api "repos/$nwo/pulls/$pr/ccr/review_threads" \
		--jq '[.[] | select(.resolved | not)] | length' 2>/dev/null)"; then
		echo "$counts"
	else
		echo "?"
	fi
}

# full review の判定に使うコメントを、種類ごとに 1 行へ落とす。
#   REQ  人が書いた `@coderabbitai full review`
#   FIN  CodeRabbit の返信「Full review finished」（走り終えた）
#   RUN  CodeRabbit の返信「triggered」（走っている）
#   LIM  CodeRabbit の返信「rate limited」（枠切れ。何分後に空くかを添える）
#   SUM  要約のコメント。最後に読んだ commit（「between A and B」の B）を添える
# 返信は 1 つのコメントが triggered → finished と書き換わるので、時刻は
# created_at（依頼との前後）と updated_at（終わった時刻）の両方を持つ。
# --jq はページごとに掛かるので、ここでは行に落とすだけにし、判定は awk で行う。
# $who / $b は jq の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
classify='.[]
	| (.user.login // "") as $who
	| (.body // "") as $b
	| if ($who | test("\\[bot\\]$") | not) and ($b | test("@coderabbitai\\s+full review"))
	    then "REQ\t\(.created_at)\t\(.updated_at)\t-"
	  elif ($who | test("coderabbit")) | not then empty
	  elif ($b | test("Full review finished")) then "FIN\t\(.created_at)\t\(.updated_at)\t-"
	  elif ($b | test("rate limited"; "i"))
	    then "LIM\t\(.created_at)\t\(.updated_at)\t\(($b | capture("available in (?<w>[0-9]+ [a-z]+)") | .w) // "?")"
	  elif ($b | test("Full review triggered|Actions? performed"; "i")) then "RUN\t\(.created_at)\t\(.updated_at)\t-"
	  elif ($b | test("Reviewing files that changed"))
	    then "SUM\t\(.created_at)\t\(.updated_at)\t\(($b | capture("between [0-9a-f]{40} and (?<h>[0-9a-f]{40})") | .h) // "?")"
	  else empty end'

latest_finished=""

for pr in "${prs[@]}"; do
	read -r head base mergeable mstate < <(gh api "repos/$nwo/pulls/$pr" \
		--jq '"\(.head.sha) \(.base.ref) \(.mergeable) \(.mergeable_state)"')
	head_time="$(gh api "repos/$nwo/commits/$head" --jq '.commit.committer.date')"

	events="$(gh api --paginate "repos/$nwo/issues/$pr/comments?per_page=100" --jq "$classify")"

	# 依頼のうち最後のもの、その後に来た最初の返信、要約を拾う。
	# ISO 8601 の UTC 時刻なので、文字列の比較で前後が決まる。
	read -r req_at reply reply_at reply_extra sum_at sum_sha < <(awk -F'\t' '
		$1 == "REQ" { req = $2 }
		$1 == "SUM" { sum_at = $3; sum_sha = $4 }
		{ kind[NR] = $1; c[NR] = $2; u[NR] = $3; x[NR] = $4 }
		END {
			r = "-"; ra = "-"; rx = "-"
			if (req != "") for (i = 1; i <= NR; i++)
				if ((kind[i] == "FIN" || kind[i] == "RUN" || kind[i] == "LIM") && c[i] >= req) {
					r = kind[i]; ra = u[i]; rx = x[i]; gsub(/ /, "_", rx); break
				}
			print (req == "" ? "-" : req), r, ra, rx, (sum_at == "" ? "-" : sum_at), (sum_sha == "" ? "-" : sum_sha)
		}' <<<"$events")

	# full review の判定。順番に意味がある（SKILL.md「full review の読み方」）。
	if [ "$req_at" = "-" ]; then
		fr="未依頼"
	elif [[ "$req_at" < "$head_time" ]]; then
		fr="要再依頼(依頼のあとに head が進んだ)"
	else
		case "$reply" in
		-) fr="返信待ち(依頼 $req_at)" ;;
		RUN) fr="走行中(依頼 $req_at)" ;;
		LIM) fr="枠切れ(${reply_extra//_/ } 後に空く。依頼し直す)" ;;
		FIN)
			[[ "$reply_at" > "$latest_finished" ]] && latest_finished="$reply_at #$pr"
			if [ "$sum_sha" != "$head" ]; then
				fr="要再依頼(読んだのは ${sum_sha:0:7})"
			elif [[ "$sum_at" < "$req_at" ]]; then
				fr="要約が古い(依頼より前に更新)"
			else
				fr="済(head を読んだ)"
			fi
			;;
		esac
	fi

	threads="$(unresolved_threads "$pr")"

	# CI。check-run が 1 本も無いのは、衝突で pull_request のワークフローが
	# 起動していないことが多い（SKILL.md「CI が走らない」）。
	# $run / $bad / $c は jq の変数。
	# shellcheck disable=SC2016
	ci="$(gh api "repos/$nwo/commits/$head/check-runs?per_page=100" --jq '
		[.check_runs[] | select(.status != "completed")] as $run
		| [.check_runs[] | select(.conclusion as $c | ["failure","cancelled","timed_out","action_required"] | index($c))] as $bad
		| if .total_count == 0 then "未起動"
		  elif ($bad | length) > 0 then "赤(\($bad | map(.name) | join(",")))"
		  elif ($run | length) > 0 then "走行中"
		  else "緑" end')"

	# 0 件と言えるのは、head を読んだ full review が終わり、未解決のスレッドが無いときだけ。
	verdict=""
	if [ "$fr" = "済(head を読んだ)" ] && [ "$threads" = "0" ]; then
		verdict="  => full review 0 件"
	fi

	printf '#%s head=%s fr=%s threads=%s mergeable=%s/%s ci=%s base=%s%s\n' \
		"$pr" "${head:0:7}" "$fr" "$threads" "$mergeable" "$mstate" "$ci" "$base" "$verdict"
done

# 枠は org で 1 時間に 1 本。走り終えた時刻ではなく依頼した時刻から数えるが、
# 終わった時刻のほうが読み違えにくいので目安として出す。
if [ -n "$latest_finished" ]; then
	echo
	echo "直近に終わった full review: ${latest_finished}（枠はおおよそ 1 時間後に空く）"
fi
