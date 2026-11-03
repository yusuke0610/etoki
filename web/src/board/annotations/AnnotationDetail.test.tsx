import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

import type { InterpretationResult } from "../../api/types";
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
      onCancel: vi.fn(),
      unavailable: null,
      premise: "",
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

  // 名前は `content_hash` の入力ではない（#303、ADR 0079）。作成済みの注釈でも、
  // 名前を変えて「変更あり」になることは無いので、そうなると添えない。
  it("作成済みの注釈でも、名前を変えると変更ありになるとは添えない", () => {
    render(
      <AnnotationDetail
        {...props()}
        annotations={[
          { id: "frame-1", name: "ログイン", granularity: "", state: "created" },
        ]}
      />,
    );
    expect(
      screen.getByRole("button", { name: "ログイン" }),
    ).not.toHaveAccessibleDescription();
    expect(startRename()).not.toHaveAccessibleDescription();
    expect(screen.queryByText(/「変更あり」になります/)).toBeNull();
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

    // 矢印や図形だけを描き直しても「変更あり」にならない（#301）。状態が動かない
    // ことを言わないと、作成済みのままなのを「読み直す必要が無い」印と読む。
    it("作成済みなら、矢印や図形だけの変更では「変更あり」にならないことを言う", () => {
      render(<AnnotationDetail {...withState("created")} />);

      // 置き場所は囲みの中に限る。囲みに出し入れすると所属が変わって動く。
      const note = screen.getByText(
        /矢印や図形、貼った画像、囲みの中での置き場所、絵の名前、ボードの背景だけを直しても/,
      );
      expect(note).toHaveTextContent(/「作成済み」のまま/);
      // 何を変えれば「変更あり」になるかも言う。言わないと、どこまでが検知の外かが読めない。
      expect(note).toHaveTextContent(/絵の中の文字・粒度・種別/);
      expect(note).toHaveTextContent(/文字を囲みに出し入れしたとき/);
      // 文字は要素の ID ごと見るので、文面が同じでも作り直せば動く。
      expect(note).toHaveTextContent(/同じ文面でも消して作り直したとき/);
    });

    // 作った直後は解釈を持ったまま作成済みになる。解釈の前の本文は消えるが、
    // この案内まで一緒に消すと、描き直す前に知っておくことが画面から無くなる。
    it("作成済みなら、絵解きの結果が出ていても、矢印や図形だけの変更の案内を出す", () => {
      const detailProps = withState("created");
      const interpretation: InterpretationResult = {
        summary: "s",
        contentHash: "h",
        items: [],
        context: { premise: "", otherItems: [], omittedOtherItems: 0 },
      };
      render(
        <AnnotationDetail
          {...detailProps}
          interpretation={{
            ...detailProps.interpretation,
            states: {
              "frame-1": {
                running: false,
                runs: [
                  {
                    id: 1,
                    at: "2026-01-01T00:00:00Z",
                    granularity: "issue",
                    result: interpretation,
                  },
                ],
                selectedId: 1,
              },
            },
          }}
        />,
      );

      expect(screen.queryByText(/GitHub にあります/)).toBeNull();
      expect(
        screen.getByText(/矢印や図形、貼った画像、囲みの中での置き場所/),
      ).toBeInTheDocument();
    });

    it("作成済みでなければ、矢印や図形だけの変更の案内は出さない", () => {
      render(<AnnotationDetail {...withState("changed")} />);

      expect(screen.queryByText(/矢印や図形、貼った画像/)).toBeNull();
    });

    // 失敗を出しているあいだは、解釈の前の本文と同じく出さない。読むのは失敗のほう。
    it("作成済みでも、絵解きの失敗を出しているあいだは案内を出さない", () => {
      const detailProps = withState("created");
      render(
        <AnnotationDetail
          {...detailProps}
          interpretation={{
            ...detailProps.interpretation,
            states: {
              "frame-1": {
                running: false,
                runs: [],
                selectedId: null,
                failure: { message: "絵解きできませんでした", detail: "" },
              },
            },
          }}
        />,
      );

      expect(screen.getByText(/絵解きできませんでした/)).toBeInTheDocument();
      expect(screen.queryByText(/矢印や図形、貼った画像/)).toBeNull();
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

  // 走っている絵解きを、押した人が止められる（#330）。
  describe("絵解きの取り消し", () => {
    function withState(state: {
      running: boolean;
      jobId?: string;
      cancelling?: boolean;
    }) {
      const detailProps = props();
      return {
        ...detailProps,
        interpretation: {
          ...detailProps.interpretation,
          states: { "frame-1": { ...state, runs: [], selectedId: null } },
        },
      };
    }

    it("走っているジョブがあれば取り消しを出し、押すと取り消す", () => {
      const detailProps = withState({ running: true, jobId: "job-1" });
      render(<AnnotationDetail {...detailProps} />);

      fireEvent.click(screen.getByRole("button", { name: "絵解きを取り消す" }));
      expect(detailProps.interpretation.onCancel).toHaveBeenCalledWith("frame-1");
    });

    // ジョブが分かる前に取り消せると、押し直したときに同じジョブが返る仕組みと
    // 競合する（`InterpretationState.jobId`）。
    it("取り消しの返事を待つあいだは、取り消し中と出す", () => {
      render(
        <AnnotationDetail
          {...withState({ running: true, jobId: "job-1", cancelling: true })}
        />,
      );

      expect(screen.getByRole("button", { name: "取り消し中…" })).toBeEnabled();
    });

    it("ジョブが分かる前は出さない", () => {
      render(<AnnotationDetail {...withState({ running: true })} />);

      expect(screen.getByRole("button", { name: "絵解き中…" })).toBeDisabled();
      expect(screen.queryByRole("button", { name: "絵解きを取り消す" })).toBeNull();
    });

    it("走っていなければ出さない", () => {
      render(<AnnotationDetail {...withState({ running: false })} />);

      expect(screen.queryByRole("button", { name: "絵解きを取り消す" })).toBeNull();
    });

    // 押したボタンが消えるので、焦点が body に落ちる。「絵解く」へ戻す。
    it("取り消したら、焦点を「絵解く」へ戻す", () => {
      const running = withState({ running: true, jobId: "job-1" });
      const { rerender } = render(<AnnotationDetail {...running} />);

      const cancel = screen.getByRole("button", { name: "絵解きを取り消す" });
      cancel.focus();
      fireEvent.click(cancel);
      rerender(<AnnotationDetail {...withState({ running: false })} />);

      expect(screen.getByRole("button", { name: "絵解く" })).toHaveFocus();
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

  // ボードの背景（ADR 0093）。押す前に見せるのは背景だけで、結果には何を添えたかを
  // 載せる。黙って材料を足すと、出力がなぜそうなったかを読めない（中核思想 3）。
  describe("絵の外から添えるもの", () => {
    const context = (over: Partial<InterpretationResult["context"]> = {}) => ({
      premise: "",
      otherItems: [],
      omittedOtherItems: 0,
      ...over,
    });

    function withResult(premise: string, added: InterpretationResult["context"]) {
      const detailProps = props();
      const result: InterpretationResult = {
        summary: "s",
        contentHash: "h",
        items: [],
        context: added,
      };
      return {
        ...detailProps,
        interpretation: {
          ...detailProps.interpretation,
          premise,
          states: {
            "frame-1": {
              running: false,
              runs: [
                {
                  id: 1,
                  at: "2026-01-01T00:00:00Z",
                  granularity: "issue" as const,
                  result,
                },
              ],
              selectedId: 1,
            },
          },
        },
      };
    }

    it("背景を書いてあれば、押す前に添えることと中身を見せる", () => {
      const detailProps = props();
      render(
        <AnnotationDetail
          {...detailProps}
          interpretation={{ ...detailProps.interpretation, premise: "acme/web の話" }}
        />,
      );

      expect(screen.getByText("ボードの背景も添えて読みます")).toBeInTheDocument();
      expect(screen.getByText("acme/web の話")).toBeInTheDocument();
    });

    // 空でも催促しない（中核思想 1）。
    it("背景が空なら、押す前に何も言わない", () => {
      render(<AnnotationDetail {...props()} />);

      expect(screen.queryByText(/ボードの背景/)).toBeNull();
    });

    it("結果には、添えた背景と別の絵から作ったものを件数ごと見せる", () => {
      render(
        <AnnotationDetail
          {...withResult(
            "acme/web の話",
            context({
              premise: "acme/web の話",
              otherItems: [
                { kind: "epic", title: "決済" },
                { kind: "issue", title: "カードを登録する" },
              ],
              omittedOtherItems: 3,
            }),
          )}
        />,
      );

      // 開かなくても「何を何件」が読める。打ち切った件数も黙らない。
      expect(
        screen.getByText(
          "絵の外から添えたもの：ボードの背景・別の絵から作ったもの 2 件（上限を超えた 3 件は添えていません）",
        ),
      ).toBeInTheDocument();
      expect(screen.getByText("acme/web の話")).toBeInTheDocument();
      expect(screen.getByText("決済")).toBeInTheDocument();
      expect(screen.getByText("カードを登録する")).toBeInTheDocument();
      // いまの背景と同じなら、違うとは言わない。
      expect(screen.queryByText(/違うボードの背景/)).toBeNull();
    });

    // 背景を書き換えても引いた解釈は捨てない（ADR 0093）。古い背景で読んだと
    // 見分ける手掛かりはここだけ。
    it("結果に添えた背景がいまの背景と違えば、畳まずに言う", () => {
      render(
        <AnnotationDetail
          {...withResult("新しい背景", context({ premise: "古い背景" }))}
        />,
      );

      const note = screen.getByText(/いまとは違うボードの背景で読んだものです/);
      expect(note).toHaveTextContent(/読み直すかどうかは選べます/);
      expect(screen.getByText("古い背景")).toBeInTheDocument();
    });

    it("背景を書く前に読んだ結果なら、そう言う", () => {
      render(<AnnotationDetail {...withResult("新しい背景", context())} />);

      const note = screen.getByText(/ボードの背景を書く前に読んだものです/);
      // 打ち手の句は「違う背景で読んだ」ときと共通なので、こちらでも見る（#315 / #318）。
      expect(note).toHaveTextContent(/いまの背景で読み直すかどうかは選べます/);
      // 何も添えていないので、添えたものの見出しは出さない。
      expect(screen.queryByText(/絵の外から添えたもの/)).toBeNull();
    });

    it("何も添えず、背景も変わっていなければ何も出さない", () => {
      render(<AnnotationDetail {...withResult("", context())} />);

      expect(screen.queryByText(/絵の外から添えたもの/)).toBeNull();
      expect(screen.queryByText(/ボードの背景/)).toBeNull();
    });
  });
});
