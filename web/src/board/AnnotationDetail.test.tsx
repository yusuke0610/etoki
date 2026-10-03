import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

import { AnnotationDetail } from "./AnnotationDetail";

function props(): ComponentProps<typeof AnnotationDetail> {
  return {
    openId: "frame-1",
    onClose: vi.fn(),
    annotations: [
      { id: "frame-1", name: "ログイン", granularity: "", state: "uncreated" },
    ],
    frames: {
      canvasIds: ["frame-1"],
      onFocus: vi.fn(),
      onChangeGranularity: vi.fn(),
      onChangeKind: vi.fn(),
    },
    interpretation: {
      states: {},
      onInterpret: vi.fn(),
      onSelect: vi.fn(),
      unavailable: null,
    },
    creation: {
      states: {},
      saving: false,
      blocked: null,
      onCreate: vi.fn(),
      projectAccess: "unknown",
      unavailable: null,
    },
    runs: { states: {}, onLoad: vi.fn() },
    stale: false,
    canEdit: true,
    projectLink: null,
    targetLabel: "acme/web › #1 ロードマップ",
  };
}

describe("AnnotationDetail", () => {
  // 種別の更新は live scene にだけ先に反映され、annotations は保存するまで古い。
  // ここで選択値を直接見ることで、古い a.kind を表示し続ける回帰を検知する。
  it("保存前でも選んだ種別を表示する", () => {
    const detailProps = props();
    render(<AnnotationDetail {...detailProps} />);

    const select = screen.getByLabelText("種別");
    fireEvent.change(select, { target: { value: "sequence" } });

    expect(detailProps.frames.onChangeKind).toHaveBeenCalledWith("frame-1", "sequence");
    expect(select).toHaveValue("sequence");
  });

  // 前の選択（sequence）の保存だけが後から追いつくと、「保存済みの値が変わった
  // から追いついた」という判定では、追いついたのが古い選択のほうでも pending を
  // 消してしまい、選択欄がキャンバスと食い違う値（sequence）に戻ってしまう。
  it("保存中に選び直しても、古い選択の保存が追いついた時点で表示を戻さない", () => {
    const detailProps = props();
    const { rerender } = render(<AnnotationDetail {...detailProps} />);

    const select = screen.getByLabelText("種別");
    fireEvent.change(select, { target: { value: "sequence" } });
    fireEvent.change(select, { target: { value: "er" } });

    expect(detailProps.frames.onChangeKind).toHaveBeenLastCalledWith("frame-1", "er");
    expect(select).toHaveValue("er");

    // 1 回目の選択（sequence）の保存だけが先に追いつく。2 回目（er）はまだ未保存。
    rerender(
      <AnnotationDetail
        {...detailProps}
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
      <AnnotationDetail
        {...detailProps}
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

  // 追いついたあとも pending を持ち続けると、元に戻す・取り込みで保存済みの値が
  // 変わったときに、選択欄が古い選択（er）を出し続ける。
  it("選んだ値が保存で追いついたあと、保存済みの値が変わればそちらを表示する", () => {
    const detailProps = props();
    const { rerender } = render(<AnnotationDetail {...detailProps} />);
    const select = screen.getByLabelText("種別");
    const withKind = (kind: "er" | undefined) => (
      <AnnotationDetail
        {...detailProps}
        annotations={[
          { id: "frame-1", name: "ログイン", granularity: "", state: "uncreated", kind },
        ]}
      />
    );

    fireEvent.change(select, { target: { value: "er" } });
    rerender(withKind("er"));
    expect(select).toHaveValue("er");

    rerender(withKind(undefined));
    expect(select).toHaveValue("");
  });

  // 詳細はキャンバスの中央を覆うので、開いたまま寄せても選んだ frame は裏に
  // 隠れる。閉じてから寄せる（ADR 0022）。
  it("「キャンバスで見る」は詳細を閉じてから frame へ寄せる", () => {
    const detailProps = props();
    render(<AnnotationDetail {...detailProps} />);

    fireEvent.click(screen.getByRole("button", { name: "キャンバスで見る" }));

    expect(detailProps.onClose).toHaveBeenCalled();
    expect(detailProps.frames.onFocus).toHaveBeenCalledWith("frame-1");
  });

  it("frame がキャンバスに無ければ「キャンバスで見る」を押させず、理由を結ぶ", () => {
    render(
      <AnnotationDetail {...props()} frames={{ ...props().frames, canvasIds: [] }} />,
    );

    const show = screen.getByRole("button", { name: "キャンバスで見る" });
    expect(show).toBeDisabled();
    expect(show).toHaveAccessibleDescription(
      "このフレームはキャンバスにありません。保存すると一覧からも消えます。",
    );
  });

  // 解釈の前でも詳細は開ける。帯には「解釈する」だけを出し、作成のボタンは
  // 解釈を 1 件選ぶまで出さない。
  it("解釈の前は帯に「解釈する」だけがある", () => {
    const detailProps = props();
    render(<AnnotationDetail {...detailProps} />);

    fireEvent.click(screen.getByRole("button", { name: "解釈する" }));
    expect(detailProps.interpretation.onInterpret).toHaveBeenCalledWith("frame-1");
    expect(screen.queryByRole("button", { name: "GitHub に作成する" })).toBeNull();
  });

  // 未保存のあいだは解釈させない（ADR 0018）。理由は帯の本文で結ぶ。
  it("未保存のあいだは「解釈する」を押させず、帯に理由を出す", () => {
    render(<AnnotationDetail {...props()} stale />);

    const interpret = screen.getByRole("button", { name: "解釈する" });
    expect(interpret).toBeDisabled();
    expect(interpret).toHaveAccessibleDescription(/保存してから解釈できます/);
  });

  // 設定の不足は保存しても変わらないので、未保存より先に出す（ADR 0030）。
  it("LLM が未設定なら、未保存より先にその理由を出す", () => {
    const detailProps = props();
    render(
      <AnnotationDetail
        {...detailProps}
        stale
        interpretation={{
          ...detailProps.interpretation,
          unavailable: "LLM が未設定です。",
        }}
      />,
    );

    expect(screen.getByRole("button", { name: "解釈する" })).toHaveAccessibleDescription(
      "LLM が未設定です。",
    );
  });

  it("読むだけの権限では帯を出さず、その理由を出す", () => {
    render(<AnnotationDetail {...props()} canEdit={false} />);

    expect(screen.queryByRole("button", { name: "解釈する" })).toBeNull();
    expect(
      screen.getByText(
        "読むだけの権限で開いています。粒度と種別は変えられず、解釈と作成もできません。",
      ),
    ).toBeInTheDocument();
    // 押せない粒度と種別も同じ文を指す。
    for (const name of ["粒度", "種別"]) {
      const select = screen.getByRole("combobox", { name });
      expect(select).toBeDisabled();
      expect(select).toHaveAccessibleDescription(
        "読むだけの権限で開いています。粒度と種別は変えられず、解釈と作成もできません。",
      );
    }
  });
});
