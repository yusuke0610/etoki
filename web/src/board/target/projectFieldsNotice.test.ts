import { describe, expect, it } from "vitest";

import { missingProjectFieldsMessage } from "../../api/errorMessage";
import { projectFieldsNotice } from "./projectFieldsNotice";

describe("projectFieldsNotice", () => {
  it("確かめている最中はそう言う", () => {
    expect(projectFieldsNotice(null)).toContain("確かめています");
  });

  it("揃っていればそう言う", () => {
    expect(
      projectFieldsNotice({ projectId: "P", status: "ok", missingFields: [] }),
    ).toContain("揃っています");
  });

  // 足りないときの文は、押した後の文と同じ部品から組む（ADR 0030 / 0088）。
  // 切れると: ここだけで書いた文が、押した後の文と別々に古くなる。
  it("足りなければ、失敗の文言と同じ部品から引く", () => {
    expect(
      projectFieldsNotice({
        projectId: "P",
        status: "missing",
        missingFields: ["parent"],
      }),
    ).toBe(missingProjectFieldsMessage(["parent"]));
  });

  // 確かめられなかったことを、足りない・揃っているのどちらにも倒さない（ADR 0017）。
  it("確かめられなければ、どちらとも言わずに選べることを言う", () => {
    const notice = projectFieldsNotice({
      projectId: "P",
      status: "unknown",
      missingFields: [],
    });

    expect(notice).toContain("確かめられませんでした");
    expect(notice).toContain("このまま選べます");
    expect(notice).not.toContain("揃っています");
    expect(notice).not.toContain("ありません");
  });
});
