import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

import { BoardStatus } from "./BoardStatus";

function props(
  overrides: Partial<ComponentProps<typeof BoardStatus>> = {},
): ComponentProps<typeof BoardStatus> {
  return {
    name: "ログイン機能のブレスト",
    nameDraft: null,
    onNameDraftChange: vi.fn(),
    renaming: false,
    onRename: vi.fn(),
    dirty: true,
    canEdit: true,
    onSave: vi.fn(),
    onEkidoki: vi.fn(),
    canSave: true,
    saveBlocked: null,
    saving: false,
    ...overrides,
  };
}

/** 右上の「保存」。名前の変更中に出る「名前を保存」と取り違えないよう完全一致で引く。 */
function saveButton() {
  return screen.queryByRole("button", { name: "保存" });
}

/**
 * 右上の「保存」（ADR 0089、#296）。
 *
 * **押しても絵解きの面を開かないことはここで見る。** E2E で「開いていない」を
 * 見ても、「絵解き」が開くのは保存のあとの取り直しを待ってからなので、取り違えた
 * 配線でも見るのが先なら通る。呼ぶ関数を取り違えたら、ここが落ちる。
 */
describe("BoardStatus の「保存」", () => {
  it("未保存なら出て、押すと保存だけを呼ぶ（絵解きは呼ばない）", () => {
    const p = props();
    render(<BoardStatus {...p} />);

    const save = saveButton();
    expect(save).not.toBeNull();
    fireEvent.click(save!);

    expect(p.onSave).toHaveBeenCalledTimes(1);
    expect(p.onEkidoki).not.toHaveBeenCalled();
  });

  // 保存済みで出すと、押しても書くものの無いボタンが「絵解き」の隣に残る。
  it("保存済みなら出さない", () => {
    render(<BoardStatus {...props({ dirty: false })} />);

    expect(saveButton()).toBeNull();
    expect(screen.getByRole("button", { name: "絵解き" })).toBeDefined();
  });

  // 保存できないロールには、押せないボタンを並べない（「絵解き」と同じ）。
  it("保存できないロールには、未保存でも出さない", () => {
    render(<BoardStatus {...props({ canEdit: false })} />);

    expect(saveButton()).toBeNull();
    expect(screen.queryByRole("button", { name: "絵解き" })).toBeNull();
    expect(screen.getByText("未保存")).toBeDefined();
  });

  // **見える文にショートカットを出さない。** キーボードの無い端末で、押せない口を
  // 案内することになる（#296 の再現 3）。読み上げにだけ伝える。
  it("ショートカットは読み上げにだけ伝え、見える文には出さない", () => {
    const { container } = render(<BoardStatus {...props()} />);

    expect(saveButton()?.getAttribute("aria-keyshortcuts")).toBe("Control+S Meta+S");
    expect(container.textContent).not.toContain("Ctrl+S");
  });

  // どちらも保存を始めるので同じ式で止まり、1 つの理由を両方から指す。
  it("押せないときは「保存」と「絵解き」が同じ理由を指す", () => {
    const reason = "作成が終わるまで保存も絵解きもできません";
    render(<BoardStatus {...props({ canSave: false, saveBlocked: reason })} />);

    const save = saveButton() as HTMLButtonElement;
    const ekidoki = screen.getByRole("button", { name: "絵解き" }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    expect(ekidoki.disabled).toBe(true);
    for (const button of [save, ekidoki]) {
      const id = button.getAttribute("aria-describedby");
      expect(id).not.toBeNull();
      expect(document.getElementById(id!)?.textContent).toBe(reason);
    }
  });

  // 対角（保存中に保存）は理由を出さない。ボタン自身が名乗る（`exclusion.ts`）。
  it("保存中は「保存中…」と名乗る", () => {
    render(<BoardStatus {...props({ canSave: false, saving: true })} />);

    const save = screen.getByRole("button", { name: "保存中…" }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    expect(save.getAttribute("aria-describedby")).toBeNull();
  });
});
