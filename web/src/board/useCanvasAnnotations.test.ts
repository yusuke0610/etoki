import { CaptureUpdateAction } from "@excalidraw/excalidraw";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Theme } from "../app/theme";
import type { SceneElement } from "../excalidraw/annotation";
import { sceneSignature } from "../excalidraw/dirty";
import { useCanvasAnnotations } from "./useCanvasAnnotations";

/**
 * キャンバスの写しと要素の書き換え（#371 で `BoardPage` から切り出し）。
 *
 * **守りたいのは 3 つ。**
 *
 * - `handleChange` と `updateElements` の同一性（ADR 0065）。崩すと Excalidraw の
 *   `onChange` が差し替わり続けて `Maximum update depth exceeded` で画面ごと
 *   落ちる。vitest も `tsc` も eslint も通ったままなので、ここで見る。
 * - etoki の書き換えは「元に戻す」に積む（#144）。
 * - 未保存の判定に渡す署名は、保存が書くものだけから作る（テーマは入れない）。
 */

const frame = (id: string): SceneElement => ({ id, type: "frame", name: id });
const annotation = (id: string): SceneElement => ({
  id,
  type: "frame",
  name: id,
  customData: { etoki: { granularity: "epic" } },
});

type AppState = Parameters<ReturnType<typeof useCanvasAnnotations>["handleChange"]>[1];

const appState = (patch: Partial<AppState> = {}): AppState => ({
  selectedElementIds: {},
  scrollX: 0,
  scrollY: 0,
  zoom: { value: 1 },
  viewBackgroundColor: "#ffffff",
  theme: "light",
  ...patch,
});

function setup(initial: { theme?: Theme } = {}) {
  const canvas = {
    elements: [frame("f1"), annotation("a1")] as SceneElement[],
    background: "#ffffff",
  };
  const updateScene = vi.fn();
  const api = {
    getAppState: () => ({ viewBackgroundColor: canvas.background }),
    getSceneElements: () => canvas.elements,
    updateScene,
  } as unknown as ExcalidrawImperativeAPI;
  const onThemeChange = vi.fn<(theme: Theme) => void>();
  const applySignature = vi.fn<(signature: string) => void>();
  const scheduleMeasure = vi.fn<() => void>();

  const hook = renderHook(
    ({ theme }: { theme: Theme }) =>
      useCanvasAnnotations({
        api,
        theme,
        onThemeChange,
        applySignature,
        scheduleMeasure,
      }),
    { initialProps: { theme: initial.theme ?? "light" } },
  );
  return { hook, canvas, updateScene, onThemeChange, applySignature, scheduleMeasure };
}

/** `updateScene` に最後に渡した要素。 */
function lastSceneElements(updateScene: ReturnType<typeof vi.fn>): SceneElement[] {
  const call = updateScene.mock.lastCall as [{ elements: SceneElement[] }] | undefined;
  return call?.[0].elements ?? [];
}

