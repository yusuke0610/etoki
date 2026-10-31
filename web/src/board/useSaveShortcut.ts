import { useEffect } from "react";

/**
 * Ctrl / Cmd + S で保存する（issue #145）。
 *
 * 保存は明示操作だけ（ADR 0021）なので、押すまでの手数の少なさがそのまま
 * 値打ちになる。**誰も拾わないと既定の動作（ブラウザの「ページを保存」）に
 * 落ちる。** Excalidraw 側の Ctrl+S は `saveToActiveFile` だが、etoki は
 * それを `UIOptions` から外してある（ADR 0045）ので、押しても何も起きず
 * `preventDefault` もされない。
 *
 * **押せないときも既定の動作は止める。** 「保存できなかった」の代わりに
 * ブラウザの保存ダイアログが出るのは、押せない理由を見せるどころではない。
 *
 * **Shift は拾わない。** Ctrl/Cmd+Shift+S は Excalidraw 側の書き出しで、
 * 別の操作（`web/e2e/scene.spec.ts` が走らないことを見ている）。
 *
 * **入力欄にフォーカスがあっても同じ扱いにする。** この画面で Ctrl+S が
 * 指しうる保存はシーンの保存 1 つだけで、ボード名の変更には専用のボタンが
 * ある。フォーカス位置で意味が変わるほうが、習慣で押す人には読めない。
 *
 * `canSave` はボタン（「保存」と「絵解き」）の `disabled` と同じ式を渡す
 * （`BoardPage`）。2 つに分けて書くと、片方だけに条件を足したときに、押せない
 * はずの経路がキーボードからだけ通る。
 */
export function useSaveShortcut(canSave: boolean, save: () => Promise<unknown>) {
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      // **`e.key` だけで見ない。** 非ラテン配列では物理の S を押しても `e.key` が
      // "s" にならず、保存されないままブラウザの既定（ページを保存）が開く。
      // 物理キーの位置（`e.code`）も見る。Ctrl+S の習慣は位置で覚えているため。
      if (e.key !== "s" && e.key !== "S" && e.code !== "KeyS") return;
      if (!(e.ctrlKey || e.metaKey) || e.shiftKey || e.altKey) return;

      e.preventDefault();
      if (!canSave) return;
      void save();
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [canSave, save]);
}
