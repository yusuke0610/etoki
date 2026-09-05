import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

import { AnnotationPanel } from "./AnnotationPanel";

function props(): ComponentProps<typeof AnnotationPanel> {
  return {
    annotations: [
      { id: "frame-1", name: "ログイン", granularity: "", state: "uncreated" },
    ],
    markableFrames: [],
    unmarkableFrames: [],
    canvasFrameIds: ["frame-1"],
    selectedFrameIds: [],
    onFocusFrame: vi.fn(),
    onMark: vi.fn(),
    onUnmark: vi.fn(),
    onChangeGranularity: vi.fn(),
    onChangeKind: vi.fn(),
    stale: false,
    interpretations: {},
    onInterpret: vi.fn(),
    onSelectInterpretation: vi.fn(),
    runHistories: {},
    onLoadRuns: vi.fn(),
    creations: {},
    saving: false,
    onCreate: vi.fn(),
    canEdit: true,
    projectAccess: "allowed",
    interpretationUnavailable: null,
    creationUnavailable: null,
    projectLink: null,
  };
}

describe("AnnotationPanel", () => {
  // 種別の更新は live scene にだけ先に反映され、annotations は保存するまで古い。
  // ここで選択値を直接見ることで、古い a.kind を表示し続ける回帰を検知する。
  it("保存前でも選んだ種別を表示する", () => {
    const panelProps = props();
    render(<AnnotationPanel {...panelProps} />);

    const select = screen.getByLabelText("種別");
    fireEvent.change(select, { target: { value: "sequence" } });

    expect(panelProps.onChangeKind).toHaveBeenCalledWith("frame-1", "sequence");
    expect(select).toHaveValue("sequence");
  });
});
