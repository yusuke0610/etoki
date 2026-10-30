import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { AnnotationStatus } from "../api/types";
import type { AnnotationMeta } from "../excalidraw/annotation";
import type { InterpretOptions } from "./annotations/useInterpretations";
import { useEkidoki } from "./useEkidoki";

/**
 * 「絵解き」の流れ（#370 で BoardPage から切り出し、ADR 0077）。
 *
 * **守りたいのは順番。** 保存 → 一覧の取り直し → 確かめ直し → 開く・読む。
 * `.claude/rules/async-ui.md` に #259 の指摘が 5 つ積まれている場所で、崩すと
 * 保存済みのテキストと画面の画像が食い違ったまま読むか、待つあいだに付けた
 * 注釈の詳細が開いた瞬間に閉じる。E2E では保存と取り直しの届く順を決め
 * きれないので、待つあいだに変わる経路はここで押さえる。
 */

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const annotation = (id: string) =>
  ({ id, name: id, granularity: "", state: "uncreated" }) satisfies AnnotationStatus;

const meta = (granularity: AnnotationMeta["granularity"]): AnnotationMeta => ({
  granularity,
});

/**
 * 呼ばれた順を 1 本の列に積む。保存と取り直しと開く・読むの順を見る。
 */
function setup(
  initial: { dirty?: boolean; metas?: Record<string, AnnotationMeta> | null } = {},
) {
  const calls: string[] = [];
  // キャンバスが未保存か。`useDirtyScene` と同じく、呼んだ時点の値を返す。
  const canvas = { dirty: initial.dirty ?? false };
  const isDirty = () => canvas.dirty;
  // 既定の保存は、送ったシーンがそのまま基準になる（待つあいだに変わらない）。
  const save = vi.fn<() => Promise<boolean>>(async () => {
    calls.push("save");
    canvas.dirty = false;
    return true;
  });
  const refreshAnnotations = vi.fn<() => Promise<AnnotationStatus[] | null>>(async () => {
    calls.push("refresh");
    return [annotation("a1")];
  });
  const interpret = vi.fn<(id: string, options?: InterpretOptions) => Promise<void>>(
    async (id) => {
      calls.push(`interpret:${id}`);
    },
  );
  const openDetail = vi.fn<(id: string) => void>((id) => {
    calls.push(`open:${id}`);
  });
  const showAnnotationList = vi.fn<() => void>(() => {
    calls.push("list");
  });

  const hook = renderHook(
    ({ metas }: { metas: Record<string, AnnotationMeta> | null }) =>
      useEkidoki({
        isDirty,
        save,
        refreshAnnotations,
        canvasMetas: metas,
        interpret,
        openDetail,
        showAnnotationList,
      }),
    { initialProps: { metas: initial.metas ?? null } },
  );
  return { hook, calls, canvas, save, refreshAnnotations, interpret, openDetail };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useEkidoki", () => {
  describe("右上の「絵解き」", () => {
    it("保存済みなら保存せず、一覧だけ取り直してから開く", async () => {
      const { hook, calls } = setup({ dirty: false });

      await act(() => hook.result.current.startEkidoki());

      // 保存は引いた解釈をすべて捨てるので、要らないときに保存しない。一覧は、
      // 手元のものが届いていないか古いことがあるので取り直す。
      expect(calls).toEqual(["refresh", "open:a1"]);
    });

    it("未保存なら保存してから取り直し、そのあとで開く", async () => {
      const { hook, calls } = setup({ dirty: true });

      await act(() => hook.result.current.startEkidoki());

      expect(calls).toEqual(["save", "refresh", "open:a1"]);
    });

    it("保存できなければ、取り直さず開かない", async () => {
      const { hook, calls, save } = setup({ dirty: true });
      save.mockImplementation(async () => {
        calls.push("save");
        return false;
      });

      await act(() => hook.result.current.startEkidoki());

      // 理由は通知と衝突の帯が言う。
      expect(calls).toEqual(["save"]);
    });

    it("一覧を取り直せなければ開かない", async () => {
      const { hook, calls, refreshAnnotations } = setup({ dirty: false });
      refreshAnnotations.mockImplementation(async () => {
        calls.push("refresh");
        return null;
      });

      await act(() => hook.result.current.startEkidoki());

      // 前の一覧で数えると、別の面を開く。
      expect(calls).toEqual(["refresh"]);
    });

    it.each([
      { name: "1 件ならその詳細", listed: ["a1"], opened: "open:a1" },
      { name: "0 件なら一覧", listed: [], opened: "list" },
      { name: "複数なら一覧", listed: ["a1", "a2"], opened: "list" },
    ])("開く先は取り直した一覧の件数で決める（$name）", async ({ listed, opened }) => {
      const { hook, calls, refreshAnnotations } = setup();
      refreshAnnotations.mockImplementation(async () => {
        calls.push("refresh");
        return listed.map(annotation);
      });

      await act(() => hook.result.current.startEkidoki());

      expect(calls).toEqual(["refresh", opened]);
    });

    it("キャンバスに在るフレームの数ではなく、保存済みの一覧で数える", async () => {
      // フレームが画面に 2 つ出ていても、保存済みの注釈が 1 件ならその詳細を開く。
      const { hook, calls } = setup({
        metas: { a1: meta("issue"), b1: meta("issue") },
      });

      await act(() => hook.result.current.startEkidoki());

      expect(calls).toEqual(["refresh", "open:a1"]);
    });
  });

  describe("詳細の「絵解く」", () => {
    it("未保存なら保存して取り直してから読む", async () => {
      const { hook, calls } = setup({ dirty: true });

      await act(() => hook.result.current.interpretAfterSave("a1"));

      // 先に読むと、保存が引いた結果を捨てるので、出た結果が直後に消える。
      expect(calls).toEqual(["save", "refresh", "interpret:a1"]);
    });

    it("保存できなければ読まない", async () => {
      const { hook, interpret, save } = setup({ dirty: true });
      save.mockResolvedValue(false);

      await act(() => hook.result.current.interpretAfterSave("a1"));

      expect(interpret).not.toHaveBeenCalled();
    });

    it("一覧を取り直せなければ読まない", async () => {
      const { hook, interpret, refreshAnnotations } = setup({ dirty: true });
      refreshAnnotations.mockResolvedValue(null);

      await act(() => hook.result.current.interpretAfterSave("a1"));

      // 消えたかどうかを前の一覧では決められない。
      expect(interpret).not.toHaveBeenCalled();
    });

    it("保存で消えた注釈は読まない", async () => {
      const { hook, interpret, refreshAnnotations } = setup({ dirty: true });
      refreshAnnotations.mockResolvedValue([annotation("a2")]);

      await act(() => hook.result.current.interpretAfterSave("a1"));

      expect(interpret).not.toHaveBeenCalled();
    });

    it("フレームが画面に無くても、保存済みの一覧に在れば読む", async () => {
      // テキストは保存済みシーンから取るので、画像が無くても読める（ADR 0018）。
      const { hook, interpret } = setup({ metas: {} });

      await act(() => hook.result.current.interpretAfterSave("a1"));

      expect(interpret).toHaveBeenCalledWith("a1", { granularity: undefined });
    });

    it("控える粒度は、押した時点ではなく読む時点のキャンバスの値", async () => {
      const { hook, interpret, save, canvas } = setup({
        dirty: true,
        metas: { a1: meta("issue") },
      });
      const saving = deferred<boolean>();
      save.mockReturnValueOnce(saving.promise);

      let done!: Promise<void>;
      act(() => {
        done = hook.result.current.interpretAfterSave("a1");
      });
      // 保存を待つあいだに選び直し、それも保存に載った。
      hook.rerender({ metas: { a1: meta("epic") } });
      await act(async () => {
        canvas.dirty = false;
        saving.resolve(true);
        await done;
      });

      expect(interpret).toHaveBeenCalledWith("a1", { granularity: "epic" });
    });
  });

  // 確かめ直しは、保存を挟む導線の全部に掛ける（#259）。右上の「絵解き」だけ
  // 直して詳細の「絵解く」が漏れた、という形を両方の入口で見る。
  describe.each([
    {
      entry: "右上の「絵解き」",
      run: (h: ReturnType<typeof setup>["hook"]) => h.result.current.startEkidoki(),
      last: "open:a1",
    },
    {
      entry: "詳細の「絵解く」",
      run: (h: ReturnType<typeof setup>["hook"]) =>
        h.result.current.interpretAfterSave("a1"),
      last: "interpret:a1",
    },
  ])("$entry：待つあいだの変更", ({ run, last }) => {
    it("保存を待つあいだに描き足されたら、保存し直してから取り直す", async () => {
      const { hook, calls, canvas, save } = setup({ dirty: true });
      // 1 回目の保存が送ったのは押した時点のシーン。待つあいだの描き足しは
      // 未保存のまま残る（`markSaved`）。
      save.mockImplementationOnce(async () => {
        calls.push("save");
        canvas.dirty = true;
        return true;
      });

      await act(() => run(hook));

      expect(calls).toEqual(["save", "save", "refresh", last]);
    });

    it("取り直しを待つあいだに描き足されたら、保存し直して取り直し直す", async () => {
      const { hook, calls, canvas, refreshAnnotations } = setup({ dirty: false });
      const first = deferred<AnnotationStatus[] | null>();
      refreshAnnotations.mockImplementationOnce(() => {
        calls.push("refresh");
        return first.promise;
      });

      let done!: Promise<void>;
      act(() => {
        done = run(hook);
      });
      // **確かめ直すのは最後の待ち（取り直し）のあと。** 保存の直後だけで
      // 確かめると、取り直すあいだの変更を見逃す。
      await act(async () => {
        canvas.dirty = true;
        first.resolve([annotation("a1")]);
        await done;
      });

      expect(calls).toEqual(["refresh", "save", "refresh", last]);
    });
  });

  it("返す束は、描き直しても同じものを返す", () => {
    const { hook } = setup();
    const first = hook.result.current;

    hook.rerender({ metas: null });

    expect(hook.result.current).toBe(first);
  });
});
