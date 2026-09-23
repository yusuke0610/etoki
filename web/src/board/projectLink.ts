import type { BoardSummary, SyncItem } from "../api/types";

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

/**
 * 作成した draft issue 1 件へのリンクを返す。組めなければ null（ADR 0057）。
 *
 * draft issue に固有の URL は無い。Project の URL に `pane=issue&itemId=` を
 * 添えると、Project を開いたうえでその item のペインが開く。
 *
 * **知らない部分は組み立てない**（ADR 0025 の線はそのまま）。土台にするのは
 * GitHub が返した Project の URL だけで、それが無い（`exact` でない）ときは
 * null。リポジトリの Projects タブに `itemId` を足しても item は開かない。
 * 呼び出し側はそのとき、リストごとの 1 本（`projectLink`）だけを出す。
 *
 * 識別子は `0`（知らない）か、JavaScript の数値で正確に表せないときも null。
 * 丸められた値は別の item を指しうるので、リンクが無いより悪い。
 *
 * 文字列連結ではなく `URL` で組む。保存された URL に既にクエリや fragment が
 * 付いていても壊れない。
 */
export function projectItemLink(
  link: ProjectLink | null,
  item: Pick<SyncItem, "itemDatabaseId">,
): string | null {
  if (link === null || !link.exact) return null;

  const id = item.itemDatabaseId ?? 0;
  if (!Number.isSafeInteger(id) || id <= 0) return null;

  let url: URL;
  try {
    url = new URL(link.href);
  } catch {
    return null;
  }
  url.searchParams.set("pane", "issue");
  url.searchParams.set("itemId", String(id));
  return url.toString();
}
