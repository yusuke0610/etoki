import { describe, expect, it } from "vitest";

import type { BoardSummary } from "../api/types";
import { groupBoards, projectLabel } from "./grouping";

function board(name: string, target: Partial<BoardSummary> = {}): BoardSummary {
  return {
    id: name,
    name,
    role: "owner",
    createdAt: "2026-08-01T09:00:00Z",
    updatedAt: "2026-08-01T09:00:00Z",
    repositoryOwner: "acme",
    repositoryName: "web",
    projectId: "PVT_1",
    projectNumber: 1,
    projectTitle: "ロードマップ",
    ...target,
  };
}

/** 木を「リポジトリ / Project / ボード名」の入れ子配列に潰す。 */
function shape(groups: ReturnType<typeof groupBoards>) {
  return groups.map((g) => ({
    repository: g.selected ? `${g.repositoryOwner}/${g.repositoryName}` : "(未選択)",
    projects: g.projects.map((p) => ({
      label: p.label,
      boards: p.boards.map((b) => b.name),
    })),
  }));
}

describe("groupBoards", () => {
  it("同じ Project のボードを 1 つの枝にまとめる", () => {
    expect(shape(groupBoards([board("a"), board("b")]))).toEqual([
      {
        repository: "acme/web",
        projects: [{ label: "#1 ロードマップ", boards: ["a", "b"] }],
      },
    ]);
  });

  it("同じリポジトリでも Project が違えば枝を分ける", () => {
    const groups = groupBoards([
      board("a"),
      board("b", { projectId: "PVT_2", projectNumber: 2, projectTitle: "技術的負債" }),
    ]);

    expect(shape(groups)).toEqual([
      {
        repository: "acme/web",
        projects: [
          { label: "#1 ロードマップ", boards: ["a"] },
          { label: "#2 技術的負債", boards: ["b"] },
        ],
      },
    ]);
  });

  // 並びは名前順にしない。API は updatedAt の降順で返すので、その順を保つと
  // 最近さわったリポジトリが上に来る。名前順にすると、使っていないものが
  // 頭に居座る。
  it("入力の順を保ち、最初に現れた順にリポジトリを並べる", () => {
    const groups = groupBoards([
      board("z", { repositoryName: "zebra", projectId: "PVT_9" }),
      board("a"),
      board("z2", { repositoryName: "zebra", projectId: "PVT_9" }),
    ]);

    expect(shape(groups)).toEqual([
      {
        repository: "acme/zebra",
        projects: [{ label: "#1 ロードマップ", boards: ["z", "z2"] }],
      },
      {
        repository: "acme/web",
        projects: [{ label: "#1 ロードマップ", boards: ["a"] }],
      },
    ]);
  });

  it("owner が違えば別のリポジトリとして分ける", () => {
    const groups = groupBoards([board("a"), board("b", { repositoryOwner: "other" })]);

    expect(shape(groups).map((g) => g.repository)).toEqual(["acme/web", "other/web"]);
  });

  // 作成先が未選択なのは移行前のボードだけ（ADR 0017）。新しいものを上に
  // 出すより、いま作れないものを末尾にまとめるほうが読みやすい。
  it("作成先が未選択のボードは末尾に 1 つにまとめる", () => {
    const unselected = {
      repositoryOwner: "",
      repositoryName: "",
      projectId: "",
      projectNumber: 0,
      projectTitle: "",
    };
    const groups = groupBoards([
      board("old", unselected),
      board("a"),
      board("old2", unselected),
    ]);

    expect(shape(groups)).toEqual([
      {
        repository: "acme/web",
        projects: [{ label: "#1 ロードマップ", boards: ["a"] }],
      },
      {
        repository: "(未選択)",
        projects: [{ label: "作成先なし", boards: ["old", "old2"] }],
      },
    ]);
  });

  it("空の一覧は空を返す", () => {
    expect(groupBoards([])).toEqual([]);
  });
});

describe("projectLabel", () => {
  it("番号と名前で出す", () => {
    expect(projectLabel(board("a"))).toBe("#1 ロードマップ");
  });

  // 表示名はスナップショットなので、取れていないボードがある（ADR 0019）。
  // node ID を出しても読めないので、名前が無いことをそのまま書く。
  it("名前を取っていなければ、そう書く", () => {
    expect(projectLabel(board("a", { projectNumber: 0, projectTitle: "" }))).toBe(
      "名称未取得のプロジェクト",
    );
  });

  it("名前だけあれば番号は出さない", () => {
    expect(projectLabel(board("a", { projectNumber: 0 }))).toBe("ロードマップ");
  });
});
