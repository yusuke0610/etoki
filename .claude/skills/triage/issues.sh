#!/usr/bin/env bash
# 開いている issue を振り分けるための材料を出す（/triage）。GitHub には何も書かない。
# 手元で書き換えるのは、枝を見るために取り直す origin/* だけ（git fetch --prune）。
#
# 毎回組み立て直していた問い合わせを 1 つにした。issue を並べる前に見るべきなのは
# issue そのものより、それを閉じる PR と、PR の無い枝のほう。PR の出ている issue を
# 未着手として並べ、同じ修正を別の枝でやり直しかけたことがある。
#
# 使い方: .claude/skills/triage/issues.sh
set -euo pipefail

# issue と PR を見るリポジトリ。**決め打ちにする。** gh の既定（GH_REPO や手元の
# checkout）から引くと、fork の checkout では fork の PR と issue が出て、/triage が
# その番号を etoki の issue として閉じる候補に並べる。以下の gh には -R で渡す。
nwo="yusuke0610/etoki"
# **origin が etoki の checkout だけを扱う。** fork を origin にすると、枝は fork、
# PR と issue は etoki から取ることになり、PR の無い枝も、PR が main に届いたか
# （SKILL.md の origin/main）も etoki と食い違う。違っていたら止める。
origin_nwo="$(gh repo view "$(git remote get-url origin)" --json nameWithOwner -q .nameWithOwner)"
if [ "$origin_nwo" != "$nwo" ]; then
	echo "origin（${origin_nwo}）が ${nwo} ではない。origin が ${nwo} の checkout で回す" >&2
	exit 1
fi

# gh pr list は --limit で打ち切り、打ち切ったことを言わない。打ち切られた PR が
# 閉じる issue は「PR が無い」に、その枝は「PR の無い枝」に見えるので、上限に
# 届いたら止める。下の 2 か所の一覧は同じ上限で取る。
pr_limit=1000
if [ "$(gh pr list -R "$nwo" --state open --limit "$pr_limit" --json number --jq length)" -ge "$pr_limit" ]; then
	echo "開いている PR が ${pr_limit} 件に届いた。pr_limit を上げてやり直す" >&2
	exit 1
fi

# 閉じるキーワード（Closes #N など）。別のリポジトリの issue も閉じられるので、
# そちらは owner/name#N で出す。番号だけにすると、このリポジトリの同じ番号と
# 取り違える。$nwo は下の --jq の式の中で定義する jq の変数。
closing_re='(?i)\\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)(?::\\s*|\\s+)(?:([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+))?#([0-9]+)'
# shellcheck disable=SC2016
closing_refs='scan("'"$closing_re"'") | if .[0] == null or (.[0] | ascii_downcase) == ($nwo | ascii_downcase) then "#\(.[1])" else "\(.[0])#\(.[1])" end'

echo "== 開いている PR と、閉じる issue（closes=GitHub が結んだもの、refs=本文の閉じるキーワード）"
# closingIssuesReferences は base が既定の枝（main）の PR でしか埋まらない。積んだ
# PR は本文の Closes #N から拾い、refs= で出す（マージしても、main に届くまで
# issue は閉じない）。拾わないと、その issue を「着手できる」と並べてしまう。
# gh の --jq は --arg を取らないので、このリポジトリの名前は式に埋め込む（owner/name
# に引用符は入らない）。
# $nwo / $repo は jq の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
gh pr list -R "$nwo" --state open --limit "$pr_limit" \
	--json number,title,baseRefName,headRefName,isDraft,mergeable,closingIssuesReferences,body \
	--jq '"'"$nwo"'" as $nwo | .[]
	      | ([.body | '"$closing_refs"'] | unique) as $refs
	      | "#\(.number) base=\(.baseRefName) "
	      + (if .baseRefName == "main"
	         then "closes=\([.closingIssuesReferences[]
	                        | (.repository.owner.login + "/" + .repository.name) as $repo
	                        | if $repo == $nwo then "#\(.number)" else "\($repo)#\(.number)" end]
	                       | join(",")) "
	         else "" end)
	      + (if ($refs | length) > 0 then "refs=\($refs | join(",")) " else "" end)
	      + "draft=\(.isDraft) \(.mergeable) \(.headRefName)"'

