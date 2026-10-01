import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

import { AnnotationPanel } from "./AnnotationPanel";

function props(): ComponentProps<typeof AnnotationPanel> {
  return {
    annotations: [
      { id: "frame-1", name: "ログイン", granularity: "epic", state: "changed" },
    ],
    detached: [],
    frames: {
      markable: [],
      unmarkable: [],
      canvasIds: ["frame-1"],
      selectedIds: [],
      onMark: vi.fn(),
      onUnmark: vi.fn(),
    },
    runs: { states: {}, onLoad: vi.fn() },
    stale: false,
    canEdit: true,
    projectLink: null,
    onOpenDetail: vi.fn(),
  };
}

describe("AnnotationPanel", () => {
  // カードはボタン 1 つ。名前は注釈の見出しだけにし、状態と要約は説明として
  // 結ぶ。全部を名前にすると、名前で引く読み上げと E2E の両方で長すぎる。
  it("カードを押すと詳細を開き、状態と要約は説明として読める", () => {
    const panelProps = props();
    render(<AnnotationPanel {...panelProps} />);

    const card = screen.getByRole("button", { name: "ログイン" });
    expect(card).toHaveAccessibleDescription("変更あり 粒度 epic · 種別 未指定");

    fireEvent.click(card);
    expect(panelProps.onOpenDetail).toHaveBeenCalledWith("frame-1");
  });

  // キャンバスに無い注釈も詳細で GitHub にあるものや履歴を読めるようにする。
  // 押せないのは詳細の「キャンバスで見る」のほう（ADR 0022）。
  it("キャンバスに無い注釈のカードも押せ、そのことを畳まずに出す", () => {
    const panelProps = props();
    render(
      <AnnotationPanel
        {...panelProps}
        frames={{ ...panelProps.frames, canvasIds: [] }}
      />,
    );

    expect(screen.getByRole("button", { name: "ログイン" })).toBeEnabled();
    expect(
      screen.getByText(
        "このフレームはキャンバスにありません。保存すると一覧からも消えます。",
      ),
    ).toBeInTheDocument();
  });
});
