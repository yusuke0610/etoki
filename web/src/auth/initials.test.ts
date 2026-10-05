import { describe, expect, it } from "vitest";

import { initialsOf } from "./initials";

describe("initialsOf", () => {
  it.each([
    ["Octo Cat", "OC"],
    // 空白で分けないと 1 語なので 1 文字。
    ["山田太郎", "山"],
    ["山田 太郎", "山太"],
    // 3 語以上でも 2 文字まで。
    ["Mona Lisa Octocat", "ML"],
    ["octo cat", "OC"],
    // 前後と連続の空白で、空の語を数えない。
    ["  Octo   Cat  ", "OC"],
    // サロゲートペアを半分に割らない。
    ["𠮷野 家", "𠮷家"],
    // 大文字にすると 2 文字以上になる文字がある（ß → SS）。語ごとに 1 文字のまま。
    ["ß A", "SA"],
    ["", ""],
  ])("「%s」は「%s」", (name, want) => {
    expect(initialsOf(name)).toBe(want);
  });
});
