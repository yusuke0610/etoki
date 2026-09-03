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
gh pr view "$pr" --json reviews \
	--jq '[.reviews[] | select(.author.login | test("coderabbit"))]
	      | "CodeRabbit review \(length) 件 / 最新 \(map(.submittedAt) | max // "なし")"'

# スレッドの先頭コメントだけを取る。返信は CodeRabbit の確認応答か、こちらが
# 返した理由で、どちらも決着済みの記録である。解決済みのスレッドも落とす。
# $owner / $name / $pr は GraphQL の変数で、シェルに展開させてはいけない。
# shellcheck disable=SC2016
threads="$(gh api graphql -f query='
query($owner:String!,$name:String!,$pr:Int!){
  repository(owner:$owner,name:$name){
    pullRequest(number:$pr){
      reviewThreads(first:100){
        nodes{ id isResolved path line
               comments(first:1){ nodes{ author{login} body } } }
      }
    }
  }
}' -F owner="${nwo%%/*}" -F name="${nwo##*/}" -F pr="$pr" \
	--jq '.data.repository.pullRequest.reviewThreads.nodes[]
	      | select(.isResolved | not)
	      | select(.comments.nodes[0].author.login | test("coderabbit"))
	      | "\n--- \(.path):\(.line // "?")  thread=\(.id) ---\n\(.comments.nodes[0].body)"' |
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
