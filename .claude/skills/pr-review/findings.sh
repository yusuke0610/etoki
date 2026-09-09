#!/usr/bin/env bash
# PR に付いた CodeRabbit の指摘のうち、まだ決着していないものだけを出す。
#
# comments API から全文を取ると 1 PR で 6 万文字に達するが、その大半は
# 「対応を確認しました」の返信と <details> に畳まれた Analysis chain で、
# 「直す」「理由を返す」を決めるのには使わない（#125）。
#
# 使い方: .claude/skills/pr-review/findings.sh <PR 番号>
set -euo pipefail

pr="${1:?PR 番号を渡す}"
nwo="$(gh repo view --json nameWithOwner -q .nameWithOwner)"

# レビューが来たかどうかを先に出す。指摘 0 件は「指摘が無い」ではなく
# 「まだ来ていない」でもありうるので、両者を区別できる材料を残す。
# author.login は bot アカウントの状態次第で null になりうるので、test() に
# 渡す前に空文字へ正規化する。そうしないと jq が型エラーで落ち、
# set -euo pipefail によりこの後の処理も止まる（#127）。
gh pr view "$pr" --json reviews \
	--jq '[.reviews[] | select((.author.login // "") | test("coderabbit"))]
	      | "CodeRabbit review \(length) 件 / 最新 \(map(.submittedAt) | max // "なし")"'

# スレッドの先頭コメントだけを取る。返信は CodeRabbit の確認応答か、こちらが
# 返した理由で、どちらも決着済みの記録である。解決済みのスレッドも落とす。
# reviewThreads は 1 ページ最大 100 件までしか返さないため、hasNextPage が
# 立っている間は endCursor を渡して全ページを回る。回さないと 101 件目以降の
# 未解決スレッドを取りこぼす（#127）。
# $owner / $name / $pr / $after は GraphQL の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
query='
query($owner:String!,$name:String!,$pr:Int!,$after:String){
  repository(owner:$owner,name:$name){
    pullRequest(number:$pr){
      reviewThreads(first:100, after:$after){
        pageInfo{ hasNextPage endCursor }
        nodes{ id isResolved path line
               comments(first:1){ nodes{ author{login} body } } }
      }
    }
  }
}'

nodes_file="$(mktemp)"
trap 'rm -f "$nodes_file"' EXIT
cursor=""
while :; do
	args=(-f query="$query" -F owner="${nwo%%/*}" -F name="${nwo##*/}" -F pr="$pr")
	if [ -n "$cursor" ]; then
		args+=(-F after="$cursor")
	fi
	page="$(gh api graphql "${args[@]}" --jq '.data.repository.pullRequest.reviewThreads')"
	jq -c '.nodes[]' <<<"$page" >>"$nodes_file"
	if [ "$(jq -r '.pageInfo.hasNextPage' <<<"$page")" != "true" ]; then
		break
	fi
	cursor="$(jq -r '.pageInfo.endCursor' <<<"$page")"
done
nodes="$(jq -sc '.' "$nodes_file")"

# author.login の null 正規化は上と同じ理由（#127）。
threads="$(jq -r '.[]
	      | select(.isResolved | not)
	      | select((.comments.nodes[0].author.login // "") | test("coderabbit"))
	      | "\n--- \(.path):\(.line // "?")  thread=\(.id) ---\n\(.comments.nodes[0].body)"' <<<"$nodes" |
	perl -0777 -pe '
	    s{<details>.*?</details>}{}gs;
	    s{</?details>|<summary>.*?</summary>}{}gs;
	    s{<!--.*?-->}{}gs;
	    s{\n{3,}}{\n\n}gs;')"

if [ -z "${threads//[[:space:]]/}" ]; then
	echo "未解決スレッド 0 件"
else
	echo "$threads"
fi
