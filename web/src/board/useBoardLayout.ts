import { useLayoutEffect, useState } from "react";

import { boardLayout, type BoardLayout } from "./layout";

/**
 * 右のパネルの幅を、配色と寸法の変数（`--side-panel-width`、`tokens.css`）から
 * 読む。**幅を TypeScript に写さない。** 写すと、パネルの幅を変えた日に並べる
 * 境目だけが古いまま残る。読めなければ null（`boardLayout` は並べない側に倒す）。
 */
function panelWidthOf(element: Element): number | null {
  const value = parseFloat(
    getComputedStyle(element).getPropertyValue("--side-panel-width"),
  );
  return Number.isFinite(value) ? value : null;
}

/**
 * 測るまでの見込み。画面の大きさで決める。注意の帯はまだ出ていないので、
 * たいていはこのまま当たる。外れても、描く前に測り直す（下の
 * `useLayoutEffect`）。
 */
function initialLayout(): BoardLayout {
  const root = document.documentElement;
  return boardLayout(root.clientWidth, root.clientHeight, panelWidthOf(root));
}

/**
 * ボードの置き方（`layout.ts`）を、いまの大きさから決め続ける（#199）。返す
 * ref は、注意の帯を除いた領域（etoki の帯とキャンバスとパネルを包む枠）に
 * 付ける。
 *
 * **画面ではなく、その枠を測る。** Excalidraw がモバイル用 UI に切り替える
 * 条件はキャンバスの枠の大きさで決まり、衝突・上限超え・削除の確認の帯は
 * キャンバスを押し下げる。画面の大きさで決めると、帯が出ているあいだだけ
 * Excalidraw がモバイル用 UI になり、下の帯の中身が消える幅ができる。
 *
 * **etoki の帯（スマホの置き方の上の帯）は枠の中に置く。** 外に置くと、帯を
 * 出したことで枠が縮んで判定が動き、同じ大きさの画面で置き方が履歴に依存する。
 */
export function useBoardLayout(): [BoardLayout, (element: HTMLElement | null) => void] {
  const [element, setElement] = useState<HTMLElement | null>(null);
  const [layout, setLayout] = useState<BoardLayout>(initialLayout);

  /*
    **描く前に測る**（`useLayoutEffect`）。見込みが外れたまま 1 度描くと、
    Excalidraw がその大きさで UI を組んでから組み直し、ちらつく。effect で
    state を置くのは、大きさが描いた DOM からしか読めないため（props や state
    から導ける値ではない）。
  */
  useLayoutEffect(() => {
    if (!element) return;
    const measure = () => {
      const { width, height } = element.getBoundingClientRect();
      setLayout(boardLayout(width, height, panelWidthOf(element)));
    };
    measure();

    // 画面の大きさだけでなく、帯が出入りして枠の高さが変わったときにも測り直す。
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [element]);

  return [layout, setElement];
}