echo
echo "== 開いている PR のコミットに書いた閉じるキーワード"
# コミットに書いた Fixes #N は、マージで issue を閉じるのに、GitHub は PR に結ばない。
# closes= にも、issue の側の prs= にも出ないので、ここで拾う。gh pr list の
# --json commits は作者まで深く取り、GitHub の問い合わせの上限を超えるので、
# 本文だけを GraphQL で取る。
# **100 を超える PR は、その PR だけコミットを最後まで引き直す。** 一覧の問い合わせは
# PR ごとに先頭 100 件しか取れない（入れ子の接続は --paginate が辿らない）。残りの
# コミットにだけ閉じるキーワードがあると、その issue は prs= にも refs= にも出ず、
# /triage・/spec・/file-issues が「PR が無い」と読む。取れなければ set -e で止まる。
# $owner / $name / $endCursor / $n は GraphQL の変数、$nwo は jq の変数。
# shellcheck disable=SC2016
commit_refs="$(gh api graphql --paginate -F owner="${nwo%%/*}" -F name="${nwo##*/}" -f query='
query($owner:String!,$name:String!,$endCursor:String){
  repository(owner:$owner,name:$name){
    pullRequests(states:OPEN, first:50, after:$endCursor){
      pageInfo{ hasNextPage endCursor }
      nodes{ number commits(first:100){ totalCount nodes{ commit{ message } } } }
    }
  }
}' --jq '"'"$nwo"'" as $nwo | .data.repository.pullRequests.nodes[]
	| if .commits.totalCount > 100 then "more \(.number)"
	  else ([.commits.nodes[].commit.message | '"$closing_refs"'] | unique) as $refs
	       | select(($refs | length) > 0) | "#\(.number) refs=\($refs | join(","))" end')"
while read -r line; do
	case "$line" in
	"") ;;
	"more "*)
		n="${line#more }"
		# shellcheck disable=SC2016
		refs="$(gh api graphql --paginate -F owner="${nwo%%/*}" -F name="${nwo##*/}" -F n="$n" -f query='
query($owner:String!,$name:String!,$n:Int!,$endCursor:String){
  repository(owner:$owner,name:$name){
    pullRequest(number:$n){
      commits(first:100, after:$endCursor){ pageInfo{ hasNextPage endCursor } nodes{ commit{ message } } }
    }
  }
}' --jq '"'"$nwo"'" as $nwo | .data.repository.pullRequest.commits.nodes[].commit.message | '"$closing_refs"'' | sort -u | paste -sd, -)"
		if [ -n "$refs" ]; then
			echo "#${n} refs=${refs}"
		fi
		;;
	*) echo "$line" ;;
	esac
done <<<"$commit_refs"

# sub-issue の進み（subIssuesSummary）と親は REST の一覧に無いので GraphQL で取る。
# 100 件を超える分は --paginate で回る（変数名は $endCursor でなければならない）。
# $owner / $name / $endCursor は GraphQL の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
query='
query($owner:String!,$name:String!,$endCursor:String){
  repository(owner:$owner,name:$name){
    issues(states:OPEN, first:100, after:$endCursor, orderBy:{field:CREATED_AT, direction:ASC}){
      pageInfo{ hasNextPage endCursor }
      nodes{ number title parent{ number repository{ nameWithOwner } } subIssuesSummary{ total completed }
             closedByPullRequestsReferences(first:50, includeClosedPrs:false){ totalCount nodes{ number repository{ nameWithOwner } } }
             issueDependenciesSummary{ blockedBy }
             blockedBy(first:50){ nodes{ number state repository{ nameWithOwner } } }
             linkedBranches(first:50){ totalCount nodes{ ref{ name repository{ nameWithOwner } } } } }
    }
  }
}'

