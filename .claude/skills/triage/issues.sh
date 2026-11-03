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

nwo="$(gh repo view --json nameWithOwner -q .nameWithOwner)"

# gh pr list は --limit で打ち切り、打ち切ったことを言わない。打ち切られた PR が
# 閉じる issue は「PR が無い」に、その枝は「PR の無い枝」に見えるので、上限に
# 届いたら止める。下の 2 か所の一覧は同じ上限で取る。
pr_limit=1000
if [ "$(gh pr list --state open --limit "$pr_limit" --json number --jq length)" -ge "$pr_limit" ]; then
	echo "開いている PR が ${pr_limit} 件に届いた。pr_limit を上げてやり直す" >&2
	exit 1
fi

echo "== 開いている PR と、閉じる issue（積んだ PR は refs=）"
# closingIssuesReferences は base が既定の枝（main）の PR でしか埋まらない。積んだ
# PR は本文の Closes #N から拾い、refs= で出す（マージしても、main に届くまで
# issue は閉じない）。拾わないと、その issue を「着手できる」と並べてしまう。
# 別のリポジトリの issue も閉じられるので、そちらは owner/name#N で出す。番号だけに
# すると、このリポジトリの同じ番号と取り違える。gh の --jq は --arg を取らないので、
# このリポジトリの名前は式に埋め込む（owner/name に引用符は入らない）。
# $nwo / $repo は jq の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
gh pr list --state open --limit "$pr_limit" \
	--json number,title,baseRefName,headRefName,isDraft,mergeable,closingIssuesReferences,body \
	--jq '"'"$nwo"'" as $nwo | .[] | "#\(.number) base=\(.baseRefName) "
	      + (if .baseRefName == "main"
	         then "closes=\([.closingIssuesReferences[]
	                        | (.repository.owner.login + "/" + .repository.name) as $repo
	                        | if $repo == $nwo then "#\(.number)" else "\($repo)#\(.number)" end]
	                       | join(","))"
	         else "refs=\([.body | scan("(?i)\\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)(?::\\s*|\\s+)(?:([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+))?#([0-9]+)")
		                       | if .[0] == null or (.[0] | ascii_downcase) == ($nwo | ascii_downcase) then "#\(.[1])" else "\(.[0])#\(.[1])" end] | unique | join(","))"
	         end)
	      + " draft=\(.isDraft) \(.mergeable) \(.headRefName)"'

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
             closedByPullRequestsReferences(first:10, includeClosedPrs:false){ nodes{ number repository{ nameWithOwner } } }
             linkedBranches(first:10){ nodes{ ref{ name repository{ nameWithOwner } } } } }
    }
  }
}'

echo
echo "== 開いている issue（sub=閉じた数/全体、parent=親、prs=閉じる開いた PR、branches=Development 欄で結んだ枝）"
# 親は別のリポジトリの issue でもよい。そちらは owner/name#N で出す（閉じる issue と
# 同じ理由）。
# prs= は issue の側から引く。Development 欄で手で結んだ PR と、別のリポジトリの PR は
# 上の PR の一覧に出ない。枝も別のリポジトリのものを結べるので、そちらは
# owner/name:枝 で出す。名前だけにすると、origin の同じ名前の枝と取り違える。
# shellcheck disable=SC2016
gh api graphql --paginate -f query="$query" -F owner="${nwo%%/*}" -F name="${nwo##*/}" \
	--jq '"'"$nwo"'" as $nwo | .data.repository.issues.nodes[]
	      | "#\(.number) "
	        + (if .subIssuesSummary.total > 0 then "sub=\(.subIssuesSummary.completed)/\(.subIssuesSummary.total) " else "" end)
	        + (if .parent == null then ""
	           elif .parent.repository.nameWithOwner == $nwo then "parent=#\(.parent.number) "
	           else "parent=\(.parent.repository.nameWithOwner)#\(.parent.number) " end)
	        + (if (.closedByPullRequestsReferences.nodes | length) > 0
	           then "prs=\([.closedByPullRequestsReferences.nodes[]
	                       | if .repository.nameWithOwner == $nwo then "#\(.number)"
	                         else "\(.repository.nameWithOwner)#\(.number)" end] | join(",")) " else "" end)
	        + (if (.linkedBranches.nodes | length) > 0
	           then "branches=\([.linkedBranches.nodes[].ref // empty
	                            | if .repository.nameWithOwner == $nwo then .name
	                              else "\(.repository.nameWithOwner):\(.name)" end] | join(",")) " else "" end)
	        + .title'

echo
echo "== PR の無い枝（新しい順、全部）"
# 自動削除が有効なので、マージ済みの枝は残っていない。残っているのは、開いている
# PR の枝か、PR を出していない枝（クラウドのセッションが push したままのものなど）。
git fetch -q --prune origin
# 照らすのは、このリポジトリから出した PR の枝だけ。fork の PR が同じ名前の枝を
# 使っていても、origin のその枝は PR の無い枝のまま残す。
open_heads="$(gh pr list --state open --limit "$pr_limit" --json headRefName,isCrossRepository \
	--jq '.[] | select(.isCrossRepository | not) | .headRefName')"
git for-each-ref --sort=-committerdate \
	--format='%(refname:lstrip=3)%09%(committerdate:short)%09%(authorname)' refs/remotes/origin |
	while IFS=$'\t' read -r branch date author; do
		case "$branch" in HEAD | main) continue ;; esac
		grep -qxF -- "$branch" <<<"$open_heads" || printf '%s %s %s\n' "$branch" "$date" "$author"
	done
# 打ち切らない。古い枝を落とすと、その枝が手を付けている issue を「着手できる」と
# 並べてしまう。自動削除が有効なので、ふつうは多くならない。
