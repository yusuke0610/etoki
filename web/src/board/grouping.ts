import type { BoardSummary } from "../api/types";

/**
 * ボードを作成先でまとめ直す（ADR 0019）。
 *
 * **これは実体の包含ではなく射影。** 利用者とボードは多対多だし、Projects v2 は
 * リポジトリに含まれるのではなくリンクされるだけ。1 つの Project に複数のボードが
 * ぶら下がる。木にするのは見せ方の選択であって、GitHub の構造を写したものではない。
 */

/** 1 つの Project と、そこに作るボード。 */
export type ProjectGroup = {
  projectId: string;
  /** 見出しに出す名前。`projectLabel` の結果。 */
  label: string;
  boards: BoardSummary[];
};

/** 1 つのリポジトリと、その中の Project。 */
export type RepositoryGroup = {
  repositoryOwner: string;
  repositoryName: string;
  /** 作成先が選ばれているかどうか。false は移行前のボードだけ（ADR 0017）。 */
  selected: boolean;
  projects: ProjectGroup[];
};

/** 作成先が未選択のボードをまとめる枝の見出し。 */
export const UNSELECTED_LABEL = "作成先なし";

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
 * 一覧をリポジトリ → Project の 2 段にまとめる。
 *
 * **並べ替えはしない。** 入力の順（API は updatedAt の降順で返す）をそのまま
 * 保ち、枝は最初に現れた順に並べる。名前順にすると、使っていないリポジトリが
 * 頭に居座る。作成先が未選択のボードは末尾に 1 つの枝としてまとめる。
 */
export function groupBoards(boards: BoardSummary[]): RepositoryGroup[] {
  const groups = new Map<string, RepositoryGroup>();
  const unselected: BoardSummary[] = [];

  for (const board of boards) {
    if (board.projectId === "") {
      unselected.push(board);
      continue;
    }

    const key = `${board.repositoryOwner}/${board.repositoryName}`;
    let group = groups.get(key);
    if (!group) {
      group = {
        repositoryOwner: board.repositoryOwner,
        repositoryName: board.repositoryName,
        selected: true,
        projects: [],
      };
      groups.set(key, group);
    }

    const project = group.projects.find((p) => p.projectId === board.projectId);
    if (project) {
      project.boards.push(board);
      continue;
    }
    group.projects.push({
      projectId: board.projectId,
      label: projectLabel(board),
      boards: [board],
    });
  }

  const out = [...groups.values()];
  if (unselected.length > 0) {
    out.push({
      repositoryOwner: "",
      repositoryName: "",
      selected: false,
      projects: [{ projectId: "", label: UNSELECTED_LABEL, boards: unselected }],
    });
  }
  return out;
}
