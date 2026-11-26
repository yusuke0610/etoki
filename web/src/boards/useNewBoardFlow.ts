import { useCallback, useMemo, useRef, useState } from "react";

import { boardsApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import type { BoardDetail } from "../api/types";
import {
  BLANK_TEMPLATE,
  templateScene,
  type TemplateChoice,
} from "../excalidraw/template";

export type NewBoardFlow = {
  /** 新しいボードのダイアログを開いているか（#200）。 */
  dialogOpen: boolean;
  name: string;
  /** 何から始めるか。**既定は空白**（中核思想 3）。 */
  template: TemplateChoice;
  /** 作っているあいだ（`create`）。 */
  submitting: boolean;
  /**
   * 作れなかった理由。ダイアログの中に出す。**通知には出さない。**
   * `showModal()` で開いたダイアログは最上位の層に載り、外の通知はその下に隠れる。
   */
  failure: Failure | null;
  openDialog: () => void;
  setName: (name: string) => void;
  setTemplate: (template: TemplateChoice) => void;
  /** ダイアログをやめる。名前とひな形は既定に戻す。 */
  cancel: () => void;
  /**
   * 名前とひな形でボードを作る。**作成先は選ばない**（ADR 0100）。作成先は、
   * 描いたあとでボードの上から選ぶ。進めなかった（名前が空・確認で断られた・
   * 作っている最中）なら null。
   *
   * `confirm` は未保存の確認（`useBoardNavigation` の `confirmDiscard`）。作った
   * ボードを開くと、開いていたボードのキャンバスが外れる。**名前を見てから
   * 訊く。** 進めない入力で確認だけ出しても、捨てた先に何も無い。
   *
   * **`confirm` を呼ぶのは送る直前だけ。** 名前が空のときと作っている最中は
   * 呼ばずに null を返し、承諾されたら必ず送る。`useBoardNavigation` はその中で
   * 送る前の世代を控えるので、弾く判定を `confirm` より後ろへ動かすと、弾いた
   * 呼び出しが作っている最中の作成を古いものにする（#481 の full review）。
   *
   * **失敗は投げずに `failure` に置き、null を返す。** ダイアログは開いたまま
   * 入力を残し、理由をその中に出す。
   */
  create: (confirm: () => boolean) => Promise<BoardDetail | null>;
};

/**
 * 新しいボードを作るまでの流れ（#230 で `App` から切り出し）。
 *
 * **開閉と入力はここで持つ。** 作れなかったときに、入力を残したまま理由を
 * 出すため。
 */
export function useNewBoardFlow(): NewBoardFlow {
  const [dialogOpen, setDialogOpen] = useState(false);
  const [name, setName] = useState("");
  // テンプレートは選ばせるもので、勝手に適用しない。
  const [template, setTemplate] = useState<TemplateChoice>(BLANK_TEMPLATE);
  const [submitting, setSubmitting] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  // 二重押しを、押した時点で弾く。state だと同じ tick に届いた 2 回目がまだ
  // false を読み、同じ名前のボードが 2 枚できる。
  const submittingRef = useRef(false);

  const openDialog = useCallback(() => setDialogOpen(true), []);

  /**
   * 次に開いたとき、やめたはずの入力が残っていると、別のボードのつもりで
   * 同じ名前を作りうる（#200）。
   */
  const cancel = useCallback(() => {
    setDialogOpen(false);
    setName("");
    setTemplate(BLANK_TEMPLATE);
    // やめた入力で作れなかった理由を、次に開いたときに出さない。
    setFailure(null);
  }, []);

  const create = useCallback(
    async (confirm: () => boolean) => {
      if (submittingRef.current) return null;
      // 進めない入力で確認だけ出さない。
      if (!name.trim()) return null;
      if (!confirm()) return null;

      submittingRef.current = true;
      setSubmitting(true);
      // 押し直したら前の理由は消す。残すと、作り直している最中にも前の失敗が出る。
      setFailure(null);
      try {
        // シーンを組み立てるのは押されたこの時点。
        const board = await boardsApi.create(name.trim(), templateScene(template));
        setDialogOpen(false);
        setName("");
        setTemplate(BLANK_TEMPLATE);
        return board;
      } catch (e) {
        setFailure(describeFailure("ボードを作成できませんでした", e));
        return null;
      } finally {
        submittingRef.current = false;
        setSubmitting(false);
      }
    },
    [name, template],
  );

  return useMemo(
    () => ({
      dialogOpen,
      name,
      template,
      submitting,
      failure,
      openDialog,
      setName,
      setTemplate,
      cancel,
      create,
    }),
    [dialogOpen, name, template, submitting, failure, openDialog, cancel, create],
  );
}
