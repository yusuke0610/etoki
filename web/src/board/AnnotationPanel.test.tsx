import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

import { AnnotationPanel } from "./AnnotationPanel";

function props(): ComponentProps<typeof AnnotationPanel> {
  return {
    annotations: [
      { id: "frame-1", name: "ログイン", granularity: "", state: "uncreated" },
    ],
    detached: [],
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
    creationBlocked: null,
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

  // 前の選択（sequence）の保存だけが後から追いつくと、「保存済みの値が変わった
  // から追いついた」という判定では、追いついたのが古い選択のほうでも pending を
  // 消してしまい、選択欄がキャンバスと食い違う値（sequence）に戻ってしまう。
  it("保存中に選び直しても、古い選択の保存が追いついた時点で表示を戻さない", () => {
    const panelProps = props();
    const { rerender } = render(<AnnotationPanel {...panelProps} />);

    const select = screen.getByLabelText("種別");
    fireEvent.change(select, { target: { value: "sequence" } });
    fireEvent.change(select, { target: { value: "er" } });

    expect(panelProps.onChangeKind).toHaveBeenLastCalledWith("frame-1", "er");
    expect(select).toHaveValue("er");

    // 1 回目の選択（sequence）の保存だけが先に追いつく。2 回目（er）はまだ未保存。
    rerender(
      <AnnotationPanel
        {...panelProps}
        annotations={[
          {
            id: "frame-1",
            name: "ログイン",
            granularity: "",
            state: "uncreated",
            kind: "sequence",
          },
        ]}
      />,
    );
    expect(select).toHaveValue("er");

    // 2 回目の選択（er）の保存が追いつく。ここで初めて表示の根拠が a.kind に戻る。
    rerender(
      <AnnotationPanel
        {...panelProps}
        annotations={[
          {
            id: "frame-1",
            name: "ログイン",
            granularity: "",
            state: "uncreated",
            kind: "er",
          },
        ]}
      />,
    );
    expect(select).toHaveValue("er");
  });
});
