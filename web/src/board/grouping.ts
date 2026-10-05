import type { BoardSummary } from "../api/types";

/**
 * ボードを作成先でまとめ直す（ADR 0019）。
 *
 * **これは実体の包含ではなく射影。** 利用者とボードは多対多だし、Projects v2 は
 * リポジトリに含まれるのではなくリンクされるだけ。1 つの Project に複数のボードが
 * ぶら下がる。節に分けるのは見せ方の選択であって、GitHub の構造を写したもの
 * ではない。
 */

/**
 * 一覧の 1 節。作成先 1 つ（リポジトリ × Project）と、そこに作るボード（#200）。
 *
 * **節は平らに並べる。** 木（リポジトリ → Project）にしていたころは、リポジトリの
 * 枝が 1 段増えるぶん畳む口が要った。カードの格子にすると 1 つの節は 1〜2 行に
 * 収まるので、見出しに両方を書けば足りる。
 */
export type BoardSection<T extends BoardSummary> = {
  /** 節を見分ける値。同じ Project が別のリポジトリにもリンクされうるので両方で作る。 */
  key: string;
  /** 見出し。`acme/web › #1 ロードマップ`、未選択の節は `UNSELECTED_LABEL`。 */
  heading: string;
  /** 作成先が選ばれているかどうか。false は移行前のボードだけ（ADR 0017）。 */
  selected: boolean;
  boards: T[];
};

/** 作成先が未選択のボードをまとめる節の見出し。 */
export const UNSELECTED_LABEL = "作成先が未選択";

/**
 * Project の表示名を組み立てる。
 *
 * 番号と名前は作成先を選んだ時点のスナップショットで、取れていないボードが
 * ある（ADR 0019）。そのときは node ID（`PVT_kwDO...`）を出しても読めないので、
 * 名前が無いことをそのまま書く。
 */
export function projectLabel(board: BoardSummary): string {
  const { projectNumber: number, projectTitle: title } = board;
  if (title === "") return "名称未取得のプロジェクト";
  return number > 0 ? `#${number} ${title}` : title;
}

/**
 * 一覧を作成先ごとの節にまとめる。
 *
 * **並べ替えはしない。** 入力の順（API は updatedAt の降順で返す）をそのまま
 * 保つ。節はリポジトリが最初に現れた順に並べ、同じリポジトリの Project はその
 * 中で最初に現れた順に続ける。名前順にすると、使っていないリポジトリが頭に
 * 居座る。作成先が未選択のボードは末尾に 1 つの節としてまとめる。
 *
 * 型は呼ぶ側の要素のまま返す。一覧の要素（`BoardListEntry`）が持つ件数を、
 * まとめ直しで落とさないため。
 */
export function boardSections<T extends BoardSummary>(boards: T[]): BoardSection<T>[] {
  // リポジトリごとに、Project の節を最初に現れた順で持つ。
  const repositories = new Map<string, Map<string, BoardSection<T>>>();
  const unselected: T[] = [];

  for (const board of boards) {
    if (board.projectId === "") {
      unselected.push(board);
      continue;
    }

    const repository = `${board.repositoryOwner}/${board.repositoryName}`;
    let projects = repositories.get(repository);
    if (!projects) {
      projects = new Map();
      repositories.set(repository, projects);
    }

    const section = projects.get(board.projectId);
    if (section) {
      section.boards.push(board);
      continue;
    }
    projects.set(board.projectId, {
      key: `${repository}/${board.projectId}`,
      heading: `${repository} › ${projectLabel(board)}`,
      selected: true,
      boards: [board],
    });
  }

  const out = [...repositories.values()].flatMap((projects) => [...projects.values()]);
  if (unselected.length > 0) {
    out.push({ key: "", heading: UNSELECTED_LABEL, selected: false, boards: unselected });
  }
  return out;
}
