import { describe, expect, it } from "vitest";

import type { BoardSummary } from "../api/types";
import { projectLink } from "./projectLink";

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
