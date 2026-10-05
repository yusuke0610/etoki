import type { AnnotationStatus } from "../api/types";
import { safeLocalStorage } from "../app/storage";
import type { RailBadge } from "./SidePanel";

const STORAGE_KEY = "etoki.sidePanel.collapsed";

/**
 * 右のパネルを畳んでいたか（#202）。**端末ごとに覚え、ボードをまたいで同じに
 * する。**
 *
 * 読めないとき・知らない値のときは開いた状態（false）で始める。畳んだまま
 * 始まると、初めて開いた人は注釈の状態がどこにあるかを探すことになる。
 */
export function readPanelCollapsed(
  storage: Storage | undefined = safeLocalStorage(),
): boolean {
  try {
    return storage?.getItem(STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

/** 開閉を覚える。失敗は握りつぶす。覚えられないだけで、いまの画面は畳める。 */
export function writePanelCollapsed(
  collapsed: boolean,
  storage: Storage | undefined = safeLocalStorage(),
): void {
  try {
    storage?.setItem(STORAGE_KEY, collapsed ? "1" : "0");
  } catch {
    // 覚えられないだけ。
  }
}

/**
 * 畳んだ帯の「注釈」に添える件数（#202）。手を打つ必要があるもの（未作成・
 * 変更あり）だけを数える。
 *
 * **注釈の一覧が壊れていても投げない。** ここは右のパネルの境界
 * （`ErrorBoundary`）の外、`BoardPage` の描画の中で数える。投げると画面ごと
 * 落ち、保存していないブレストが消える（ADR 0027）。壊れた要素は数えない。
 * 壊れていること自体は、注釈のパネルが自分の境界の中で出す。
 */
export function railBadgesOf(annotations: readonly AnnotationStatus[]): RailBadge[] {
  let uncreated = 0;
  let changed = 0;
  if (Array.isArray(annotations)) {
    // 契約の外の値（null など）も来うるものとして読む。
    for (const a of annotations as readonly (Partial<AnnotationStatus> | null)[]) {
      if (a?.state === "uncreated") uncreated++;
      else if (a?.state === "changed") changed++;
    }
  }
  return [
    { label: "未作成", state: "uncreated", count: uncreated },
    { label: "変更あり", state: "changed", count: changed },
  ];
}
