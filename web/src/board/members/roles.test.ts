import { describe, expect, it } from "vitest";

import { canEditBoard, isOwner, ROLE_LABELS, roleOptions } from "./roles";

describe("roleOptions", () => {
  // 選択肢は表から並べる。別の配列で持つと、契約にロールを足したときに
  // 選択肢から黙って抜ける（#156）。
  it("表のロールを 1 つずつ、表の並びで返す", () => {
    expect(roleOptions()).toEqual(["owner", "editor", "viewer"]);
    expect(roleOptions()).toEqual(Object.keys(ROLE_LABELS));
  });
});

describe("canEditBoard", () => {
  // viewer は読むだけ。解釈も許さない（ADR 0017）。
  it("viewer だけが編集できない", () => {
    expect(canEditBoard("owner")).toBe(true);
    expect(canEditBoard("editor")).toBe(true);
    expect(canEditBoard("viewer")).toBe(false);
  });
});

describe("isOwner", () => {
  it("owner だけが真", () => {
    expect(isOwner("owner")).toBe(true);
    expect(isOwner("editor")).toBe(false);
    expect(isOwner("viewer")).toBe(false);
  });
});
