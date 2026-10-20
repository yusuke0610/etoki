import { useEffect, type RefObject } from "react";

/** キャンバスのメニューの入口（≡）の読み上げの名前（#299）。 */
export const MENU_TRIGGER_NAME = "メニュー";

/** Excalidraw が描くメニューの入口のうち、まだ名前の無いもの。 */
const UNNAMED_TRIGGER = '[data-testid="main-menu-trigger"]:not([aria-label])';

function nameTriggers(root: ParentNode): void {
  for (const trigger of root.querySelectorAll(UNNAMED_TRIGGER)) {
    trigger.setAttribute("aria-label", MENU_TRIGGER_NAME);
  }
}

/**
 * Excalidraw のメニューの入口（≡）に、読み上げの名前を付けておく（#299、
 * ADR 0039）。
 *
 * 入口は Excalidraw が描く、アイコンだけのボタン。etoki の操作（一覧へ戻る・
 * 共有・書き出しなど、ADR 0065）はこの中にあるのに、読み上げでは「ボタン」と
 * しか聞こえなかった。**0.18.1 の `MainMenu` は入口に名前を渡す口を持たない**
 * ので、描かれたボタンに外から付ける。
 *
 * - **作り直されたら付け直す。** 置き方が変わる（広い画面 ⇄ スマホ）と、
 *   Excalidraw は入口を作り直す。最初の 1 つにだけ付けると、切り替えたあとは
 *   名前が消える。キャンバスの枠の下の追加を見張る。
 * - **名前が付いていれば触らない。** ライブラリが自分で付けるようになったら、
 *   そちらに任せる。
 * - **ライブラリの DOM に寄りかかっている。** 版を上げて印（`data-testid`）が
 *   変わると黙って外れる。E2E（`openBoardMenu` が入口を名前で引く）と axe が
 *   見ている。
 *
 * `ready` は Excalidraw が描かれたこと（`excalidrawAPI` で受け取った API）。
 * 描かれる前は枠の中に入口が無い。
 */
export function useMenuTriggerName(
  frame: RefObject<HTMLElement | null>,
  ready: unknown,
): void {
  useEffect(() => {
    const root = frame.current;
    if (ready === null || root === null) return;
    nameTriggers(root);
    const observer = new MutationObserver(() => nameTriggers(root));
    observer.observe(root, { childList: true, subtree: true });
    return () => observer.disconnect();
  }, [frame, ready]);
}
