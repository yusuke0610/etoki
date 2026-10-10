import { useEffect, useRef } from "react";

import type { BoardRole } from "../../api/types";
import { ErrorBoundary } from "../../app/ErrorBoundary";
import { MemberPanel } from "./MemberPanel";

type Props = {
  /** 開いているか。開くのはキャンバスのメニューの「共有…」（`BoardMenu`）。 */
  open: boolean;
  onClose: () => void;
  boardId: string;
  /** 見ている人のロール。owner だけが招待と解除を触れる。 */
  role: BoardRole;
  /** 共有が組み立てられていない構成なら理由。使えるなら null（ADR 0030）。 */
  unavailable: string | null;
};

/**
 * ボードの共有（メンバーの一覧と招待）のダイアログ（#248、ADR 0078）。
 *
 * **右のパネルには置かない。** パネルに並ぶ「絵解いた」と「etoki AI」はボードの
 * 中身に対する作業で、共有はボードそのものの管理。開く頻度も違うので、たまに
 * 押す操作の置き場所（キャンバスのメニュー、ADR 0065）から開く。
 *
 * **ブラウザの `<dialog>` をモーダルで開く**（`NewBoardDialog` と同じ）。キャンバスの
 * 形を変えずに、開く場所を 1 つに保てる。パネルのタブと違って、閉じたら中身は
 * 外す。**開くたびに一覧を引き直す**（押したときだけ引く、
 * `.claude/rules/async-ui.md`）。招待の書きかけは閉じると消えるが、共有は
 * 手早く済ませる操作で、開き直して打ち直せば足りる。
 */
export function ShareDialog({ open, onClose, boardId, role, unavailable }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = dialog.current;
    if (element === null) return;
    if (open && !element.open) element.showModal();
    else if (!open && element.open) element.close();
  }, [open]);

  return (
    <dialog
      ref={dialog}
      className="share-dialog"
      aria-labelledby="share-dialog-heading"
      // Esc はブラウザが閉じ、「閉じる」も閉じるだけなので、閉じたときの処理は
      // ここ 1 か所に集まる。
      onClose={() => {
        onClose();
        // **焦点をメニューの口へ戻す。** 開いた項目はメニューごと消えているので、
        // 戻さないと焦点は body に落ち、キーボードの利用者は居場所を失う。
        document
          .querySelector<HTMLElement>('.excalidraw [data-testid="main-menu-trigger"]')
          ?.focus();
      }}
    >
      <h2 id="share-dialog-heading">共有</h2>
      {open &&
        (unavailable !== null ? (
          // 押しても 503 しか返らない構成。口は黙って消さず、開いた先で理由を
          // 出す（中核思想 3、ADR 0030）。
          <p className="hint">{unavailable}</p>
        ) : (
          // 落ちてもキャンバスごと外れないよう、ダイアログの中で包む（ADR 0027）。
          <ErrorBoundary name="共有" recovery="remount">
            <MemberPanel boardId={boardId} role={role} />
          </ErrorBoundary>
        ))}
      <div className="dialog-actions">
        <button type="button" className="quiet" onClick={() => dialog.current?.close()}>
          閉じる
        </button>
      </div>
    </dialog>
  );
}
