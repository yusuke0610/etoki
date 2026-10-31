import { useEffect, useRef } from "react";

import { ErrorBoundary } from "../app/ErrorBoundary";
import { GRANULARITY_LABEL, STATE_LABEL } from "./annotations/annotationLabel";
import { diagramKinds, DIAGRAM_KIND_LABELS } from "./diagram/diagramLabels";

type Props = {
  /** 開いているか。開くのはキャンバスのメニューと、右のパネルの案内（`BoardPage`）。 */
  open: boolean;
  onClose: () => void;
};

/** 見出しの id。ダイアログの名前にする。 */
const HEADING_ID = "usage-guide-heading";

/**
 * etoki の使い方のダイアログ（ADR 0094）。
 *
 * **勝手には開かない**（中核思想 3）。開くのはキャンバスのメニューの
 * 「etoki の使い方」と、右のパネルの「選択中のフレーム」の案内だけ。
 *
 * **心得を手順より先に置く。** 手順だけを読むと「描く前に囲む枠を決める」
 * 使い方に寄りやすい。ブレスト中は構造を意識しない（中核思想 1）ことを
 * 最初に言う。
 *
 * 画面の文言は ADR 0086 の語彙（「絵」「絵解き」「フレーム」）で書く。状態・
 * 粒度・種別の見出しは表（`STATE_LABEL` など）から引く。書き写すと、表を
 * 直した日にここだけが古くなる。
 *
 * 器と閉じ方は `ShareDialog` と同じ（ブラウザの `<dialog>` をモーダルで開く）。
 */
export function UsageGuideDialog({ open, onClose }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  // 閉じたときに焦点を戻す先。開いた時点で焦点を持っていたもの。
  const opener = useRef<HTMLElement | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    const element = dialog.current;
    if (element === null) return;
    if (open && !element.open) {
      opener.current =
        document.activeElement instanceof HTMLElement ? document.activeElement : null;
      element.showModal();
      // **焦点は見出しに置き、先頭から読ませる。** ブラウザの既定は最初の押せる
      // もの（いちばん下の「閉じる」）で、そこまでスクロールして心得が画面の外に
      // 出る。心得を手順より先に読ませる並びが、開いた瞬間に崩れる。
      element.scrollTop = 0;
      heading.current?.focus();
    } else if (!open && element.open) {
      element.close();
    }
  }, [open]);

  return (
    <dialog
      ref={dialog}
      className="modal-dialog usage-guide"
      aria-labelledby={HEADING_ID}
      // Esc も「閉じる」もここに集まる（`ShareDialog` と同じ）。
      onClose={() => {
        onClose();
        // **開いた口へ焦点を戻す。** パネルの案内から開いたならそのボタンへ。
        // メニューから開いたときは項目がメニューごと消えている（焦点は body に
        // ある）ので、メニューの口へ戻す。
        const back = opener.current;
        opener.current = null;
        if (back !== null && back !== document.body && back.isConnected) {
          back.focus();
          return;
        }
        document
          .querySelector<HTMLElement>('.excalidraw [data-testid="main-menu-trigger"]')
          ?.focus();
      }}
    >
      {/* 開いたときに焦点を受ける。押せるものではないので Tab の順には入れない。 */}
      <h2 id={HEADING_ID} ref={heading} tabIndex={-1}>
        etoki の使い方
      </h2>
      {open && (
        // 中身は固定の文だが、パネルと同じく 1 枚ずつ包む（ADR 0027）。
        <ErrorBoundary name="使い方" recovery="remount">
          <UsageGuideBody />
        </ErrorBoundary>
      )}
      <div className="dialog-actions">
        <button type="button" className="quiet" onClick={() => dialog.current?.close()}>
          閉じる
        </button>
      </div>
    </dialog>
  );
}

/** 使い方の本文。ダイアログと分けておくのは、単体テストで中身だけを見るため。 */
export function UsageGuideBody() {
  return (
    <div className="usage-guide-body">
      <section>
        <h3>はじめに</h3>
        <ul>
          <li>
            <strong>
              描いているあいだは、issue や epic のことは考えなくてかまいません。
            </strong>
            付箋・矢印・手書きの文字で、思いつくままに描いてください。
          </li>
          <li>
            etoki が読むのは、<strong>フレームで囲んで「絵」にした範囲だけ</strong>
            です。囲むのは描き終えてからで十分です。
          </li>
          <li>
            絵は AI が読んで、draft issue の下書きにします。etoki
            が並びや矢印から構造を決めることはありません。
          </li>
          <li>
            保存も作成も、自動では行いません。どれもあなたが押したときだけ動きます。
          </li>
        </ul>
      </section>

      <section>
        <h3>流れ</h3>
        <ol className="usage-guide-steps">
          <li>
            <strong>描く。</strong>キャンバスに自由に描きます。
          </li>
          <li>
            <strong>フレームで囲む。</strong>ツールバーのフレームツール（
            <kbd>F</kbd>
            ）で、読ませたい範囲を囲みます。囲んだ中の要素がそのフレームに入ります。
          </li>
          <li>
            <strong>絵にする。</strong>
            囲んだフレームを選び、右のパネル「絵解いた」の「選択中のフレーム」で
            「〈名前〉を絵にする」を押します。外すときも同じ場所からです。
          </li>
          <li>
            <strong>必要なら、名前・粒度・種別を選ぶ。</strong>
            一覧の絵を押すと詳細が開きます。どれも選ばなくてかまいません。
          </li>
          <li>
            <strong>絵解く。</strong>
            右上の「絵解き」か、詳細の「絵解く」を押します。未保存なら先に保存してから読みます。AI
            を使うので利用料がかかります。
          </li>
          <li>
            <strong>確かめて作る。</strong>
            下書きを確かめ、作るものを選んだり直したりしてから「GitHub
            に作成する」を押します。作った draft issue は etoki からは消せません。
          </li>
        </ol>
      </section>

      <section>
        <h3>フレームのコツ</h3>
        <ul>
          <li>
            ブレストのためにフレームを使ってもかまいません。読まれるのは「絵」にしたフレームだけです。
          </li>
          <li>
            フレームの中にフレームを入れないでください。内側のフレームの文字は、外側の絵の文字として読まれません。
          </li>
          <li>
            フレームの名前は AI
            も読みます。何の絵かを一言で書いておくと伝わりやすくなります。
          </li>
        </ul>
      </section>

      <section>
        <h3>粒度と種別</h3>
        <dl className="usage-guide-terms">
          <dt>粒度</dt>
          <dd>
            作るものの大きさの目安です（
            {Object.values(GRANULARITY_LABEL).join(" / ")}）。
          </dd>
          <dt>種別</dt>
          <dd>
            何の図として読ませるかです（
            {diagramKinds()
              .map((k) => DIAGRAM_KIND_LABELS[k])
              .join(" / ")}
            ）。ひな形を選んで作ったボードでも、種別は自動では付きません。
          </dd>
        </dl>
      </section>

      <section>
        <h3>絵の状態</h3>
        <dl className="usage-guide-terms">
          <dt>{STATE_LABEL.uncreated}</dt>
          <dd>まだ GitHub に何も作っていない絵です。</dd>
          <dt>{STATE_LABEL.created}</dt>
          <dd>作ったときから中身が変わっていない絵です。</dd>
          <dt>{STATE_LABEL.changed}</dt>
          <dd>
            作ったあとに中身・粒度・種別が変わった絵です。作り直すかどうかはあなたが決めます。
          </dd>
        </dl>
      </section>
    </div>
  );
}
