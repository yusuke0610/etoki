import { act, renderHook } from "@testing-library/react";
import type { KeyboardEvent } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useNameDraft } from "./useNameDraft";

/**
 * 注釈の名前欄の書きかけ（#384 で `AnnotationFace` から切り出し）。
 *
 * **守りたいのは、触っていない名前を書き戻さないことと、書けない対象に書かない
 * こと。** どちらも `.claude/rules/async-ui.md` に積まれた形（#261 / #308）で、
 * 破ると開いて閉じただけで未保存になるか、入力した名前が黙って消える。
 */

type Props = { name: string; canRename: boolean };

function setup(initial: Partial<Props> = {}) {
  const onChangeName = vi.fn<(frameId: string, name: string) => void>();
  const hook = renderHook(
    ({ name, canRename }: Props) =>
      useNameDraft({ id: "a1", name, canRename, onChangeName }),
    {
      initialProps: {
        name: initial.name ?? "決済",
        canRename: initial.canRename ?? true,
      },
    },
  );
  return { hook, onChangeName };
}

/** 欄のキー入力。 */
function key(k: string, isComposing = false) {
  return {
    key: k,
    nativeEvent: { isComposing },
    preventDefault: vi.fn(),
    stopPropagation: vi.fn(),
  } as unknown as KeyboardEvent<HTMLInputElement> & {
    preventDefault: ReturnType<typeof vi.fn>;
    stopPropagation: ReturnType<typeof vi.fn>;
  };
}

afterEach(() => {
  vi.restoreAllMocks();
  document.body.replaceChildren();
});

describe("useNameDraft", () => {
  it("ラベルを押すと、いまの名前から書き始める", () => {
    const { hook } = setup({ name: "決済" });
    expect(hook.result.current.draft).toBeNull();

    act(() => hook.result.current.start());

    expect(hook.result.current.draft).toBe("決済");
  });

  it("確定したら書き、欄を閉じる", () => {
    const { hook, onChangeName } = setup();
    act(() => hook.result.current.start());
    act(() => hook.result.current.change("支払い"));

    act(() => hook.result.current.commit());

    expect(onChangeName).toHaveBeenCalledWith("a1", "支払い");
    expect(hook.result.current.draft).toBeNull();
  });

  it("打っているあいだは書かない", () => {
    const { hook, onChangeName } = setup();
    act(() => hook.result.current.start());

    act(() => hook.result.current.change("支"));
    act(() => hook.result.current.change("支払"));

    // 1 文字ごとに書くと、打つたびに未保存になり「元に戻す」に積まれる。
    expect(onChangeName).not.toHaveBeenCalled();
  });

  it("変えていなければ書かない（前後の空白だけの違いも）", () => {
    const { hook, onChangeName } = setup({ name: "決済" });
    act(() => hook.result.current.start());
    act(() => hook.result.current.change("  決済 "));

    act(() => hook.result.current.commit());

    // 書くと、開いて閉じただけで未保存になる。
    expect(onChangeName).not.toHaveBeenCalled();
  });

  it("変えたかどうかは開いた時点の名前と比べ、あとから届いた名前とは比べない（#308）", () => {
    // キャンバスの名前が分かる前に、保存済みの名前で欄を開いた。
    const { hook, onChangeName } = setup({ name: "保存済みの名前" });
    act(() => hook.result.current.start());

    // 開いているあいだに、キャンバスの名前が届いた。
    hook.rerender({ name: "キャンバスの名前", canRename: true });
    act(() => hook.result.current.commit());

    // 触っていないのに、古い名前をキャンバスへ書き戻さない。
    expect(onChangeName).not.toHaveBeenCalled();
  });

  it("書いている途中で変えられなくなったら書きかけを捨て、条件が戻っても復活させない（#308）", () => {
    const { hook, onChangeName } = setup();
    act(() => hook.result.current.start());
    act(() => hook.result.current.change("支払い"));

    // 注釈から外した・キャンバスから消えた・読むだけに変わった。
    hook.rerender({ name: "決済", canRename: false });
    expect(hook.result.current.draft).toBeNull();

    hook.rerender({ name: "決済", canRename: true });
    expect(hook.result.current.draft).toBeNull();

    act(() => hook.result.current.commit());
    expect(onChangeName).not.toHaveBeenCalled();
  });

  describe("欄のキー", () => {
    it("Enter で確定する。変換を確定する Enter では書かない", () => {
      const { hook, onChangeName } = setup();
      act(() => hook.result.current.start());
      act(() => hook.result.current.change("支払い"));

      const composing = key("Enter", true);
      act(() => hook.result.current.onInputKeyDown(composing));
      // 書くと、変換の途中の名前がキャンバスに載る。
      expect(onChangeName).not.toHaveBeenCalled();
      expect(hook.result.current.draft).toBe("支払い");

      const enter = key("Enter");
      act(() => hook.result.current.onInputKeyDown(enter));
      expect(onChangeName).toHaveBeenCalledWith("a1", "支払い");
      expect(enter.preventDefault).toHaveBeenCalled();
    });

    it("Escape は書きかけを捨てるだけで、面までは閉じさせない", () => {
      const { hook, onChangeName } = setup();
      act(() => hook.result.current.start());
      act(() => hook.result.current.change("支払い"));

      const escape = key("Escape");
      act(() => hook.result.current.onInputKeyDown(escape));

      expect(hook.result.current.draft).toBeNull();
      expect(onChangeName).not.toHaveBeenCalled();
      // 面の Escape（閉じる）まで泡立たせない。
      expect(escape.stopPropagation).toHaveBeenCalled();
    });

    it.each([
      ["Enter", key("Enter")],
      ["Escape", key("Escape")],
    ])("%s で閉じたら、焦点をラベルへ戻す", (_name, event) => {
      const { hook } = setup();
      const label = document.createElement("button");
      document.body.append(label);
      act(() => hook.result.current.attachLabel(label));
      act(() => hook.result.current.start());

      act(() => hook.result.current.onInputKeyDown(event));

      expect(document.activeElement).toBe(label);
    });

    it("欄を離れて確定したときは、離れた先の焦点を奪わない", () => {
      const { hook } = setup();
      const label = document.createElement("button");
      const elsewhere = document.createElement("button");
      document.body.append(label, elsewhere);
      act(() => hook.result.current.attachLabel(label));
      act(() => hook.result.current.start());
      act(() => hook.result.current.change("支払い"));

      elsewhere.focus();
      act(() => hook.result.current.commit());

      expect(document.activeElement).toBe(elsewhere);
    });
  });
});