describe("useCanvasAnnotations", () => {
  describe("同一性（ADR 0065）", () => {
    it("onChange が state を書いて描き直されても、handleChange と updateElements は同じものを返す", () => {
      const { hook, canvas } = setup();
      const { handleChange, updateElements } = hook.result.current;

      act(() => hook.result.current.handleChange(canvas.elements, appState()));
      act(() =>
        hook.result.current.handleChange(
          canvas.elements,
          appState({ selectedElementIds: { f1: true }, scrollX: 40 }),
        ),
      );
      act(() => hook.result.current.updateElements(canvas.elements));

      expect(hook.result.current.handleChange).toBe(handleChange);
      expect(hook.result.current.updateElements).toBe(updateElements);
    });
  });

  describe("キャンバスの写し", () => {
    it("最初の onChange までは「まだ聞いていない」（null）で、空と混ぜない", () => {
      const { hook, canvas } = setup();
      expect(hook.result.current.canvasFrameIds).toBeNull();
      expect(hook.result.current.canvasMetas).toBeNull();
      expect(hook.result.current.canvasNames).toBeNull();

      act(() => hook.result.current.handleChange(canvas.elements, appState()));

      expect(hook.result.current.canvasFrameIds).toEqual(["f1", "a1"]);
      expect(hook.result.current.canvasMetas).toEqual({ a1: { granularity: "epic" } });
      expect(hook.result.current.canvasNames).toEqual({ a1: "a1" });
    });

    it("未保存の判定には、要素と背景色の署名を渡す", () => {
      const { hook, canvas, applySignature, scheduleMeasure } = setup();

      act(() =>
        hook.result.current.handleChange(
          canvas.elements,
          appState({ viewBackgroundColor: "#ffc9c9" }),
        ),
      );

      expect(applySignature).toHaveBeenLastCalledWith(
        sceneSignature(canvas.elements, "#ffc9c9"),
      );
      expect(scheduleMeasure).toHaveBeenCalledTimes(1);
    });

    it("テーマだけを切り替えても、署名は変わらない", () => {
      // テーマは保存が書かないので、入れると切り替えただけで未保存になる。
      const { hook, canvas, applySignature } = setup();

      act(() => hook.result.current.handleChange(canvas.elements, appState()));
      act(() =>
        hook.result.current.handleChange(canvas.elements, appState({ theme: "dark" })),
      );

      expect(applySignature.mock.calls[1]?.[0]).toBe(applySignature.mock.calls[0]?.[0]);
    });
  });

  describe("テーマ（ADR 0055）", () => {
    it("キャンバスのメニューで切り替えたら、持ち主に返す", () => {
      const { hook, canvas, onThemeChange } = setup({ theme: "light" });

      act(() => hook.result.current.handleChange(canvas.elements, appState()));
      expect(onThemeChange).not.toHaveBeenCalled();

      act(() =>
        hook.result.current.handleChange(canvas.elements, appState({ theme: "dark" })),
      );
      expect(onThemeChange).toHaveBeenCalledWith("dark");
    });

    it("持ち主が先に変えた直後に届いた古いテーマを、選び直しと取り違えない", () => {
      const { hook, canvas, onThemeChange } = setup({ theme: "light" });

      // OS の設定が変わり、持ち主が props を dark に差し替えた。キャンバスは
      // まだ古いテーマ（light）のまま onChange を出す。
      hook.rerender({ theme: "dark" });
      act(() => hook.result.current.handleChange(canvas.elements, appState()));

      // props と比べると、ここで light に戻してしまい、OS に従うのをやめる。
      expect(onThemeChange).not.toHaveBeenCalled();
    });
  });

  describe("書き換え（#144）", () => {
    it("「元に戻す」に積む形で書き、onChange を待たずに署名を渡す", () => {
      const { hook, canvas, updateScene, applySignature } = setup();
      const next = [frame("f1")];

      act(() => hook.result.current.updateElements(next));

      // 既定（EVENTUALLY）のままだと積まれず、戻すと直前に描いたものが消える。
      expect(updateScene).toHaveBeenCalledWith(
        expect.objectContaining({
          elements: next,
          captureUpdate: CaptureUpdateAction.IMMEDIATELY,
        }),
      );
      expect(applySignature).toHaveBeenLastCalledWith(
        sceneSignature(next, canvas.background),
      );
    });

    it("背景色も変える書き換え（取り込み）は、渡された新しい色で署名する", () => {
      const { hook, applySignature } = setup();
      const next = [frame("f1")];

      act(() =>
        hook.result.current.updateElements(next, { viewBackgroundColor: "#a5d8ff" }),
      );

      // updateScene の直後に getAppState から読むと、まだ前の色が返りうる。
      expect(applySignature).toHaveBeenLastCalledWith(sceneSignature(next, "#a5d8ff"));
    });

    it.each([
      {
        name: "注釈にする",
        run: (h: ReturnType<typeof setup>["hook"]) =>
          h.result.current.handleMark("f1", "issue"),
        check: (els: SceneElement[]) =>
          expect(els.find((e) => e.id === "f1")?.customData).toEqual({
            etoki: { granularity: "issue" },
          }),
      },
      {
        name: "種別を変える",
        run: (h: ReturnType<typeof setup>["hook"]) =>
          h.result.current.handleChangeAnnotationKind("a1", "sequence"),
        check: (els: SceneElement[]) =>
          expect(els.find((e) => e.id === "a1")?.customData).toEqual({
            etoki: { granularity: "epic", kind: "sequence" },
          }),
      },
      {
        name: "名前を変える",
        run: (h: ReturnType<typeof setup>["hook"]) =>
          h.result.current.handleChangeAnnotationName("a1", "決済"),
        check: (els: SceneElement[]) =>
          expect(els.find((e) => e.id === "a1")?.name).toBe("決済"),
      },
      {
        name: "注釈から外す",
        run: (h: ReturnType<typeof setup>["hook"]) => h.result.current.handleUnmark("a1"),
        check: (els: SceneElement[]) =>
          expect(els.find((e) => e.id === "a1")?.customData).toEqual({}),
      },
    ])(
      "$name操作も、いまのキャンバスに対して「元に戻す」に積む形で書く",
      ({ run, check }) => {
        const { hook, updateScene } = setup();

        act(() => run(hook));

        expect(updateScene).toHaveBeenCalledWith(
          expect.objectContaining({ captureUpdate: CaptureUpdateAction.IMMEDIATELY }),
        );
        check(lastSceneElements(updateScene));
      },
    );
  });
});
