import type { AppState, ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useEffect } from "react";
import { flushSync } from "react-dom";

import {
  arrowScrollDelta,
  edgeScrollVelocity,
  scrolledBy,
  visibleCanvasRect,
  type Rect,
  type ScrollDelta,
} from "./canvasScroll";

/** 見えているキャンバスの枠を、その場で測って返す。測れなければ null。 */
export type VisibleRect = () => Rect | null;

/**
 * キャンバスの枠（`.canvas-frame`）から、見えているキャンバスの枠を測る。
 *
 * 覆うものとして数えるのは、同じ `.board-body` に置いた右のパネルと畳んだ帯。
 * **注意の帯（衝突・上限超え・削除の確認）は数えなくてよい。** キャンバスを
 * 押し下げるだけで、枠の上には重ならない（`useBoardLayout`）。
 */
export function measureVisibleCanvas(frame: HTMLElement): Rect | null {
  const body = frame.closest(".board-body");
  const covers = body
    ? Array.from(
        body.querySelectorAll(
          ":scope > .side-panel:not([hidden]), :scope > .side-panel-rail",
        ),
        (el) => el.getBoundingClientRect(),
      )
    : [];
  return visibleCanvasRect(frame.getBoundingClientRect(), covers);
}

function scrollBy(
  api: ExcalidrawImperativeAPI,
  state: AppState,
  delta: ScrollDelta,
): void {
  api.updateScene({
    appState: scrolledBy(
      { scrollX: state.scrollX, scrollY: state.scrollY, zoom: state.zoom.value },
      delta,
    ),
  });
}

/**
 * 焦点がキャンバスにあるか。**キャンバスの枠そのもの（Excalidraw の
 * `.excalidraw-container`）か、どこにも無いときだけ。**
 *
 * キャンバスを押すと焦点は枠に移る。枠の**中**の部品（ツールバーのラジオ、
 * メニュー、ライブラリ）は矢印キーを自分で使うので含めない。etoki の部品
 * （右のパネルのタブ、入力欄、ダイアログ）も同じ。
 */
export function focusIsOnCanvas(active: Element | null): boolean {
  if (active === null || active === document.body) return true;
  return active.classList.contains("excalidraw-container");
}

/**
 * Excalidraw の状態から見て、矢印キーをスクロールに使ってよいか。
 *
 * **選んだ要素があれば使わない。** 矢印キーはそれを動かすためのもの。
 * 文字の編集中・ダイアログ・右クリックのメニューが開いているあいだも使わない。
 * メニューはキャンバスの位置に貼り付いているので、動かすと押した場所から離れる。
 */
export function canArrowScroll(
  state: Pick<
    AppState,
    "selectedElementIds" | "editingTextElement" | "openDialog" | "contextMenu"
  >,
): boolean {
  return (
    Object.keys(state.selectedElementIds).length === 0 &&
    state.editingTextElement === null &&
    state.openDialog === null &&
    state.contextMenu === null
  );
}

/**
 * 何も選んでいないとき、矢印キーでキャンバスをスクロールする（#250）。Shift で
 * 大きく動かす。**読むだけのロールでも使える。** キーボードでボードを見て回る
 * 手段がほかに無い。
 *
 * **修飾キー（⌘/Ctrl、Alt）付きは拾わない。** ⌘/Ctrl+矢印はフローチャートの
 * 図形を足す Excalidraw のショートカット。**変換中も拾わない。**
 *
 * Excalidraw も同じ keydown を受けて、選択が空のまま矢印の処理を走らせ、
 * `preventDefault` する。**`defaultPrevented` では「誰かが使った」を判定でき
 * ない**ので、選択が空かどうかを自分で見る。
 */
export function useArrowKeyScroll(
  api: ExcalidrawImperativeAPI | null,
  visibleRect: VisibleRect,
): void {
  useEffect(() => {
    if (!api) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey || e.isComposing) return;
      if (!focusIsOnCanvas(document.activeElement)) return;
      const rect = visibleRect();
      if (!rect) return;
      const delta = arrowScrollDelta(
        e.key,
        e.shiftKey,
        rect.right - rect.left,
        rect.bottom - rect.top,
      );
      if (!delta) return;
      const state = api.getAppState();
      if (!canArrowScroll(state)) return;

      e.preventDefault();
      scrollBy(api, state, delta);
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [api, visibleRect]);
}

