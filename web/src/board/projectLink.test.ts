import { describe, expect, it } from "vitest";

import type { BoardSummary } from "../api/types";
import { projectItemLink, projectLink } from "./projectLink";

function target(over: Partial<BoardSummary> = {}): BoardSummary {
  return {
    id: "board-1",
    name: "b",
    role: "owner",
    createdAt: "2026-08-01T09:00:00Z",
    updatedAt: "2026-08-01T09:00:00Z",
    repositoryOwner: "acme",
    repositoryName: "web",
    projectId: "PVT_1",
    projectNumber: 1,
    projectTitle: "ロードマップ",
    projectUrl: "https://github.com/orgs/acme/projects/1",
    ...over,
  };
}

describe("projectLink", () => {
  it("保存された URL があればそれを使う", () => {
    expect(projectLink(target())).toEqual({
      href: "https://github.com/orgs/acme/projects/1",
      exact: true,
    });
  });

  // 保存されているのは GitHub が返した URL なので、user 所有でも org 所有でも
  // そのまま通る。etoki 側はどちらなのかを知らないままでよい（ADR 0025）。
  it("user 所有の Project の URL もそのまま通す", () => {
    const link = projectLink(
      target({ projectUrl: "https://github.com/users/yusuke0610/projects/4" }),
    );
    expect(link).toEqual({
      href: "https://github.com/users/yusuke0610/projects/4",
      exact: true,
    });
  });

  // URL を保存する前に作成先を選んだボード。番号は持っているが、そこから
  // 組み立てると owner の種別を当てにいくことになるので使わない。
  it("URL が無ければリポジトリの Projects タブに落とす", () => {
    expect(projectLink(target({ projectUrl: "" }))).toEqual({
      href: "https://github.com/acme/web/projects",
      exact: false,
    });
  });

  it("番号があっても URL が無ければ組み立てない", () => {
    const link = projectLink(target({ projectUrl: "", projectNumber: 7 }));
    expect(link?.href).not.toContain("projects/7");
  });

  // 移行前のボード（ADR 0017）。作成先が無いので飛び先も無い。
  it("作成先が未選択ならリンクを出さない", () => {
    expect(
      projectLink(
        target({
          repositoryOwner: "",
          repositoryName: "",
          projectId: "",
          projectUrl: "",
        }),
      ),
    ).toBeNull();
  });

  // projectId だけあってリポジトリが空、という壊れた組み合わせ。フォール
  // バック先が組めないので、`https://github.com//projects` を出さずに黙る。
  it("リポジトリが欠けていればリンクを出さない", () => {
    expect(projectLink(target({ repositoryName: "", projectUrl: "" }))).toBeNull();
  });

  it("リポジトリ名に記号があってもエスケープする", () => {
    const link = projectLink(
      target({ repositoryOwner: "a/b", repositoryName: "c d", projectUrl: "" }),
    );
    expect(link?.href).toBe("https://github.com/a%2Fb/c%20d/projects");
  });
});

describe("projectItemLink", () => {
  const exact = { href: "https://github.com/orgs/acme/projects/1", exact: true };

  it("Project の URL に item のペインを開くクエリを足す", () => {
    expect(projectItemLink(exact, { itemDatabaseId: 123456789 })).toBe(
      "https://github.com/orgs/acme/projects/1?pane=issue&itemId=123456789",
    );
  });

  // 識別子を知らない item（移行前の run、GitHub が返さなかった run）。
  it("識別子が 0 か省略ならリンクを出さない", () => {
    expect(projectItemLink(exact, { itemDatabaseId: 0 })).toBeNull();
    expect(projectItemLink(exact, {})).toBeNull();
  });

  // 丸められた値は別の item を指しうる。リンクが無いより悪い。
  it("数値として正確に表せない識別子ならリンクを出さない", () => {
    expect(
      projectItemLink(exact, { itemDatabaseId: Number.MAX_SAFE_INTEGER + 2 }),
    ).toBeNull();
    expect(projectItemLink(exact, { itemDatabaseId: 1.5 })).toBeNull();
    expect(projectItemLink(exact, { itemDatabaseId: -3 })).toBeNull();
  });

  // Project の URL を知らない（リポジトリの Projects タブに落ちている）なら、
  // 土台を組み立て直さない（ADR 0025）。タブに itemId を足しても item は開かない。
  it("Project そのものの URL でなければリンクを出さない", () => {
    expect(
      projectItemLink(
        { href: "https://github.com/acme/web/projects", exact: false },
        { itemDatabaseId: 1 },
      ),
    ).toBeNull();
    expect(projectItemLink(null, { itemDatabaseId: 1 })).toBeNull();
  });

  // 保存された URL に既にクエリや fragment があっても、文字列連結で壊さない。
  it("既存のクエリと fragment を保つ", () => {
    const href = projectItemLink(
      {
        href: "https://github.com/users/u/projects/4/views/2?layout=board#top",
        exact: true,
      },
      { itemDatabaseId: 9 },
    );
    const url = new URL(href ?? "");
    expect(url.pathname).toBe("/users/u/projects/4/views/2");
    expect(url.searchParams.get("layout")).toBe("board");
    expect(url.searchParams.get("pane")).toBe("issue");
    expect(url.searchParams.get("itemId")).toBe("9");
    expect(url.hash).toBe("#top");
  });

  // 既に pane や itemId が付いた URL を控えていても、別の item を指さない。
  it("既存の itemId は上書きする", () => {
    const href = projectItemLink(
      {
        href: "https://github.com/orgs/acme/projects/1?pane=issue&itemId=5",
        exact: true,
      },
      { itemDatabaseId: 7 },
    );
    expect(new URL(href ?? "").searchParams.getAll("itemId")).toEqual(["7"]);
  });
});
