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
      names: { "frame-1": "ログイン" },
      onFocus: vi.fn(),
      onChangeGranularity: vi.fn(),
      onChangeKind: vi.fn(),
      onChangeName: vi.fn(),
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
  // 名前は見出しの隣のラベルを押して、その場で変える（#305）。欄は常には出さない。
  const startRename = (name = "ログイン") => {
    fireEvent.click(screen.getByRole("button", { name }));
    return screen.getByLabelText("名前");
  };

  // 名前も、粒度と種別と同じくキャンバスの値を出す（#249）。保存済みの値は次の
  // 保存まで古い。
  it("名前は、保存済みの値ではなくキャンバスの値を出す", () => {
    const detailProps = props();
    render(
      <AnnotationDetail
        {...detailProps}
        frames={{ ...detailProps.frames, names: { "frame-1": "ログインの入口" } }}
      />,
    );

    expect(screen.queryByLabelText("名前")).not.toBeInTheDocument();
    expect(startRename("ログインの入口")).toHaveValue("ログインの入口");
  });

  // 名前を消して確定したら、保存を待たずに番号の見出しになる。保存済みの名前から
  // 作った見出しに落とすと、消したはずの旧名が見出しとダイアログの名前に残る。
  it("名前を消した直後は、保存済みの旧名ではなく番号の見出しを出す", () => {
    const detailProps = props();
    render(
      <AnnotationDetail
        {...detailProps}
        frames={{ ...detailProps.frames, names: { "frame-1": "" } }}
      />,
    );

    expect(screen.getByRole("button", { name: "絵1" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "ログイン" })).not.toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "絵解き 絵1" })).toBeInTheDocument();
  });

  // 名前は常設の欄ではなく、見出しの隣のラベルを押して変える。
  it("名前の欄は常には出さず、ラベルを押すと出て焦点が入る", () => {
    render(<AnnotationDetail {...props()} />);

    expect(screen.queryByLabelText("名前")).not.toBeInTheDocument();
    expect(startRename()).toHaveFocus();
  });

  // 注釈を外した frame（保存前）は、キャンバスには在るが注釈ではない。名前を書く
  // 口は注釈にしか書かないので、押せると入力した名前が黙って消える。押せなくして
  // 理由を出す（ADR 0039）。
  it("注釈から外した frame では、名前を変えさせず理由を出す", () => {
    const detailProps = props();
    render(
      <AnnotationDetail {...detailProps} frames={{ ...detailProps.frames, names: {} }} />,
    );

    expect(screen.queryByRole("button", { name: "ログイン" })).not.toBeInTheDocument();
    expect(screen.getByText(/絵から外してあります/)).toBeInTheDocument();
  });

  // キャンバスからまだ聞いていない（null）うちは、外したことにしない（`canvasIds`
  // と同じ扱い）。開いた直後の一瞬だけ押せなくなる。
  it("キャンバスの名前がまだ分からないうちは、名前を変える口を止めない", () => {
    const detailProps = props();
    render(
      <AnnotationDetail
        {...detailProps}
        frames={{ ...detailProps.frames, names: null }}
      />,
    );

    expect(screen.getByRole("button", { name: "ログイン" })).toBeEnabled();
  });

  // 読むだけのロールは名前を変えられない。理由は本文に出ている（ADR 0030 / 0039）。
  it("読むだけのロールでは、名前を押せるものにしない", () => {
    render(<AnnotationDetail {...props()} canEdit={false} />);

    expect(screen.queryByRole("button", { name: "ログイン" })).not.toBeInTheDocument();
    expect(screen.getByText("ログイン", { selector: "span" })).toBeInTheDocument();
  });

  // 1 文字ごとに書くと、打つたびに未保存になり、「元に戻す」に 1 文字ずつ積まれる。
  // 確定するのは Enter か、欄から離れたとき。
  it("名前は入力の途中では書かず、Enter か欄を離れたときに書く", () => {
    const detailProps = props();
    render(<AnnotationDetail {...detailProps} />);

    const input = startRename();
    fireEvent.change(input, { target: { value: "ログインの入口" } });
    expect(detailProps.frames.onChangeName).not.toHaveBeenCalled();

    fireEvent.keyDown(input, { key: "Enter" });
    expect(detailProps.frames.onChangeName).toHaveBeenCalledWith(
      "frame-1",
      "ログインの入口",
    );
    expect(screen.queryByLabelText("名前")).not.toBeInTheDocument();

    const again = startRename();
    fireEvent.change(again, { target: { value: "入口" } });
    fireEvent.blur(again);
    expect(detailProps.frames.onChangeName).toHaveBeenLastCalledWith("frame-1", "入口");
    expect(screen.queryByLabelText("名前")).not.toBeInTheDocument();
  });

  // 「絵解く」は押しても焦点を奪わない（`DetailBand`）。欄を離れたときの確定が
  // 走らないので、押した処理の中で書きかけを確定してから読む。順番が逆だと、
  // 保存にも解釈にも新しい名前が入らない。
  it("名前の書きかけは、「絵解く」を押したときに読む前に確定する", () => {
    const detailProps = props();
    const calls: string[] = [];
    detailProps.frames.onChangeName = vi.fn(() => calls.push("name"));
    detailProps.interpretation.onInterpret = vi.fn(() => calls.push("interpret"));
    render(<AnnotationDetail {...detailProps} />);

    fireEvent.change(startRename(), {
      target: { value: "ログインの入口" },
    });
    fireEvent.click(screen.getByRole("button", { name: "絵解く" }));

    expect(calls).toEqual(["name", "interpret"]);
    expect(detailProps.frames.onChangeName).toHaveBeenCalledWith(
      "frame-1",
      "ログインの入口",
    );
  });

  // 日本語の変換を確定する Enter で書くと、変換の途中の名前が書かれる。
  it("変換中の Enter では書かない", () => {
    const detailProps = props();
    render(<AnnotationDetail {...detailProps} />);

    const input = startRename();
    fireEvent.change(input, { target: { value: "ろぐいん" } });
    fireEvent.keyDown(input, { key: "Enter", isComposing: true });
    expect(detailProps.frames.onChangeName).not.toHaveBeenCalled();
    expect(screen.getByLabelText("名前")).toBeInTheDocument();
  });

  // 変えていないのに書くと、開いて閉じただけで未保存になる。
  it("名前を変えずに欄を離れても書かない", () => {
    const detailProps = props();
    render(<AnnotationDetail {...detailProps} />);

    fireEvent.blur(startRename());
    expect(detailProps.frames.onChangeName).not.toHaveBeenCalled();
    expect(screen.queryByLabelText("名前")).not.toBeInTheDocument();
  });

  // Escape は、書きかけを捨てるだけにする。詳細まで閉じると、書きかけを捨てた
  // つもりで面ごと閉じる。欄が閉じたあとの Escape は面を閉じる。焦点はラベルへ戻す。
  it("名前の書きかけは Escape で捨て、詳細は閉じない", () => {
    const detailProps = props();
    render(<AnnotationDetail {...detailProps} />);

    const input = startRename();
    fireEvent.change(input, { target: { value: "書きかけ" } });
    fireEvent.keyDown(input, { key: "Escape" });

    expect(screen.queryByLabelText("名前")).not.toBeInTheDocument();
    const label = screen.getByRole("button", { name: "ログイン" });
    expect(label).toHaveFocus();
    expect(detailProps.onClose).not.toHaveBeenCalled();
    fireEvent.keyDown(label, { key: "Escape" });
    expect(detailProps.onClose).toHaveBeenCalled();
    expect(detailProps.frames.onChangeName).not.toHaveBeenCalled();
  });

  // 書いている途中で押せる条件を失ったら、書きかけを捨てて欄を閉じる。入力欄を
  // 押せる条件は、確定したときに書く口が書ける条件と揃える（async-ui.md）。残すと
  // 書けない対象に確定を押させ、条件が戻ったときに古い書きかけが欄ごと復活する。
  it("書いている途中で名前を変えられなくなったら、書きかけを捨てて欄を閉じる", () => {
    const detailProps = props();
    const { rerender } = render(<AnnotationDetail {...detailProps} />);

    fireEvent.change(startRename(), { target: { value: "書きかけ" } });
    rerender(<AnnotationDetail {...detailProps} canEdit={false} />);

    expect(screen.queryByLabelText("名前")).not.toBeInTheDocument();
    expect(detailProps.frames.onChangeName).not.toHaveBeenCalled();

    rerender(<AnnotationDetail {...detailProps} canEdit={true} />);
    expect(screen.queryByLabelText("名前")).not.toBeInTheDocument();
    expect(detailProps.frames.onChangeName).not.toHaveBeenCalled();
  });

  // キャンバスの名前がまだ分からない（null）うちに欄を開くと、下書きは保存済みの
  // 名前から始まる。あとでキャンバスの名前が届いたとき、触っていない下書きを確定して
  // 古い名前をキャンバスへ書き戻さない。「変えたか」は開いた時点の値と比べる。
  it("開いたあとにキャンバスの名前が届いても、触っていない下書きでは書き戻さない", () => {
    const detailProps = props();
    const { rerender } = render(
      <AnnotationDetail
        {...detailProps}
        frames={{ ...detailProps.frames, names: null }}
      />,
    );

    const input = startRename();
    rerender(
      <AnnotationDetail
        {...detailProps}
        frames={{ ...detailProps.frames, names: { "frame-1": "入口" } }}
      />,
    );
    fireEvent.keyDown(input, { key: "Enter" });

    expect(detailProps.frames.onChangeName).not.toHaveBeenCalled();
  });

  // Enter で確定したあとは、焦点をラベルへ戻す。欄を離れて確定したときは、離れた
  // 先の焦点を奪わない。
  it("焦点は Enter のあとだけラベルへ戻し、欄を離れたときは奪わない", () => {
    render(<AnnotationDetail {...props()} />);

    fireEvent.change(startRename(), { target: { value: "入口" } });
    fireEvent.keyDown(screen.getByLabelText("名前"), { key: "Enter" });
    expect(screen.getByRole("button", { name: "ログイン" })).toHaveFocus();

    const input = startRename();
    fireEvent.change(input, { target: { value: "出口" } });
    screen.getByRole("button", { name: "絵解く" }).focus();
    fireEvent.blur(input);
    expect(screen.getByRole("button", { name: "絵解く" })).toHaveFocus();
  });

  // 名前は解釈の入力で `content_hash` にも入る（ADR 0036）。作成済みの注釈の名前を
  // 変えると「変更あり」になることを、変える前に言う。未作成なら影響が無いので
  // 出さない。
  it("作成済みの注釈にだけ、名前を変えると変更ありになることを添える", () => {
    const detailProps = props();
    const { unmount } = render(<AnnotationDetail {...detailProps} />);
    expect(
      screen.getByRole("button", { name: "ログイン" }),
    ).not.toHaveAccessibleDescription();
    unmount();

    render(
      <AnnotationDetail
        {...detailProps}
        annotations={[
          { id: "frame-1", name: "ログイン", granularity: "", state: "created" },
        ]}
      />,
    );
    expect(screen.getByRole("button", { name: "ログイン" })).toHaveAccessibleDescription(
      /変えると「変更あり」になります/,
    );
    expect(startRename()).toHaveAccessibleDescription(/変えると「変更あり」になります/);
  });

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

  // 見出しは「絵解き」で、どの注釈かは隣の名前（#247）。面は注釈の数だけ並ぶので、
  // ダイアログの名前は 2 つをつないで読ませる。見出しだけにすると面の見分けが付かない。
  it("見出しは「絵解き」で、ダイアログの名前に注釈の名前を含める", () => {
    render(<AnnotationDetail {...props()} />);

    expect(screen.getByRole("heading", { name: "絵解き" })).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toHaveAccessibleName(/^絵解き .+/);
  });

  // 解釈の前でも詳細は開ける。帯には「絵解く」だけを出し、作成のボタンは
  // 解釈を 1 件選ぶまで出さない。
  it("解釈の前は帯に「絵解く」だけがある", () => {
    const detailProps = props();
    render(<AnnotationDetail {...detailProps} />);

    fireEvent.click(screen.getByRole("button", { name: "絵解く" }));
    expect(detailProps.interpretation.onInterpret).toHaveBeenCalledWith("frame-1");
    expect(screen.queryByRole("button", { name: "GitHub に作成する" })).toBeNull();
  });

  // 引いた解釈はメモリにしか無い（#39）ので、開き直すと作成済みでも変更ありでも
  // 「引く前」の本文になる。状態で分けないと、作成済みなのに「まだ」と言い、変更
  // ありの次の一手が無い（#290）。
  describe("解釈の前の本文は、同期の状態で分ける", () => {
    function withState(state: "uncreated" | "created" | "changed") {
      return {
        ...props(),
        annotations: [
          { id: "frame-1", name: "ログイン", granularity: "" as const, state },
        ],
      };
    }

    it("未作成なら、まだ絵解きしていないと言う", () => {
      render(<AnnotationDetail {...withState("uncreated")} />);

      const hint = screen.getByText(/まだ絵解きしていません/);
      expect(hint).toHaveTextContent(/作るものの下書きを出します/);
    });

    it("作成済みなら「まだ」と言わず、GitHub にあることと更新の候補を言う", () => {
      render(<AnnotationDetail {...withState("created")} />);

      expect(screen.queryByText(/まだ絵解きしていません/)).toBeNull();
      const hint = screen.getByText(/GitHub にあります/);
      // 開き直すと結果が消えることも言う。言わないと、出ていないのを作っていない印と読む。
      expect(hint).toHaveTextContent(/画面を開いているあいだしか持たない/);
      expect(hint).toHaveTextContent(
        /開き直したり保存したりすると、ここには出なくなります/,
      );
      expect(hint).toHaveTextContent(/前回作ったものの更新を候補にした下書き/);
      expect(hint).toHaveTextContent(/押しただけでは何も作りません/);
      expect(hint).toHaveTextContent(/項目ごとに選べます/);
      // 変更ありの文を出していないこと。共通の後半だけでは取り違えても通る。
      expect(hint).not.toHaveTextContent(/一致しない/);
    });

    // 中身が同じでも changed になりうる（ADR 0047）ので、変わったとは言い切らない。
    it("変更ありなら、押しても作らないことを言い、変わったとは断定しない", () => {
      render(<AnnotationDetail {...withState("changed")} />);

      expect(screen.queryByText(/まだ絵解きしていません/)).toBeNull();
      const hint = screen.getByText(/前回作ったものの更新を候補にした下書き/);
      expect(hint).toHaveTextContent(/押しただけでは何も作りません/);
      expect(hint).toHaveTextContent(/項目ごとに選べます/);
      expect(hint).toHaveTextContent(/一致しないという印/);
      expect(hint).toHaveTextContent(/中身を書き換えていなくても付く/);
      expect(hint).not.toHaveTextContent(/変わりました/);
      expect(hint).not.toHaveTextContent(/GitHub にあります/);
    });
  });

  // 未保存でも押させる。押した操作の中で保存してから読む（#247、`BoardPage`）。
  // 止めると、粒度や種別を選び直した直後に押せなくなる。保存がほかの注釈の結果も
  // 捨てることは本文で言う。
  it("未保存でも「絵解く」を押させ、保存してから読むことを案内する", () => {
    const detailProps = props();
    render(<AnnotationDetail {...detailProps} stale />);

    const interpret = screen.getByRole("button", { name: "絵解く" });
    expect(interpret).toBeEnabled();
    expect(screen.getByText(/押すと保存してから読みます/)).toBeInTheDocument();
    fireEvent.click(interpret);
    expect(detailProps.interpretation.onInterpret).toHaveBeenCalledWith("frame-1");
  });

  // 設定の不足は保存しても変わらないので、押せない理由はそれだけ（ADR 0030）。
  it("LLM が未設定なら、未保存でもその理由で止める", () => {
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

    const interpret = screen.getByRole("button", { name: "絵解く" });
    expect(interpret).toBeDisabled();
    expect(interpret).toHaveAccessibleDescription("LLM が未設定です。");
  });

  // 解釈は 1 人につき同時に 1 件まで（ADR 0044）。別の注釈が走っているあいだに
  // 押すと 429 で断られ、押したのに何も起きないように見える。押す前に理由を言う。
  describe("別の注釈の解釈が走っているあいだ", () => {
    function withRunning(otherRunning: boolean) {
      const detailProps = props();
      return {
        ...detailProps,
        annotations: [
          {
            id: "frame-1",
            name: "ログイン",
            granularity: "" as const,
            state: "uncreated" as const,
          },
          {
            id: "frame-2",
            name: "決済",
            granularity: "" as const,
            state: "uncreated" as const,
          },
        ],
        interpretation: {
          ...detailProps.interpretation,
          states: {
            "frame-2": { running: otherRunning, runs: [], selectedId: null },
          },
        },
      };
    }

    it("「解釈する」を押させず、帯に理由を出す", () => {
      render(<AnnotationDetail {...withRunning(true)} />);

      const interpret = screen.getByRole("button", { name: "絵解く" });
      expect(interpret).toBeDisabled();
      expect(interpret).toHaveAccessibleDescription(/ほかの囲みを絵解き中/);
    });

    // 走っていない注釈の state が残っているだけ（結果を引いたあと）では止めない。
    // 止めると、一度解釈した囲みがあるだけで、ほかの囲みが二度と押せなくなる。
    it("走り終わった注釈の結果が残っているだけなら押せる", () => {
      render(<AnnotationDetail {...withRunning(false)} />);

      expect(screen.getByRole("button", { name: "絵解く" })).toBeEnabled();
    });

    // 自分自身が走っているときは、今までどおり「解釈中…」で押せない。理由の文は
    // 出さない（待っているのは自分の解釈で、ほかの囲みではない）。
    it("自分が走っているときは、ほかの囲みの理由を出さない", () => {
      const detailProps = withRunning(false);
      render(
        <AnnotationDetail
          {...detailProps}
          interpretation={{
            ...detailProps.interpretation,
            states: { "frame-1": { running: true, runs: [], selectedId: null } },
          }}
        />,
      );

      expect(screen.getByRole("button", { name: "絵解き中…" })).toBeDisabled();
      expect(screen.queryByText(/ほかの囲みを絵解き中/)).toBeNull();
    });
  });

  it("読むだけの権限では帯を出さず、その理由を出す", () => {
    render(<AnnotationDetail {...props()} canEdit={false} />);

    expect(screen.queryByRole("button", { name: "絵解く" })).toBeNull();
    expect(
      screen.getByText(
        "読むだけの権限で開いています。名前・粒度・種別は変えられず、絵解きと作成もできません。",
      ),
    ).toBeInTheDocument();
    // 押せない粒度と種別も同じ文を指す。
    for (const name of ["粒度", "種別"]) {
      const select = screen.getByRole("combobox", { name });
      expect(select).toBeDisabled();
      expect(select).toHaveAccessibleDescription(
        "読むだけの権限で開いています。名前・粒度・種別は変えられず、絵解きと作成もできません。",
      );
    }
  });
});
