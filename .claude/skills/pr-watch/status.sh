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
#   REV  人が書いた `@coderabbitai review`（増分のレビュー。要約を書き換える）
#   FIN  CodeRabbit の返信「Full review finished」（走り終えた）
#   RUN  CodeRabbit の返信「triggered」（走っている）
#   LIM  CodeRabbit の返信「rate limited」か「Action not completed」（枠切れ。
#        何分後に空くかを添える）
#   SUM  要約のコメント。最後に読んだ commit を添える。「between A and B」の B、
#        それが無ければ `"coveredCommitId":"<sha>","kind":"reviewed"` の sha。
#        どちらも無ければ「?」（読んだ head と言わない）
# **要約は印（`summarize by coderabbit.ai`）で見分け、返信より先に判定する。**
# 要約は本文に「rate limited」を含むことがあり、後に回すと LIM に取られて
# 要約が無いことになる（「between A and B」を持たない要約もある）。
# 返信は 1 つのコメントが triggered → finished と書き換わるので、時刻は
# created_at（依頼との前後）と updated_at（終わった時刻）の両方を持つ。末尾に
# updated_at の秒（epoch）を添える（要約と返信の書き換わりの前後を、date の
# 実装の違いに頼らずに比べるため）。
# --jq はページごとに掛かるので、ここでは行に落とすだけにし、判定は awk で行う。
# $who / $b は jq の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
classify='.[]
	| (.user.login // "") as $who
	| (.body // "") as $b
	| (.updated_at | fromdateiso8601) as $ep
	| if ($who | test("\\[bot\\]$") | not) and ($b | test("@coderabbitai\\s+full review"))
	    then "REQ\t\(.created_at)\t\(.updated_at)\t-\t\($ep)"
	  elif ($who | test("\\[bot\\]$") | not) and ($b | test("@coderabbitai\\s+review\\b"))
	    then "REV\t\(.created_at)\t\(.updated_at)\t-\t\($ep)"
	  elif $who != "coderabbitai[bot]" then empty
	  elif ($b | test("summarize by coderabbit\\.ai|Reviewing files that changed"))
	    then "SUM\t\(.created_at)\t\(.updated_at)\t\(
	      ($b | capture("between [0-9a-f]{40} and (?<h>[0-9a-f]{40})") | .h)
	      // ($b | capture("\"coveredCommitId\":\"(?<h>[0-9a-f]{40})\",\"kind\":\"reviewed\"") | .h)
	      // "?")\t\($ep)"
	  elif ($b | test("Full review finished")) then "FIN\t\(.created_at)\t\(.updated_at)\t-\t\($ep)"
	  elif ($b | test("rate limited|Action not completed"; "i"))
	    then "LIM\t\(.created_at)\t\(.updated_at)\t\(($b | capture("available in (?<w>[0-9]+ [a-z]+)") | .w) // "?")\t\($ep)"
	  elif ($b | test("Full review triggered|Actions? performed"; "i")) then "RUN\t\(.created_at)\t\(.updated_at)\t-\t\($ep)"
	  else empty end'

# 依頼のあとに CodeRabbit が付けた指摘の数。返信して解決するとスレッドは 0 に
# なるが、その full review が指摘を出したことは変わらない。
#
# review ごとに「行ごとのコメント（返信を除く）」と「本文にだけ載る指摘」を足す。
#   - 行ごとのコメントの数は、本文の「Actionable comments posted: N」と比べて
#     大きいほうを使う。**diff の外への指摘は N に入らない**（#414 の 5778439557
#     は N=1 で、行が 1 件、diff の外が 1 件だった）。
#   - 本文にだけ載る指摘は、`comments` の後ろに数が来る節の見出し
#     （`Outside diff range comments (N)`、`Nitpick comments (N)`、
#     `Additional comments: N` など。<summary> か ** で囲まれたもの）の合計。
#     件数行を持たず、節にだけ指摘を載せる review もある（#414 の 5732403886）。
#   - 件数行も節の見出しも無いのに、決まった部品を除いて文字が残る本文は、件数を
#     読めない。読めない本文を 0 件と読まない。除く部品は、自動のチェックボックス、
#     HTML コメント、<h6> の「Review info」、区切りの ---、それに指摘の一覧
#     （`Inline comments:` などの行）を持たない「Prompt to fix all issues」の欄
#     （指摘 0 件の full review でも pre-merge check の案内だけを載せて付く。#414 の
#     5821456498）。何も残らない本文（スレッドへの返信で増える空の review、状態
#     だけの review、指摘 0 件の review）は 0。
# 読めない本文があれば、ほかで数えた数に「+」を付け（0 なら「?」）、確定した数とは
# 言わない。
# $since / $b / $act / $sec は jq の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
findings_since() {
	local pr="$1" since="$2" inline body
	# 取れなかったら失敗で返す（呼ぶ側が 0 件と読まないように）。
	inline="$(gh api --paginate "repos/$nwo/pulls/$pr/comments?per_page=100" \
		--jq '.[] | select(.in_reply_to_id == null) | select(.user.login == "coderabbitai[bot]")
		      | "I \(.pull_request_review_id) \(.created_at)"')" || return 1
	body="$(gh api --paginate "repos/$nwo/pulls/$pr/reviews?per_page=100" \
		--jq '.[] | select(.user.login == "coderabbitai[bot]")
		      | (.body // "") as $b
		      | (($b | capture("Actionable comments posted: (?<n>[0-9]+)") | .n) // "-") as $act
		      | ([$b | scan("comments(?::[ \\t]*|[ \\t]*\\()([0-9]+)\\)?[ \\t]*(?:</summary>|\\*\\*)"; "i") | .[0] | tonumber] | add // "-") as $sec
		      | ($b | test("(?m)^(Inline|Outside diff|Nitpick|Additional|Duplicate) comments:")) as $listed
		      | ($b | gsub("<!-- autofix_checkbox_start -->[\\s\\S]*?<!-- autofix_checkbox_end -->"; "")
		            | gsub("<!--[\\s\\S]*?-->"; "")
		            | gsub("<h6>[\\s\\S]*?</h6>"; "")
		            | if $listed then . else gsub("<details>\\s*<summary>[^\\n]*?Prompt to fix all issues</summary>[\\s\\S]*?</details>"; "") end
		            | gsub("(?m)^-{3,}[ \\t]*$"; "")
		            | test("\\S")) as $rest
		      | "R \(.id) \(.submitted_at) \($act) \($sec) \(if $rest then 1 else 0 end)"')" || return 1
	awk -v s="$since" '
		$1 == "I" && $3 >= s { inl[$2]++ }
		$1 == "R" && $3 >= s { seen[$2] = 1; act[$2] = $4; sec[$2] = $5; rest[$2] = $6 }
		END {
			for (id in seen) {
				i = inl[id] + 0
				a = (act[id] == "-" ? 0 : act[id] + 0)
				n += (a > i ? a : i) + (sec[id] == "-" ? 0 : sec[id])
				if (act[id] == "-" && sec[id] == "-" && rest[id] == 1) u = 1
			}
			# 依頼より前に出た review に属する行のコメントは、上の since で落ちている。
			# review が取れなかった行のコメントも数え落とさない。
			for (id in inl) if (!(id in seen)) n += inl[id]
			if (u) print (n > 0 ? n "+" : "?")
			else print n + 0
		}' <<<"$inline
$body"
}

# CI の状態。check-run が 1 本も無いのは、衝突で pull_request のワークフローが
# 起動していないことが多い（SKILL.md「CI が走らない」）。
# **数えるのは GitHub Actions の check-run だけ**（app が github-actions）。
# CodeRabbit のようなほかの App も check-run を付け（CodeRabbit は skipped）、
# 数えると CI のジョブが 1 本も無いのに緑と出る。
# **ワークフローの run が 1 本でも終わっていなければ走行中**とする。needs で
# 後に回るジョブの check-run は前のジョブが終わるまで出ないので、check-run
# だけ見ると、出揃う前に緑と出る。
# 緑と言えるのは、run がすべて終わり、check-run の結論が success / neutral /
# skipped のものだけのとき。stale のような結果の決まっていない結論や、知らない
# 結論が 1 本でもあれば「未確定」にする（赤でも走行中でもないものを緑に数えない）。
# 1 ページは 100 本までなので全ページを回す。--jq はページごとに掛かるので、
# 1 本 1 行に落としてから awk でまとめる。取れなければ「?」を出し、緑とは言わない。
ci_state() {
	local head="$1" runs pending
	runs="$(gh api --paginate "repos/$nwo/commits/$head/check-runs?per_page=100" \
		--jq '.check_runs[] | select(.app.slug == "github-actions")
		      | "\(.status)\t\(.conclusion // "")\t\(.name)"')" || { echo "?"; return 0; }
	pending="$(gh api --paginate "repos/$nwo/actions/runs?head_sha=$head&per_page=100" \
		--jq '.workflow_runs[] | select(.status != "completed") | .id' | awk 'END { print NR }')" || { echo "?"; return 0; }
	awk -F'\t' -v pending="$pending" '
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
			if (n == 0 && pending == 0) print "未起動"
			else if (bad != "") print "赤(" bad ")"
			else if (run > 0 || pending > 0) print "走行中"
			else if (unknown != "") print "未確定(" unknown ")"
			else print "緑"
		}' <<<"$runs"
}

latest_finished=""

for pr in "${prs[@]}"; do
	read -r head base mergeable mstate head_ref < <(gh api "repos/$nwo/pulls/$pr" \
		--jq '"\(.head.sha) \(.base.ref) \(.mergeable) \(.mergeable_state) \(.head.ref)"')
	# head がいつ来たか。committer の日付は書き手が決められる（古い日付のまま
	# push できる）ので、GitHub がこの PR の枝でその commit の check-suite を最初に
	# 作った時刻（その枝への push を受けた時刻）とのうち遅いほうを使う。
	# check-suite は commit ごとに作られ、最初に push された枝を head_branch に持つ。
	# 同じ commit を先に別の枝へ push してから PR の枝へ移すと、PR の枝の
	# check-suite は無く、別の枝の古い時刻しか無い。そのときと、check-suite が
	# 1 つも無いとき、取れなかったときは「?」にして、依頼との前後を言わない。
	head_time="$(gh api "repos/$nwo/commits/$head" --jq '.commit.committer.date')" || head_time="?"
	if seen_at="$(gh api --paginate "repos/$nwo/commits/$head/check-suites?per_page=100" \
		--jq '.check_suites[] | "\(.head_branch // "")\t\(.created_at)"' |
		awk -F'\t' -v b="$head_ref" '$1 == b { print $2 }' | sort | awk 'NR == 1')" && [ -n "$seen_at" ]; then
		if [[ "$seen_at" > "$head_time" ]]; then head_time="$seen_at"; fi
	else
		head_time="?"
	fi

	# 取れなければ、この PR は「取れなかった」と出して後ろの PR に進む（set -e で
	# 止めると、残りの PR の状態が出ない）。
	events_ok=1
	events="$(gh api --paginate "repos/$nwo/issues/$pr/comments?per_page=100" --jq "$classify")" || {
		events_ok=0
		events=""
	}

	# 依頼のうち最後のもの、その後に来た最初の返信、要約を拾う。
	# ISO 8601 の UTC 時刻なので、文字列の比較で前後が決まる。
	read -r req_at reply reply_at reply_extra sum_at sum_sha reply_ep sum_ep rev_at < <(awk -F'\t' '
		$1 == "REQ" { req = $2 }
		$1 == "REV" { rev = $2 }
		$1 == "SUM" { sum_at = $3; sum_sha = $4; sum_ep = $5 }
		{ kind[NR] = $1; c[NR] = $2; u[NR] = $3; x[NR] = $4; e[NR] = $5 }
		END {
			r = "-"; ra = "-"; rx = "-"; re = 0
			if (req != "") for (i = 1; i <= NR; i++)
				if ((kind[i] == "FIN" || kind[i] == "RUN" || kind[i] == "LIM") && c[i] >= req) {
					r = kind[i]; ra = u[i]; rx = x[i]; re = e[i]; gsub(/ /, "_", rx); break
				}
			print (req == "" ? "-" : req), r, ra, rx, (sum_at == "" ? "-" : sum_at), (sum_sha == "" ? "-" : sum_sha), re, sum_ep + 0, (rev == "" ? "-" : rev)
		}' <<<"$events")

	# full review の判定。順番に意味がある（SKILL.md「full review の読み方」）。
	if [ "$events_ok" = 0 ]; then
		fr="コメントを取れなかった(0 件とは読まない。取り直す)"
	elif [ "$req_at" = "-" ]; then
		fr="未依頼"
	elif [ "$head_time" = "?" ]; then
		fr="head の時刻を取れなかった(0 件とは読まない。取り直す)"
	# 同じ秒に記録された push も「依頼のあと」に数える（前後を言えない）。
	elif [[ ! "$req_at" > "$head_time" ]]; then
		fr="要再依頼(依頼のあとに head が進んだ)"
	# force push で、前にこの枝にあった commit へ戻すと、その commit の
	# check-suite は依頼より前のまま。依頼のあとの force push は head が変わったと読む。
	elif ! forced="$(gh api --paginate "repos/$nwo/issues/$pr/timeline?per_page=100" \
		--jq '.[] | select(.event == "head_ref_force_pushed") | .created_at' |
		awk -v r="$req_at" '$1 >= r { n++ } END { print n + 0 }')"; then
		fr="force push の有無を取れなかった(0 件とは読まない。取り直す)"
	elif [ "$forced" != "0" ]; then
		fr="要再依頼(依頼のあとに force push された)"
	# 増分のレビューは要約を、それが読んだ head で書き換える。依頼のあとに頼んで
	# いたら、要約の commit が full review の読んだものと言えない。
	elif [ "$rev_at" != "-" ] && [[ ! "$rev_at" < "$req_at" ]]; then
		fr="要再依頼(依頼のあとに増分のレビューを頼んだ。要約が読んだ head と言えない)"
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
			# 要約は full review の終わりと数秒の差で書き換わる。それより 5 分以上
			# あとに書き換わっていたら、別のレビュー（増分）が読んだ head を指して
			# いるかもしれない（head の時刻は push を受けた時刻でも見ているが、
			# 要約が別のレビューで書き換わったことはここでしか分からない）。
			elif ((sum_ep > reply_ep + 300)); then
				fr="要再依頼(要約が full review のあとに書き換わった。読んだ head と言えない)"
			elif ! found="$(findings_since "$pr" "$req_at")"; then
				fr="指摘の数を取れなかった(0 件とは読まない。取り直す)"
			elif [ "$found" = "?" ]; then
				fr="指摘の数を読めない(件数の無い review 本文がある。0 件とは読まない。本文を読む)"
			elif [[ "$found" == *+ ]]; then
				fr="指摘あり(この full review で ${found%+} 件以上。件数を読めない本文もあるので本文を読む。直して依頼し直す)"
			elif [ "$found" != "0" ]; then
				fr="指摘あり(この full review で ${found} 件。直して依頼し直す)"
			else
				fr="済(head を読んだ)"
			fi
			;;
		esac
	fi

	threads="$(unresolved_threads "$pr")"

	ci="$(ci_state "$head")"

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
