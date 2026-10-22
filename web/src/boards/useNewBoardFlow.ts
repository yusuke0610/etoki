import { useCallback, useMemo, useRef, useState } from "react";

import { boardsApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import type { BoardDetail, BoardTarget } from "../api/types";
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
  /** 作成しようとしているボードの名前。null なら作成中ではない。 */
  creating: string | null;
  /** 作成先を選ばずに作っているあいだ（`createWithoutTarget`）。 */
  submitting: boolean;
  /**
   * 作成先を選ばずに作れなかった理由（`createWithoutTarget`）。ダイアログの中に
   * 出す。**通知には出さない。** `showModal()` で開いたダイアログは最上位の層に
   * 載り、外の通知はその下に隠れる。
   */
  failure: Failure | null;
  openDialog: () => void;
  setName: (name: string) => void;
  setTemplate: (template: TemplateChoice) => void;
  /**
   * 名前を確定して、作成先の選択に進む。ここではまだ作らない。進んだら true。
   *
   * `confirm` は未保存の確認（`App` の `confirmDiscard`）。作成先の選択画面に
   * 移ると、開いていたボードのキャンバスが外れる。**名前を見てから訊く。**
   * 進めない入力で確認だけ出しても、捨てた先に何も無い。
   */
  start: (confirm: () => boolean) => boolean;
  /** ダイアログをやめる。名前とひな形は既定に戻す。 */
  cancel: () => void;
  /** 作成先の選択をやめて、ダイアログを入力ごと開き直す。 */
  backToDialog: () => void;
  /** 作成先の選択から離れる（ボードを開いた・一覧へ戻った）。入力は残す。 */
  leave: () => void;
  /**
   * 作成先が決まったのでボードを作る。作成中でなければ null。
   *
   * **失敗は投げる。** 作成先の選択（`RepositoryPicker`）が受けて表示する。
   */
  create: (target: BoardTarget) => Promise<BoardDetail | null>;
  /**
   * 作成先を選ばずに、ダイアログからそのまま作る（ADR 0090）。**GitHub が
   * 未設定の構成でだけ使う。** 作成先の候補を引けないので、選択画面に進むと
   * 行き止まりになる。進めなかった（名前が空・確認で断られた・作っている
   * 最中）なら null。
   *
   * `confirm` の意味は `start` と同じ。**失敗は投げずに `failure` に置き、null を
   * 返す。** ダイアログは開いたまま入力を残し、理由をその中に出す。
   */
  createWithoutTarget: (confirm: () => boolean) => Promise<BoardDetail | null>;
};

/**
 * 新しいボードを作るまでの流れ（#230 で `App` から切り出し）。
 *
 * **開閉と入力はここで持つ。** 作成先の選択（別の画面）から戻ったときに、
 * 入力を残したまま開き直すため。選び直すために戻った人に、名前を打ち直させない。
 */
export function useNewBoardFlow(): NewBoardFlow {
  const [dialogOpen, setDialogOpen] = useState(false);
  const [name, setName] = useState("");
  // テンプレートは選ばせるもので、勝手に適用しない。
  const [template, setTemplate] = useState<TemplateChoice>(BLANK_TEMPLATE);
  // **作成先はボードを作る前に選ばせる**（ADR 0017）。書ける Project を持たない
  // 人はここで先へ進めず、それが「作成にはリポジトリへのアクセス権が要る」
  // ことの表れになる。GitHub が未設定の構成だけは選ばずに作る（ADR 0090）。
  const [creating, setCreating] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  // 作成先を選ばずに作る口の二重押しを、押した時点で弾く。state だと同じ tick に
  // 届いた 2 回目がまだ false を読み、同じ名前のボードが 2 枚できる。
  const submittingRef = useRef(false);

  const openDialog = useCallback(() => setDialogOpen(true), []);

  const start = useCallback(
    (confirm: () => boolean) => {
      if (!name.trim()) return false;
      if (!confirm()) return false;

      // 入力は残したまま閉じる。作成先の選択から戻ったら、同じ入力で開き直す。
      setDialogOpen(false);
      setCreating(name.trim());
      return true;
    },
    [name],
  );

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

  const backToDialog = useCallback(() => {
    setCreating(null);
    setDialogOpen(true);
  }, []);

  const leave = useCallback(() => setCreating(null), []);

  const create = useCallback(
    async (target: BoardTarget) => {
      if (creating === null) return null;

      // シーンを組み立てるのは押されたこの時点。選んだ時点で作ると、作成先を
      // 選ばずに引き返した回数だけ使わないシーンを持つことになる。
      const board = await boardsApi.create(creating, target, templateScene(template));
      setName("");
      setTemplate(BLANK_TEMPLATE);
      setCreating(null);
      return board;
    },
    [creating, template],
  );

  const createWithoutTarget = useCallback(
    async (confirm: () => boolean) => {
      if (submittingRef.current) return null;
      // 進めない入力で確認だけ出さない（`start` と同じ順）。
      if (!name.trim()) return null;
      if (!confirm()) return null;

      submittingRef.current = true;
      setSubmitting(true);
      // 押し直したら前の理由は消す。残すと、作り直している最中にも前の失敗が出る。
      setFailure(null);
      try {
        const board = await boardsApi.create(name.trim(), null, templateScene(template));
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
      creating,
      submitting,
      failure,
      openDialog,
      setName,
      setTemplate,
      start,
      cancel,
      backToDialog,
      leave,
      create,
      createWithoutTarget,
    }),
    [
      dialogOpen,
      name,
      template,
      creating,
      submitting,
      failure,
      openDialog,
      start,
      cancel,
      backToDialog,
      leave,
      create,
      createWithoutTarget,
    ],
  );
}
