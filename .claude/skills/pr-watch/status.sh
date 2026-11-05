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
	# process substitution の失敗は mapfile に伝わらず、「開いている PR が無い」と
	# 区別できない。先に変数へ取り、落ちたら止める。
	if ! list="$(gh api --paginate "repos/$nwo/pulls?state=open&per_page=100" --jq '.[].number')"; then
		echo "開いている PR の一覧を取れなかった" >&2
		exit 1
	fi
	mapfile -t prs < <(sed '/^$/d' <<<"$list" | sort -n)
fi

# **CodeRabbit かどうかはログイン名の完全一致で見る。** REST では
# `coderabbitai[bot]`、GraphQL では Bot の `coderabbitai`。部分一致にすると、
# 名前に coderabbit を含む別のアカウントが書いた「Full review finished」や要約で、
# 0 件と出せてしまう（リポジトリは公開で、誰でもコメントを書ける）。
#
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
        nodes{ isResolved comments(first:1){ nodes{ author{ login __typename } } } }
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
		       | select(.comments.nodes[0].author | (.__typename == "Bot" and .login == "coderabbitai"))] | length' 2>/dev/null)"; then
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
#   LIM  CodeRabbit の返信「rate limited」か「Action not completed」（枠切れ。
#        何分後に空くかを添える）
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
	  elif $who != "coderabbitai[bot]" then empty
	  elif ($b | test("Full review finished")) then "FIN\t\(.created_at)\t\(.updated_at)\t-"
	  elif ($b | test("rate limited|Action not completed"; "i"))
	    then "LIM\t\(.created_at)\t\(.updated_at)\t\(($b | capture("available in (?<w>[0-9]+ [a-z]+)") | .w) // "?")"
	  elif ($b | test("Full review triggered|Actions? performed"; "i")) then "RUN\t\(.created_at)\t\(.updated_at)\t-"
	  elif ($b | test("Reviewing files that changed"))
	    then "SUM\t\(.created_at)\t\(.updated_at)\t\(($b | capture("between [0-9a-f]{40} and (?<h>[0-9a-f]{40})") | .h) // "?")"
	  else empty end'

# 依頼のあとに CodeRabbit が付けた指摘の数。行ごとのコメント（返信を除く）と、
# review 本文から読んだ数の大きいほう。返信して解決するとスレッドは 0 に
# なるが、その full review が指摘を出したことは変わらない。
#
# 本文から読む数は次の順で決める。
#   1. 「Actionable comments posted: N」があれば N（本文にしか出ない diff の外への
#      指摘もそちらに数えられる）。
#   2. 無ければ、本文の節の見出し（`Additional comments: N`、`Nitpick comments (N)`
#      など、`comments` の後ろに数が来る <summary>）の合計。件数行を持たず、
#      本文の節にだけ指摘を載せる review がある（#414 の 5732403886）。
#   3. どちらも無く、自動のチェックボックスと HTML コメントを除いて文字が残れば
#      「?」。読めない本文を 0 件と読まない。何も残らなければ 0（スレッドへの
#      返信で増える空の review と、状態だけの review）。
# 「?」の本文があり、ほかの本文にも行ごとのコメントにも指摘が無ければ「?」を返す
# （指摘が 1 件でも数えられれば、0 件でないことは言える）。
# $since / $b / $act / $sec は jq の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
findings_since() {
	local pr="$1" since="$2" inline body
	# 取れなかったら失敗で返す（呼ぶ側が 0 件と読まないように）。pipefail なので
	# gh が落ちれば代入ごと失敗する。
	inline="$(gh api --paginate "repos/$nwo/pulls/$pr/comments?per_page=100" \
		--jq '.[] | select(.in_reply_to_id == null) | select(.user.login == "coderabbitai[bot]") | .created_at' |
		awk -v s="$since" '$1 >= s { n++ } END { print n + 0 }')" || return 1
	body="$(gh api --paginate "repos/$nwo/pulls/$pr/reviews?per_page=100" \
		--jq '.[] | select(.user.login == "coderabbitai[bot]")
		      | (.body // "") as $b
		      | (($b | capture("Actionable comments posted: (?<n>[0-9]+)") | .n) // null) as $act
		      | ([$b | scan("comments(?::[ \\t]*|[ \\t]*\\()([0-9]+)\\)?[ \\t]*</summary>"; "i") | .[0] | tonumber] | add) as $sec
		      | "\(.submitted_at) \(
		          if $act != null then $act
		          elif $sec != null then $sec
		          elif ($b | gsub("<!-- autofix_checkbox_start -->[\\s\\S]*?<!-- autofix_checkbox_end -->"; "")
		                   | gsub("<!--[\\s\\S]*?-->"; "") | test("\\S")) then "?"
		          else 0 end)"' |
		awk -v s="$since" '$1 >= s { if ($2 == "?") u = 1; else n += $2 } END { print (u && n == 0 ? "?" : n + 0) }')" || return 1
	if [ "$body" = "?" ]; then
		if [ "$inline" -gt 0 ]; then echo "$inline"; else echo "?"; fi
		return 0
	fi
	echo $((inline > body ? inline : body))
}

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
			elif ! found="$(findings_since "$pr" "$req_at")"; then
				fr="指摘の数を取れなかった(0 件とは読まない。取り直す)"
			elif [ "$found" = "?" ]; then
				fr="指摘の数を読めない(件数の無い review 本文がある。0 件とは読まない。本文を読む)"
			elif [ "$found" != "0" ]; then
				fr="指摘あり(この full review で ${found} 件。直して依頼し直す)"
			else
				fr="済(head を読んだ)"
			fi
			;;
		esac
	fi

	threads="$(unresolved_threads "$pr")"

	# CI。check-run が 1 本も無いのは、衝突で pull_request のワークフローが
	# 起動していないことが多い（SKILL.md「CI が走らない」）。1 ページは 100 本まで
	# なので全ページを回す。--jq はページごとに掛かるので、1 本 1 行に落としてから
	# awk でまとめる。取れなければ「?」を出し、緑とは言わない。
	# 緑と言えるのは、結論が success / neutral / skipped のものだけのとき。stale の
	# ような結果の決まっていない結論や、知らない結論が 1 本でもあれば「未確定」に
	# する（赤でも走行中でもないものを緑に数えない）。
	if runs="$(gh api --paginate "repos/$nwo/commits/$head/check-runs?per_page=100" \
		--jq '.check_runs[] | "\(.status)\t\(.conclusion // "")\t\(.name)"')"; then
		ci="$(awk -F'\t' '
			NF == 0 { next }
			{ n++ }
			$1 != "completed" { run++ }
			$2 == "failure" || $2 == "cancelled" || $2 == "timed_out" || $2 == "action_required" {
				bad = bad (bad == "" ? "" : ",") $3
				next
			}
			$1 == "completed" && $2 != "success" && $2 != "neutral" && $2 != "skipped" {
				unknown = unknown (unknown == "" ? "" : ",") $3 ":" $2
			}
			END {
				if (n == 0) print "未起動"
				else if (bad != "") print "赤(" bad ")"
				else if (run > 0) print "走行中"
				else if (unknown != "") print "未確定(" unknown ")"
				else print "緑"
			}' <<<"$runs")"
	else
		ci="?"
	fi

	# 0 件と言えるのは、head を読んだ full review が終わり、その full review が指摘を
	# 出さず（上の findings_since）、未解決のスレッドも無いときだけ。
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
