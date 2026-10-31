import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { GRANULARITY_LABEL, STATE_LABEL } from "./annotations/annotationLabel";
import { DIAGRAM_KIND_LABELS } from "./diagram/diagramLabels";
import { UsageGuideBody } from "./UsageGuideDialog";

/**
 * 使い方の本文（ADR 0094）。見た目は E2E のスクリーンショットが見る。ここで
 * 押さえるのは、本文が画面の表と食い違わないことと、心得が手順より先に来ること。
 */
describe("UsageGuideBody", () => {
  // 切れると: 状態・粒度・種別の表を直したのに、使い方だけが古い語で説明する。
  // 本文に書き写すと、どれか 1 つ消しても通るので、表の値をすべて引いて見る。
  // **それぞれの説明の中で見る。** epic / issue は心得の文にも出るので、本文
  // 全体で探すと粒度の並びを消しても通る。
  it("状態・粒度・種別の見出しを、表の値のまま全部出す", () => {
    render(<UsageGuideBody />);
    const termOf = (name: string) =>
      screen.getByText(name, { selector: "dt" }).nextElementSibling?.textContent ?? "";

    for (const label of Object.values(STATE_LABEL)) {
      expect(screen.getByText(label, { selector: "dt" })).toBeTruthy();
    }
    for (const label of Object.values(GRANULARITY_LABEL)) {
      expect(termOf("粒度")).toContain(label);
    }
    for (const label of Object.values(DIAGRAM_KIND_LABELS)) {
      expect(termOf("種別")).toContain(label);
    }
  });

  // 切れると: 手順だけが目に入り、ブレストの前に枠を決める使い方に寄る
  // （中核思想 1）。心得は最初の節に置く。
  it("最初の節で、描いているあいだは構造を考えなくてよいと言う", () => {
    render(<UsageGuideBody />);

    const headings = screen.getAllByRole("heading", { level: 3 });
    expect(headings[0]?.textContent).toBe("はじめに");
    const first = headings[0]?.closest("section");
    expect(first?.textContent).toContain(
      "描いているあいだは、issue や epic のことは考えなくてかまいません。",
    );
    expect(first?.textContent).toContain("フレームで囲んで「絵」にした範囲だけ");
    expect(first?.textContent).toContain("自動では行いません");
  });

  // 切れると: フレームの引き方と、絵にする操作の場所が分からないまま残る。
  // どちらも画面の文言（ツールのキーとボタンの語）で言う。
  it("フレームの引き方と、絵にする操作の場所を手順に書く", () => {
    render(<UsageGuideBody />);

    const steps = screen.getAllByRole("listitem").map((li) => li.textContent ?? "");
    expect(steps.some((s) => s.includes("フレームツール（F）"))).toBe(true);
    expect(
      steps.some((s) => s.includes("「選択中のフレーム」") && s.includes("を絵にする")),
    ).toBe(true);
    expect(steps.some((s) => s.includes("「絵解き」") && s.includes("「絵解く」"))).toBe(
      true,
    );
    expect(steps.some((s) => s.includes("GitHub に作成する"))).toBe(true);
  });
});
