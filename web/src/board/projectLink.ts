import type { BoardSummary } from "../api/types";

/**
 * 作成先 Project へのリンクを組む（ADR 0025）。
 *
 * draft issue の作成は取り消せない（ADR 0009）。取り消せない操作の結果を
 * 確かめられないと、「途中で失敗しました（3 件は作成済み）」と出たときに
 * 何が作られたのかを見にいけない。その導線がこれ。
 *
 * **URL は組み立てず、保存されているものを使う。** Projects v2 の URL は
 * `/orgs/{owner}/projects/{n}` と `/users/{owner}/projects/{n}` に分かれるが、
 * etoki が持っているのは `repositoryOwner` の文字列だけで、どちらの形になるかを
 * 決める材料が無い。決め打つと外したほうのボードで 404 になる。確認のための
 * 導線が 404 を返すのは、リンクが無いより悪い。
 */

/**
 * 作成先へのリンク 1 本。
 *
 * `exact` は Project そのものに着地するかどうか。false のときはリポジトリの
 * Projects タブ止まりで、そこから 1 つ選ぶ必要がある。**呼び出し側はこれを
 * 隠さない。** Project へ飛ぶと言って一覧に着地させると、リンクの約束が崩れる。
 */
export type ProjectLink = {
  href: string;
  exact: boolean;
};

/**
 * 作成先へのリンクを返す。組めなければ null。
 *
 * 順に、
 *
 * 1. 作成先が未選択なら null。移行前のボードが該当する（ADR 0017）。
 * 2. 保存された `projectUrl` があればそれ。GitHub が返したものなので正確。
 * 3. 無ければリポジトリの Projects タブ。`projectUrl` を保存する前に作成先を
 *    選んだボードが該当する。**番号からは組み立てない。**
 */
export function projectLink(target: BoardSummary): ProjectLink | null {
  const { repositoryOwner: owner, repositoryName: name, projectId } = target;

  // 判定に使うのは projectId / owner / name の 3 つだけ。表示用のスナップ
  // ショットは作成先が選ばれているかどうかを決めない（ADR 0019）。
  if (projectId === "" || owner === "" || name === "") return null;

  if (target.projectUrl !== "") {
    return { href: target.projectUrl, exact: true };
  }

  return {
    href: `https://github.com/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/projects`,
    exact: false,
  };
}
