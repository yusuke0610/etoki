import { useEffect, useRef } from "react";

import type { Failure } from "../api/errorMessage";
import { ErrorNotice } from "../app/ErrorNotice";
import type { TemplateChoice } from "../excalidraw/template";
import { TemplatePicker } from "./TemplatePicker";

type Props = {
  /** 開いているか。開閉は App が持つ（作成先の選択から戻ったときに開き直すため）。 */
  open: boolean;
  name: string;
  onNameChange: (name: string) => void;
  template: TemplateChoice;
  onTemplateChange: (template: TemplateChoice) => void;
  /**
   * 作成先の選択へ進むか。false なら選ばずにここから作る（GitHub が未設定の
   * 構成、ADR 0090）。
   */
  choosesTarget: boolean;
  /** 作成先を選ばずに作っているあいだ。`choosesTarget` が false のときだけ立つ。 */
  submitting: boolean;
  /**
   * 作成先を選ばずに作れなかった理由。**このダイアログの中に出す。** モーダルで
   * 開いているあいだ、外の通知はダイアログの下に隠れて読めない。
   */
  failure: Failure | null;
  /**
   * 名前を確定して、作成先の選択へ進む。ここではまだ作らない。作成先を選ばない
   * 構成では、ここで作る。
   */
  onNext: () => void;
  /** やめた（閉じた）。入力を既定に戻すのは呼ぶ側。 */
  onCancel: () => void;
  /** 閉じ終わった。焦点を戻すのは呼ぶ側（閉じるまで外は触れないため）。 */
  onClosed: () => void;
};

/**
 * 新しいボードのダイアログ（#200）。名前とひな形を決めて、作成先の選択へ進む。
 * GitHub が未設定の構成では、作成先を選ばずにここから作る（ADR 0090）。
 *
 * **別の画面にしない。** 入力は 2 つだけで、URL を持つ画面を増やすほどではない。
 * 作成中は URL に載せない（`useBoardNavigation` の `startCreating`）のと同じ
 * 理由で、読み込み直した先で復元できるものが無い。
 *
 * **ブラウザの `<dialog>` をモーダルで開く。** 開いているあいだ外は触れなくなり、
 * 焦点も中に閉じ込められる。自前で同じことをすると、焦点の閉じ込めと外の不活性化を
 * 両方持つことになる。
 */
export function NewBoardDialog({
  open,
  name,
  onNameChange,
  template,
  onTemplateChange,
  choosesTarget,
  submitting,
  failure,
  onNext,
  onCancel,
  onClosed,
}: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const element = dialog.current;
    if (element === null) return;
    if (open && !element.open) {
      element.showModal();
      // 開いたら名前の欄から始める。開き直したとき（作成先の選択から戻った
      // とき）も同じで、打ち直すのではなく直すところから始められる。
      input.current?.focus();
    } else if (!open && element.open) {
      element.close();
    }
  }, [open]);

  return (
    <dialog
      ref={dialog}
      className="modal-dialog new-board-dialog"
      aria-labelledby="new-board-heading"
      // **閉じたらやめたことにする。** Esc はブラウザが閉じ、「キャンセル」も
      // 閉じるだけなので、やめる処理はここ 1 か所に集まる。「次へ」は閉じずに
      // 一覧の画面ごと外れる（開いたまま外れた `<dialog>` は close を送らない）
      // ので、ここを通らず、作成先の選択から戻ったときに入力が残る。
      onClose={() => {
        onCancel();
        onClosed();
      }}
      // 作っているあいだは Esc で閉じさせない。閉じると入力が既定に戻り、作れ
      // なかったときに直す元が残らない。
      onCancel={(e) => {
        if (submitting) e.preventDefault();
      }}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim() !== "" && !submitting) onNext();
        }}
      >
        <h2 id="new-board-heading">新しいボード</h2>
        <label className="new-board-name">
          ボード名
          {/*
            必須であることは支援技術にも伝える。「次へ」が押せないだけでは、読み上げで
            欄を移っている人には届かない。空白だけの名前は `required` を通るので、
            進ませない判定は「次へ」の側に残す。
          */}
          <input
            ref={input}
            required
            value={name}
            onChange={(e) => onNameChange(e.target.value)}
          />
        </label>
        {/*
          何から始めるかをここで選ばせる（#52）。**画面を 1 枚増やさない。**
          増やすと、空白で始めたい人にも通り抜けるだけの手順が要る。
        */}
        <TemplatePicker value={template} onChange={onTemplateChange} />
        {/*
          作成先を選ばずに作ることを、押す前に言う（中核思想 3）。いつもの流れと
          違って選択画面が挟まらないので、黙っていると作成先がどうなったのかが
          開いた先まで分からない。
        */}
        {!choosesTarget && (
          <p className="hint">
            GitHub
            が未設定なので、作成先は選ばずに作ります。設定したあとで開くと選べます。
          </p>
        )}
        {failure && <ErrorNotice failure={failure} />}
        <div className="dialog-actions">
          {/* やめるのは控えめな操作（#203）。主となる操作は「次へ」の 1 つ。 */}
          <button
            type="button"
            className="quiet"
            disabled={submitting}
            onClick={() => dialog.current?.close()}
          >
            キャンセル
          </button>
          {/*
            空白だけの名前では進ませない。誤って空名のボードが増えるのを防ぐ。
            作成先を選ばない構成では、押すとここで作るので「作成」と名乗る。
          */}
          <button
            type="submit"
            className="primary"
            disabled={name.trim() === "" || submitting}
          >
            {choosesTarget ? "次へ" : submitting ? "作成中…" : "作成"}
          </button>
        </div>
      </form>
    </dialog>
  );
}