/** 1 コマの経過時間の上限（ms）。タブが裏にあった直後に大きく飛ばないように。 */
const MAX_FRAME_MS = 50;

/**
 * 要素をドラッグしているあいだ、ポインターを端に寄せたらキャンバスをスクロール
 * する（#250）。**対象は frame に限らず、ドラッグするどの要素も同じ。**
 *
 * Excalidraw は掴んだ点からのずれを、`pointermove` のたびにシーンの座標で
 * 測り直して要素を置く（掴んだときの要素の位置 + ずれ）。スクロールしても
 * ポインターを止めていれば `pointermove` が来ないので、要素はシーンに残った
 * まま流れていく。**スクロールのたびに、同じ位置で `pointermove` を 1 つ投げる。**
 * Excalidraw はそれを新しいスクロールで測り直し、要素がポインターに付いてくる。
 *
 * - **投げる先は `document.body`。** Excalidraw のドラッグはこれを `window` で
 *   聞いていて、`target` が HTML 要素であることしか見ない。キャンバスへ投げると
 *   React のハンドラ（ホバーや他の人のポインター）まで通る。
 * - **スクロールは `flushSync` で確定させてから投げる。** Excalidraw は
 *   `pointermove` を次のコマでまとめて処理する。確定していないスクロールで
 *   測ると要素が 1 コマずつ遅れ、離した位置がポインターからずれる。
 * - **記録するのは本物の `pointermove` だけ**（`isTrusted`）。投げた写しを
 *   拾うと、修飾キーを読み直す先が自分になる。
 * - **流すのは要素をドラッグしているあいだだけ**（`selectedElementsAreBeingDragged`）。
 *   範囲選択や図形を描いている途中では流さない。離したあとに流れないのも
 *   この判定による。
 * - **離したらコマの繰り返しを止める。** 判定で流れは止まるが、止めないと
 *   次に押すまで毎コマ回り続ける。
 */
export function useDragEdgeScroll(
  api: ExcalidrawImperativeAPI | null,
  visibleRect: VisibleRect,
): void {
  useEffect(() => {
    if (!api) return;

    let pointer: PointerEvent | null = null;
    let frame = 0;
    let previous: number | null = null;

    const onMove = (e: PointerEvent) => {
      if (e.isTrusted) pointer = e;
    };

    const tick = (now: number) => {
      frame = requestAnimationFrame(tick);
      const elapsed = previous === null ? 0 : Math.min(now - previous, MAX_FRAME_MS);
      previous = now;
      if (pointer === null || elapsed === 0) return;

      const state = api.getAppState();
      if (!state.selectedElementsAreBeingDragged) return;
      const rect = visibleRect();
      if (!rect) return;
      const velocity = edgeScrollVelocity(pointer.clientX, pointer.clientY, rect);
      if (velocity.x === 0 && velocity.y === 0) return;

      const seconds = elapsed / 1000;
      flushSync(() =>
        scrollBy(api, state, { x: velocity.x * seconds, y: velocity.y * seconds }),
      );
      document.body.dispatchEvent(
        new PointerEvent("pointermove", {
          bubbles: true,
          cancelable: true,
          clientX: pointer.clientX,
          clientY: pointer.clientY,
          screenX: pointer.screenX,
          screenY: pointer.screenY,
          button: pointer.button,
          buttons: pointer.buttons,
          pointerId: pointer.pointerId,
          pointerType: pointer.pointerType,
          isPrimary: pointer.isPrimary,
          pressure: pointer.pressure,
          shiftKey: pointer.shiftKey,
          ctrlKey: pointer.ctrlKey,
          metaKey: pointer.metaKey,
          altKey: pointer.altKey,
        }),
      );
    };

    const stop = () => {
      cancelAnimationFrame(frame);
      frame = 0;
      pointer = null;
      previous = null;
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
      window.removeEventListener("blur", stop);
    };

    const unsubscribe = api.onPointerDown((_tool, _state, event) => {
      stop();
      pointer = event.nativeEvent;
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", stop);
      window.addEventListener("pointercancel", stop);
      window.addEventListener("blur", stop);
      frame = requestAnimationFrame(tick);
    });

    return () => {
      unsubscribe();
      stop();
    };
  }, [api, visibleRect]);
}