echo
echo "== 開いている issue（sub=閉じた数/全体、parent=親、blocked=開いている blocked by、prs=閉じる開いた PR、branches=Development 欄で結んだ枝）"
# 親は別のリポジトリの issue でもよい。そちらは owner/name#N で出す（閉じる issue と
# 同じ理由）。
# prs= は issue の側から引く。Development 欄で手で結んだ PR と、別のリポジトリの PR は
# 上の PR の一覧に出ない。枝も別のリポジトリのものを結べるので、そちらは
# owner/name:枝 で出す。名前だけにすると、origin の同じ名前の枝と取り違える。
# blocked= は issue の依存（blocked by）のうち開いているもの。本文に依存を書かず、
# そこにだけ結んだ issue もあり、出さないと「着手できる」と並べてしまう。
# issue ごとの一覧（prs= / blocked= / branches=）は先頭 50 件まで取る。取りきれ
# なかったら「ほか N 件」と出す。黙って落とすと、残りに PR・依存・枝があっても
# 無いように見える。開いている依存の数は issueDependenciesSummary から取る。
# shellcheck disable=SC2016
gh api graphql --paginate -f query="$query" -F owner="${nwo%%/*}" -F name="${nwo##*/}" \
	--jq 'def more($total; $shown): if $total > $shown then "（ほか \($total - $shown) 件）" else "" end;
	      "'"$nwo"'" as $nwo | .data.repository.issues.nodes[]
	      | "#\(.number) "
	        + (if .subIssuesSummary.total > 0 then "sub=\(.subIssuesSummary.completed)/\(.subIssuesSummary.total) " else "" end)
	        + (if .parent == null then ""
	           elif .parent.repository.nameWithOwner == $nwo then "parent=#\(.parent.number) "
	           else "parent=\(.parent.repository.nameWithOwner)#\(.parent.number) " end)
	        + ([.blockedBy.nodes[] | select(.state == "OPEN")
	            | if .repository.nameWithOwner == $nwo then "#\(.number)"
	              else "\(.repository.nameWithOwner)#\(.number)" end] as $blocked
	           | if .issueDependenciesSummary.blockedBy > 0
	             then "blocked=\($blocked | join(","))\(more(.issueDependenciesSummary.blockedBy; $blocked | length)) "
	             else "" end)
	        + (if .closedByPullRequestsReferences.totalCount > 0
	           then "prs=\([.closedByPullRequestsReferences.nodes[]
	                       | if .repository.nameWithOwner == $nwo then "#\(.number)"
	                         else "\(.repository.nameWithOwner)#\(.number)" end] | join(","))\(more(.closedByPullRequestsReferences.totalCount; .closedByPullRequestsReferences.nodes | length)) "
	           else "" end)
	        + (if .linkedBranches.totalCount > 0
	           then "branches=\([.linkedBranches.nodes[].ref // empty
	                            | if .repository.nameWithOwner == $nwo then .name
	                              else "\(.repository.nameWithOwner):\(.name)" end] | join(","))\(more(.linkedBranches.totalCount; .linkedBranches.nodes | length)) "
	           else "" end)
	        + .title'

echo
echo "== PR の無い枝（新しい順、全部）"
# 自動削除が有効なので、マージ済みの枝は残っていない。残っているのは、開いている
# PR の枝か、PR を出していない枝（クラウドのセッションが push したままのものなど）。
git fetch -q --prune origin
# 照らすのは、head が origin にある PR の枝だけ。別の fork の PR が同じ名前の枝を
# 使っていても、origin のその枝は PR の無い枝のまま残す。head の fork が消えた PR は
# head が null。
open_heads="$(gh pr list -R "$nwo" --state open --limit "$pr_limit" --json headRefName,headRepository \
	--jq '.[] | select(.headRepository.nameWithOwner == "'"$origin_nwo"'") | .headRefName')"
git for-each-ref --sort=-committerdate \
	--format='%(refname:lstrip=3)%09%(committerdate:short)%09%(authorname)' refs/remotes/origin |
	while IFS=$'\t' read -r branch date author; do
		case "$branch" in HEAD | main) continue ;; esac
		grep -qxF -- "$branch" <<<"$open_heads" || printf '%s %s %s\n' "$branch" "$date" "$author"
	done
# 打ち切らない。古い枝を落とすと、その枝が手を付けている issue を「着手できる」と
# 並べてしまう。自動削除が有効なので、ふつうは多くならない。
