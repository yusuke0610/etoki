import { useCallback } from "react";

import type { DeletionState } from "./useBoardManagement";

type Props = {
  /** 消すボードの名前。 */
  name: string;
  /** 失われるものを引き終えた確認（ADR 0042）。引いている最中は描かない。 */
  deletion: Exclude<DeletionState, { status: "loading" }>;
  onConfirm: () => void;
  onCancel: () => void;
};

/**
 * ボードの削除の確認（ADR 0042）。#230 で `BoardPage` から切り出し。
 *
 * **何が残るのかを見せてから確認させる**（中核思想 3）。etoki は GitHub 側の
 * draft issue を消さない（消せない）ので、消えるのは出どころの記録のほうだと
 * 分けて言う。ブラウザの confirm を使わないのは、件数を出す場所が無いため。
 */
export function DeleteConfirm({ name, deletion, onConfirm, onCancel }: Props) {
  /**
   * 確認が出たら、そこへフォーカスを移す。
   *
   * **移さないとキーボードの居場所が消える。** 押した「ボードを削除」は確認が
   * 開くと disabled になり、focus を body へ落とす。取り消せない操作の直前で
   * 行き先を失わせない。
   */
  const focusOnOpen = useCallback((node: HTMLElement | null) => {
    node?.focus();
  }, []);

  return (
    <section
      className="delete-confirm"
      role="alertdialog"
      aria-labelledby="delete-confirm-title"
      // 見出しではなく枠を受け皿にする。読み上げは aria-labelledby で
      // 見出しを読み、次のタブ移動が中のボタンに入る。
      tabIndex={-1}
      ref={focusOnOpen}
    >
      <h2 id="delete-confirm-title">「{name}」を削除しますか</h2>
      <p>
        {"シーンもメンバーも実行の記録も消えます。"}
        <strong>取り消せません。</strong>
      </p>
      {deletion.losing.recordedItemCount > 0 ? (
        <p>
          {`このボードから作成した draft issue が ${deletion.losing.recordedItemCount} 件記録されています。`}
          {"GitHub 側の draft issue は削除されません（etoki からは消せません）。"}
          {"削除すると、その draft issue がどこから作られたのかを辿れなくなります。"}
        </p>
      ) : (
        <p>このボードから作成した draft issue の記録はありません。</p>
      )}
      <div className="delete-confirm-actions">
        <button
          type="button"
          className="danger"
          onClick={onConfirm}
          disabled={deletion.status === "deleting"}
        >
          {deletion.status === "deleting" ? "削除中…" : "削除する"}
        </button>
        <button
          type="button"
          className="quiet"
          onClick={onCancel}
          disabled={deletion.status === "deleting"}
        >
          やめる
        </button>
      </div>
    </section>
  );
}
