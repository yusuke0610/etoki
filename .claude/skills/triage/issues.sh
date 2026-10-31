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

echo "== 開いている PR と、閉じる issue"
gh pr list --state open --limit 100 \
	--json number,title,headRefName,isDraft,mergeable,closingIssuesReferences \
	--jq '.[] | "#\(.number) closes=\([.closingIssuesReferences[].number | "#\(.)"] | join(",")) draft=\(.isDraft) \(.mergeable) \(.headRefName)"'

# sub-issue の進み（subIssuesSummary）と親は REST の一覧に無いので GraphQL で取る。
# 100 件を超える分は --paginate で回る（変数名は $endCursor でなければならない）。
# $owner / $name / $endCursor は GraphQL の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
query='
query($owner:String!,$name:String!,$endCursor:String){
  repository(owner:$owner,name:$name){
    issues(states:OPEN, first:100, after:$endCursor, orderBy:{field:CREATED_AT, direction:ASC}){
      pageInfo{ hasNextPage endCursor }
      nodes{ number title parent{ number } subIssuesSummary{ total completed } }
    }
  }
}'

echo
echo "== 開いている issue（sub=閉じた数/全体、parent=親）"
gh api graphql --paginate -f query="$query" -F owner="${nwo%%/*}" -F name="${nwo##*/}" \
	--jq '.data.repository.issues.nodes[]
	      | "#\(.number) "
	        + (if .subIssuesSummary.total > 0 then "sub=\(.subIssuesSummary.completed)/\(.subIssuesSummary.total) " else "" end)
	        + (if .parent then "parent=#\(.parent.number) " else "" end)
	        + .title'

echo
echo "== PR の無い枝（新しい順に 15 本）"
# 自動削除が有効なので、マージ済みの枝は残っていない。残っているのは、開いている
# PR の枝か、PR を出していない枝（クラウドのセッションが push したままのものなど）。
git fetch -q --prune origin
open_heads="$(gh pr list --state open --limit 100 --json headRefName --jq '.[].headRefName')"
git for-each-ref --sort=-committerdate \
	--format='%(refname:lstrip=3)%09%(committerdate:short)%09%(authorname)' refs/remotes/origin |
	while IFS=$'\t' read -r branch date author; do
		case "$branch" in HEAD | main) continue ;; esac
		grep -qxF -- "$branch" <<<"$open_heads" || printf '%s %s %s\n' "$branch" "$date" "$author"
	done |
	# head で打ち切ると、残りを書こうとした while が SIGPIPE で落ち、pipefail で
	# スクリプトごと失敗する。awk は最後まで読んでから捨てる。
	awk 'NR <= 15'
