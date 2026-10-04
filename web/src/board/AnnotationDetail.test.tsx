import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

import { AnnotationDetail } from "./AnnotationDetail";

function props(): ComponentProps<typeof AnnotationDetail> {
  return {
    openId: "frame-1",
    openRequest: 1,
    onClose: vi.fn(),
    annotations: [
      { id: "frame-1", name: "ログイン", granularity: "", state: "uncreated" },
    ],
    frames: {
      canvasIds: ["frame-1"],
      metas: { "frame-1": { granularity: "", kind: undefined } },
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
  // 選択欄が書く先はキャンバスの要素で、保存済みの値（annotations）は次の保存
  // まで古い。保存済みの値に「選んだ値」を重ねて出す作りは、保存中の選び直しや
  // 「元に戻す」でキャンバスと食い違った（#124、#214）。
  it("粒度と種別は、保存済みの値ではなくキャンバスの値を出す", () => {
    const detailProps = props();
    render(
      <AnnotationDetail
        {...detailProps}
        annotations={[
          // 遅れて届いた前の保存。キャンバスはもう別の値になっている。
          {
            id: "frame-1",
            name: "ログイン",
            granularity: "epic",
            state: "uncreated",
            kind: "er",
          },
        ]}
        frames={{
          ...detailProps.frames,
          metas: { "frame-1": { granularity: "issue", kind: "sequence" } },
        }}
      />,
    );

    expect(screen.getByLabelText("粒度")).toHaveValue("issue");
    expect(screen.getByLabelText("種別")).toHaveValue("sequence");
  });

  // キャンバスの注釈に種別が無いことと、キャンバスに注釈が無いことを取り違えると、
  // 「指定なし」に選び直した直後に保存済みの種別が出る。
  it("キャンバスの注釈が種別を持たなければ、保存済みの種別ではなく「指定なし」を出す", () => {
    const detailProps = props();
    render(
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

    expect(screen.getByLabelText("種別")).toHaveValue("");
  });

  // 未保存で消した frame の注釈は一覧に残る。キャンバスに値が無いので保存済みを出す。
  // Excalidraw からまだ聞いていない（null）ときも同じ。
  it("キャンバスに無い注釈は、保存済みの値を出す", () => {
    const detailProps = props();
    const saved = [
      {
        id: "frame-1",
        name: "ログイン",
        granularity: "epic" as const,
        state: "uncreated" as const,
        kind: "er" as const,
      },
    ];
    const { rerender } = render(
      <AnnotationDetail
        {...detailProps}
        annotations={saved}
        frames={{ ...detailProps.frames, metas: {} }}
      />,
    );
    expect(screen.getByLabelText("粒度")).toHaveValue("epic");
    expect(screen.getByLabelText("種別")).toHaveValue("er");

    rerender(
      <AnnotationDetail
        {...detailProps}
        annotations={saved}
        frames={{ ...detailProps.frames, metas: null }}
      />,
    );
    expect(screen.getByLabelText("粒度")).toHaveValue("epic");
    expect(screen.getByLabelText("種別")).toHaveValue("er");
  });

  it("選んだ粒度と種別はキャンバスへ書く", () => {
    const detailProps = props();
    render(<AnnotationDetail {...detailProps} />);

    fireEvent.change(screen.getByLabelText("粒度"), { target: { value: "epic" } });
    fireEvent.change(screen.getByLabelText("種別"), { target: { value: "sequence" } });
    fireEvent.change(screen.getByLabelText("種別"), { target: { value: "" } });

    expect(detailProps.frames.onChangeGranularity).toHaveBeenCalledWith(
      "frame-1",
      "epic",
    );
    expect(detailProps.frames.onChangeKind).toHaveBeenNthCalledWith(
      1,
      "frame-1",
      "sequence",
    );
    // 「指定なし」は種別の語彙に無いので undefined で渡す（customData からキーごと落ちる）。
    expect(detailProps.frames.onChangeKind).toHaveBeenNthCalledWith(
      2,
      "frame-1",
      undefined,
    );
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
