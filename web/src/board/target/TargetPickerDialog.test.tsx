import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { log } from "../../app/logger";
import { TargetPickerDialog } from "./TargetPickerDialog";

/**
 * 作成先を選ぶダイアログ（ADR 0100）。開き方・閉じ方・焦点の戻り先は E2E
 * （`web/e2e/target.spec.ts`）が実ブラウザの `<dialog>` で見る。ここで押さえる
 * のは、中身が落ちてもダイアログの中で受け止めること（ADR 0027）。
 */

vi.mock("./RepositoryPicker", () => ({
  RepositoryPicker: () => {
    throw new Error("boom");
  },
}));

const showModal = Object.getOwnPropertyDescriptor(
  HTMLDialogElement.prototype,
  "showModal",
);

beforeEach(() => {
  // React は境界が受け止めた例外も console.error に出す。出力を汚さない。
  vi.spyOn(console, "error").mockImplementation(() => {});
  vi.spyOn(log, "error").mockImplementation(() => {});
  // jsdom は `<dialog>` のモーダル表示を持たない。開いた印だけ立てる。
  HTMLDialogElement.prototype.showModal = function (this: HTMLDialogElement) {
    this.open = true;
  };
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  if (showModal)
    Object.defineProperty(HTMLDialogElement.prototype, "showModal", showModal);
  else delete (HTMLDialogElement.prototype as Partial<HTMLDialogElement>).showModal;
});

describe("TargetPickerDialog", () => {
  // 切れると: 選択部品の例外が App の境界まで届き、保存していないキャンバスごと
  // 外れる（#481 の full review）。ダイアログはキャンバスの上に開くので、ほかの
  // ダイアログ（共有・ボードの背景）と同じく中身を 1 枚で包む。
  it("選択部品が落ちても、ダイアログの中で受け止める", () => {
    render(
      <TargetPickerDialog
        open
        boardName="ブレスト"
        onSelected={() => Promise.resolve()}
        onClose={() => {}}
      />,
    );

    expect(screen.getByRole("alert")).toHaveTextContent("この部分を表示できませんでした");
    expect(screen.getByRole("button", { name: "再表示" })).toBeInTheDocument();
  });
});
