import { describe, expect, it } from "vitest";

import type { BoardSummary } from "../api/types";
import { boardSections, projectLabel } from "./grouping";

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
    projectUrl: "https://github.com/orgs/acme/projects/1",
    ...target,
  };
}

/** 節を「見出し: ボード名」の組に潰す。 */
function shape(sections: ReturnType<typeof boardSections>) {
  return sections.map((s) => ({
    heading: s.heading,
    boards: s.boards.map((b) => b.name),
  }));
}

const unselected = {
  repositoryOwner: "",
  repositoryName: "",
  projectId: "",
  projectNumber: 0,
  projectTitle: "",
};

describe("boardSections", () => {
  it("同じ Project のボードを 1 つの節にまとめ、見出しにリポジトリと Project を書く", () => {
    expect(shape(boardSections([board("a"), board("b")]))).toEqual([
      { heading: "acme/web › #1 ロードマップ", boards: ["a", "b"] },
    ]);
  });

  it("同じリポジトリでも Project が違えば節を分ける", () => {
    const sections = boardSections([
      board("a"),
      board("b", { projectId: "PVT_2", projectNumber: 2, projectTitle: "技術的負債" }),
    ]);

    expect(shape(sections)).toEqual([
      { heading: "acme/web › #1 ロードマップ", boards: ["a"] },
      { heading: "acme/web › #2 技術的負債", boards: ["b"] },
    ]);
  });

  // 並びは名前順にしない。API は updatedAt の降順で返すので、その順を保つと
  // 最近さわったリポジトリが上に来る。名前順にすると、使っていないものが
  // 頭に居座る。
  it("入力の順を保ち、最初に現れた順にリポジトリを並べる", () => {
    const sections = boardSections([
      board("z", { repositoryName: "zebra", projectId: "PVT_9" }),
      board("a"),
      board("z2", { repositoryName: "zebra", projectId: "PVT_9" }),
    ]);

    expect(shape(sections)).toEqual([
      { heading: "acme/zebra › #1 ロードマップ", boards: ["z", "z2"] },
      { heading: "acme/web › #1 ロードマップ", boards: ["a"] },
    ]);
  });

  // 節を Project の最初の出現順だけで並べると、同じリポジトリの節が
  // 別のリポジトリをはさんで離れる。
  it("同じリポジトリの Project は、間に別のリポジトリが現れても並べて置く", () => {
    const sections = boardSections([
      board("a"),
      board("other", { repositoryName: "infra", projectId: "PVT_8" }),
      board("b", { projectId: "PVT_2", projectNumber: 2, projectTitle: "技術的負債" }),
    ]);

    expect(shape(sections).map((s) => s.heading)).toEqual([
      "acme/web › #1 ロードマップ",
      "acme/web › #2 技術的負債",
      "acme/infra › #1 ロードマップ",
    ]);
  });

  it("owner が違えば別のリポジトリとして分ける", () => {
    const sections = boardSections([
      board("a"),
      board("b", { repositoryOwner: "other" }),
    ]);

    expect(shape(sections).map((s) => s.heading)).toEqual([
      "acme/web › #1 ロードマップ",
      "other/web › #1 ロードマップ",
    ]);
  });

  // 1 つの Project は複数のリポジトリにリンクできる。key を Project だけで
  // 作ると、2 つの節が同じ key になって React が取り違える。
  it("同じ Project でもリポジトリが違えば、key も分ける", () => {
    const sections = boardSections([board("a"), board("b", { repositoryName: "api" })]);

    expect(new Set(sections.map((s) => s.key)).size).toBe(2);
  });

  // 作成先が未選択なのは移行前のボードだけ（ADR 0017）。新しいものを上に
  // 出すより、いま作れないものを末尾にまとめるほうが読みやすい。
  it("作成先が未選択のボードは末尾に 1 つの節にまとめる", () => {
    const sections = boardSections([
      board("old", unselected),
      board("a"),
      board("old2", unselected),
    ]);

    expect(shape(sections)).toEqual([
      { heading: "acme/web › #1 ロードマップ", boards: ["a"] },
      { heading: "作成先が未選択", boards: ["old", "old2"] },
    ]);
    expect(sections.map((s) => s.selected)).toEqual([true, false]);
  });

  it("空の一覧は空を返す", () => {
    expect(boardSections([])).toEqual([]);
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
