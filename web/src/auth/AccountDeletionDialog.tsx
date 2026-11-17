import { useCallback, useEffect, useId, useRef, useState } from "react";

import { accountApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import type { AccountDeletion, AccountDeletionBoard } from "../api/types";
import { ErrorNotice } from "../app/ErrorNotice";

type Props = {
  /** 開いているか。 */
  open: boolean;
  /** 閉じた（消さずに）。焦点を戻すのは呼ぶ側。 */
  onClose: () => void;
  /** 消し終えた。サーバーはセッションも消しているので、呼ぶ側はログイン状態を読み直す。 */
  onDeleted: () => void;
};

/** 1 枚ぶんの、ボードと一緒に失われるもの。何も無ければ空。 */
function boardNotes(board: AccountDeletionBoard): string[] {
  const notes: string[] = [];
  if (board.otherMemberCount > 0) {
    notes.push(
      `ほかに ${board.otherMemberCount} 人のメンバーがいます。その人たちからも消えます。`,
    );
  }
  if (board.recordedItemCount > 0) {
    notes.push(`作成した draft issue の記録 ${board.recordedItemCount} 件`);
  }
  // 届いたか分からない書き込みは記録の件数に混ぜない（ADR 0042 の追記、0056）。
  if (board.unconfirmedWriteCount > 0) {
    notes.push(`GitHub に届いたか分からない書き込み ${board.unconfirmedWriteCount} 件`);
  }
  return notes;
}

/**
 * 自分を etoki から消す確認（ADR 0101）。
 *
 * **何が消えて何が残るのかを見せてから確認させる**（ボードの削除と同じ形、
 * ADR 0042、中核思想 3）。共有していても、自分だけがオーナーのボードは消えるので、
 * そこにいるほかのメンバーの数を並べて出す。誰に引き継ぐかを etoki は決めない。
 *
 * **開くたびに引き直す。** 消えるものは、ほかのメンバーの操作（オーナーにする、
 * 外れる）で変わる。開くまでは引かない（押したときだけ引く、
 * `.claude/rules/async-ui.md`）。**引けないうちは「削除する」を出さない。**
 * 何が消えるかを見せずに消させない。
 */
export function AccountDeletionDialog({ open, onClose, onDeleted }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [losing, setLosing] = useState<AccountDeletion | null>(null);
  const [error, setError] = useState<Failure | null>(null);
  // 二重に押させない守りは ref に置く（同じ tick の 2 回目が state の古い値を読む）。
  const deletingRef = useRef(false);
  // 画面に出すための写し。ref は再描画しない。
  const [deleting, setDeleting] = useState(false);
  const deletingReasonId = useId();
  // 読み込みの世代。開き直したら前の応答は捨てる。
  const loading = useRef(0);

  const load = useCallback(async () => {
    const generation = ++loading.current;
    setLosing(null);
    setError(null);
    try {
      const got = await accountApi.deletion();
      if (generation === loading.current) setLosing(got);
    } catch (e) {
      if (generation === loading.current) {
        setError(describeFailure("消えるものを確かめられませんでした", e));
      }
    }
  }, []);

  useEffect(() => {
    const element = dialog.current;
    if (element === null) return;
    if (open && !element.open) {
      element.showModal();
      void load();
    } else if (!open && element.open) {
      element.close();
    }
  }, [load, open]);

  const remove = useCallback(async () => {
    if (deletingRef.current) return;
    deletingRef.current = true;
    setDeleting(true);
    setError(null);
    try {
      await accountApi.delete();
    } catch (e) {
      // 途中まで消えていても、もう一度押せば続きから消える（ADR 0101）。
      setError(describeFailure("アカウントを削除できませんでした", e));
      deletingRef.current = false;
      setDeleting(false);
      return;
    }
    // **消し終えたら押せる形に戻さない。** ログイン状態を読み直して画面が
    // 替わるまでのあいだに、消えた利用者としてもう一度押させない。
    onDeleted();
  }, [onDeleted]);

  return (
    <dialog
      ref={dialog}
      className="modal-dialog account-deletion-dialog"
      aria-labelledby="account-deletion-heading"
      onCancel={(e) => {
        // 消している途中は閉じさせない。閉じると、消えたのかどうかを読む場所が無くなる。
        if (deletingRef.current) e.preventDefault();
      }}
      onClose={() => {
        // 閉じたら走っている読み込みの応答を捨てる（`.claude/rules/async-ui.md`）。
        loading.current++;
        onClose();
      }}
    >
      <h2 id="account-deletion-heading">アカウントを削除しますか</h2>
      <p>
        {"etoki からあなたの記録を消します。"}
        <strong>取り消せません。</strong>
      </p>

      {/* 失敗は確認の中に出す。操作した場所から離すと、どの操作の結果か読めない（ADR 0058）。 */}
      {error && <ErrorNotice failure={error} />}

      {losing === null && error === null && (
        <p className="hint">消えるものを確かめています…</p>
      )}
      {losing !== null && (
        <>
          {losing.boards.length === 0 ? (
            <p>一緒に消えるボードはありません。</p>
          ) : (
            <section aria-labelledby="account-deletion-boards">
              <h3 id="account-deletion-boards">
                {`一緒に消えるボード（${losing.boards.length} 枚）`}
              </h3>
              <p className="hint">
                あなただけがオーナーのボードです。残したいボードは、先に参加者をオーナーにしてください。
              </p>
              <ul className="account-deletion-boards">
                {losing.boards.map((board) => (
                  <li key={board.id}>
                    <p className="account-deletion-board-name">{board.name}</p>
                    {boardNotes(board).map((note) => (
                      <p key={note} className="hint">
                        {note}
                      </p>
                    ))}
                  </li>
                ))}
              </ul>
            </section>
          )}
          {losing.leftBoardCount > 0 && (
            <p>
              {`ほかのボード ${losing.leftBoardCount} 枚からはメンバーとして外れます。ボードは残ります。`}
            </p>
          )}
          <p>ログイン・保存した GitHub のトークン・MCP の接続も消えます。</p>
          <p className="hint">
            {"GitHub に作った draft issue は消えません（etoki からは消せません）。"}
            {
              "GitHub 側のアプリの認可も残ります。GitHub の Settings → Applications → Authorized GitHub Apps から取り消せます。"
            }
          </p>
        </>
      )}

      {/*
        消しているあいだは押せない理由を本文で出す（ADR 0039）。待てば終わることは、
        待てると知っている人にしか分からない。
      */}
      {deleting && (
        <p className="hint" id={deletingReasonId}>
          削除しています。途中でやめることはできません。
        </p>
      )}

      <div className="dialog-actions">
        {losing !== null && (
          <button
            type="button"
            className="danger"
            disabled={deleting}
            aria-describedby={deleting ? deletingReasonId : undefined}
            onClick={() => void remove()}
          >
            {deleting ? "削除しています…" : "削除する"}
          </button>
        )}
        <button
          type="button"
          className="quiet"
          disabled={deleting}
          aria-describedby={deleting ? deletingReasonId : undefined}
          onClick={() => dialog.current?.close()}
        >
          やめる
        </button>
      </div>
    </dialog>
  );
}
