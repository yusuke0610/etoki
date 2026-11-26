import { useCallback, useEffect, useRef } from "react";

import type { BoardTarget } from "../../api/types";
import { ErrorBoundary } from "../../app/ErrorBoundary";
import { RepositoryPicker } from "./RepositoryPicker";

type Props = {
  /**
   * 開いているか。開くのはメニューの作成先の項目と、作成の帯の「作成先を選ぶ」
   * （`useBoardNavigation` の `startPicking`）。URL から開くこともある（ADR 0059）。
   */
  open: boolean;
  /** 見出しに出すボードの名前。 */
  boardName: string;
  /** 作成先が決まった。失敗は投げてよい。選択部品がそのまま表示する。 */
  onSelected: (target: BoardTarget) => Promise<void>;
  /** 閉じた（やめた・Esc・選び終えた）。 */
  onClose: () => void;
};

/** 見出しの id。ダイアログの名前にする。 */
const HEADING_ID = "target-picker-heading";

/**
 * 見出しが無いときのダイアログの名前。**見出しの文言と同じにする。**
 *
 * 見出しは選択部品（`RepositoryPicker`）が描くので、選択部品が落ちて境界の
 * 代わりの表示になると、`aria-labelledby` の参照先ごと消える（#481 の full
 * review）。参照先があるうちは `aria-labelledby` が勝つので、ふだんの名前は
 * 変わらない。
 */
const DIALOG_NAME = "作成先を選ぶ";

/**
 * 作成先を選ぶダイアログ（ADR 0100）。
 *
 * **ボードの画面を外さない。** 以前は別の画面で、選ぶあいだキャンバスごと
 * 外していた。いまは描いて絵解いてから選ぶので、外すと絵解きの結果と手直しが
 * 消え、選んだあとに絵解き直す（課金される）ことになる。
 *
 * 中身は選択部品（`RepositoryPicker`）のまま。**開いているあいだだけ描く。**
 * 開くたびに候補を引き直し、前に開いたときの選びかけを持ち越さない。
 *
 * 器と閉じ方は、ほかのダイアログ（`UsageGuideDialog`）と同じ。ブラウザの
 * `<dialog>` をモーダルで開き、閉じたら開いた元へ焦点を戻す。
 */
export function TargetPickerDialog({ open, boardName, onSelected, onClose }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  // 作成先を設定しているあいだ。**Esc で閉じさせない**（`NewBoardDialog` と同じ）。
  // 閉じると、設定できなかったときに理由を出す先が無くなる。読むのは Esc を
  // 押した時点だけなので、state にせず ref で持つ。
  const saving = useRef(false);

  useEffect(() => {
    const element = dialog.current;
    if (element === null) return;
    if (open && !element.open) {
      opener.current =
        document.activeElement instanceof HTMLElement ? document.activeElement : null;
      element.showModal();
      // **焦点は見出しに置く**（`UsageGuideDialog` と同じ）。押せるものに置くと、
      // 候補が届く前は「やめる」しか無く、Enter 1 回で閉じてしまう。見出しは
      // `tabIndex={-1}` なので、ブラウザの既定（最初に焦点を受けられるもの）でも
      // 見出しに載る。明示するのは、既定の選び方に頼らないため。
      element.querySelector<HTMLElement>(`#${HEADING_ID}`)?.focus();
    } else if (!open && element.open) {
      element.close();
    }
  }, [open]);

  const select = useCallback(
    async (target: BoardTarget) => {
      saving.current = true;
      try {
        await onSelected(target);
      } finally {
        saving.current = false;
      }
    },
    [onSelected],
  );

  return (
    <dialog
      ref={dialog}
      className="modal-dialog target-picker-dialog"
      aria-labelledby={HEADING_ID}
      aria-label={DIALOG_NAME}
      onCancel={(e) => {
        if (saving.current) e.preventDefault();
      }}
      onClose={() => {
        onClose();
        // 開いた元へ戻す。帯の「作成先を選ぶ」は、選び終えると同じボタンのまま
        // 「GitHub に作成する」に替わる（`DetailBand`）ので、次に押すものに戻る。
        // メニューの項目はメニューごと閉じて消えているので、そのときはメニューの
        // 口へ戻す（`UsageGuideDialog` と同じ）。
        //
        // **戻せたかを確かめる。** 替わった「GitHub に作成する」が押せない（1 件も
        // 選ばれていない、など）と、無効なボタンは焦点を受けず、焦点が body に
        // 落ちる（#481 の full review）。そのときもメニューの口へ戻す。
        const back = opener.current;
        opener.current = null;
        if (back !== null && back !== document.body && back.isConnected) {
          back.focus();
          if (document.activeElement === back) return;
        }
        document
          .querySelector<HTMLElement>('.excalidraw [data-testid="main-menu-trigger"]')
          ?.focus();
      }}
    >
      {open && (
        // キャンバスの上に開くので、ほかのダイアログと同じく中身を 1 枚で包む
        // （ADR 0027）。包まないと、選択部品の例外が App の境界まで届き、保存して
        // いないキャンバスごと外れる（#481 の full review）。
        <ErrorBoundary name="作成先の選択" recovery="remount">
          <RepositoryPicker
            boardName={boardName}
            headingId={HEADING_ID}
            onSelected={select}
            onCancel={() => dialog.current?.close()}
          />
        </ErrorBoundary>
      )}
    </dialog>
  );
}
