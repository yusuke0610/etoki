import { useEffect, useRef } from "react";

import { ErrorBoundary } from "../../app/ErrorBoundary";
import { ErrorNotice } from "../../app/ErrorNotice";
import type { PremiseEditor } from "./usePremise";

type Props = {
  /** 書きかけと保存（`usePremise`）。開閉もここが持つ。 */
  editor: PremiseEditor;
  /** 書き換えられるか。読むだけの人には中身と理由だけを出す（ADR 0093）。 */
  canEdit: boolean;
  /** いまの背景。読むだけの人に見せる。 */
  premise: string;
};

/**
 * ボードの背景（ADR 0093）のダイアログ。開くのはキャンバスのメニューの
 * 「ボードの背景…」（`BoardMenu`）。
 *
 * **キャンバスにも右のパネルにも置かない。** 背景はブレストの外に置くもので
 * （中核思想 1）、たまに書き換えるボードそのものの設定。開く場所は共有と同じ
 * （ADR 0078）。
 *
 * **ブラウザの `<dialog>` をモーダルで開く**（`ShareDialog` と同じ）。共有と
 * 違って**書きかけはダイアログの外（`usePremise`）で持つ**ので、閉じても消えない。
 *
 * **空でも催促しない**（中核思想 1）。書くかどうかは人が決め、etoki は既定の文も
 * 例文も入れない。入れた時点で、読み方を決めているのが etoki になる。
 */
export function PremiseDialog({ editor, canEdit, premise }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const { open, closeEditor, saving } = editor;
  useEffect(() => {
    const element = dialog.current;
    if (element === null) return;
    if (open && !element.open) element.showModal();
    else if (!open && element.open) element.close();
  }, [open]);

  return (
    <dialog
      ref={dialog}
      className="modal-dialog premise-dialog"
      aria-labelledby="premise-dialog-heading"
      // Esc はブラウザが閉じ、「閉じる」も閉じるだけなので、閉じたときの処理は
      // ここ 1 か所に集まる。
      onClose={() => {
        closeEditor();
        // **焦点をメニューの口へ戻す**（`ShareDialog` と同じ）。開いた項目は
        // メニューごと消えているので、戻さないと焦点は body に落ちる。
        document
          .querySelector<HTMLElement>('.excalidraw [data-testid="main-menu-trigger"]')
          ?.focus();
      }}
      // 保存しているあいだは Esc で閉じさせない。閉じると、断られたときの理由が
      // 開き直すまで見えない。
      onCancel={(e) => {
        if (saving) e.preventDefault();
      }}
    >
      <h2 id="premise-dialog-heading">ボードの背景</h2>
      <p className="hint" id="premise-dialog-about">
        絵解きのたびに、囲んだ絵と一緒に LLM
        へ渡す文です。どのリポジトリの話か、チームでの呼び方や前提などを書いておけます。
      </p>
      {/*
        **変えても「作成済み」は動かないことを、書き換える場所で言う**（ADR 0093）。
        背景は注釈のハッシュに入らない。黙っていると、作成済みのままなのを
        「読み直す必要が無い」印と読む。
      */}
      <p className="hint">
        書き換えても、作成済みの絵は「作成済み」のままです。新しい背景で読み直すかどうかは、絵ごとに選べます。
      </p>
      {/* 落ちてもキャンバスごと外れないよう、ダイアログの中で包む（ADR 0027）。 */}
      <ErrorBoundary name="ボードの背景" recovery="remount">
        {canEdit ? (
          <PremiseForm editor={editor} onClose={() => dialog.current?.close()} />
        ) : (
          <>
            <PremiseText premise={premise} />
            {/*
              読むのは viewer にも許す（ADR 0093）。書き換えの口は出さず、なぜ無い
              のかを言う（`.claude/rules/async-ui.md` の「理由を出す約束は、消した
              ボタンにも掛かる」）。
            */}
            <p className="hint">
              ボードの背景を書き換えられるのは、編集できる人だけです。
            </p>
            <div className="dialog-actions">
              <button
                type="button"
                className="quiet"
                onClick={() => dialog.current?.close()}
              >
                閉じる
              </button>
            </div>
          </>
        )}
      </ErrorBoundary>
    </dialog>
  );
}

function PremiseForm({
  editor,
  onClose,
}: {
  editor: PremiseEditor;
  onClose: () => void;
}) {
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void editor.save();
      }}
    >
      {/*
        名前は見出し（見えている文字）から取る（WCAG 2.5.3）。説明は上の文。
      */}
      <textarea
        className="premise-input"
        aria-labelledby="premise-dialog-heading"
        aria-describedby="premise-dialog-about"
        rows={12}
        value={editor.text}
        onChange={(e) => editor.change(e.target.value)}
      />
      {editor.edited && (
        <p className="hint">
          保存していない書きかけです。閉じても、このボードを開いているあいだは残ります。
        </p>
      )}
      {editor.failure && <ErrorNotice failure={editor.failure} />}
      {editor.latest !== null && (
        <section className="premise-latest" aria-labelledby="premise-latest-heading">
          <h3 id="premise-latest-heading">いまの背景</h3>
          <PremiseText premise={editor.latest} />
          <p className="hint">
            「保存」を押すと、この背景を上の書きかけで置き換えます。書きかけを捨てると、この背景に戻ります。
          </p>
        </section>
      )}
      <div className="dialog-actions">
        {/* やめる・捨てるは控えめな操作（#203）。主となる操作は「保存」の 1 つ。 */}
        {editor.edited && (
          <button
            type="button"
            className="quiet"
            disabled={editor.saving}
            onClick={editor.discard}
          >
            書きかけを捨てる
          </button>
        )}
        <button
          type="button"
          className="quiet"
          disabled={editor.saving}
          onClick={onClose}
        >
          閉じる
        </button>
        <button type="submit" className="primary" disabled={editor.saving}>
          {editor.saving ? "保存中…" : "保存"}
        </button>
      </div>
    </form>
  );
}

/** 背景の本文。書いた人の改行と空行をそのまま見せる。 */
function PremiseText({ premise }: { premise: string }) {
  if (premise === "") return <p className="hint">まだ書かれていません。</p>;
  return <p className="premise-text">{premise}</p>;
}
