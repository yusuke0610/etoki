import { useCallback, useMemo, useState } from "react";

import { boardsApi } from "../api/boards";
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
  // ことの表れになる。
  const [creating, setCreating] = useState<string | null>(null);

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

  return useMemo(
    () => ({
      dialogOpen,
      name,
      template,
      creating,
      openDialog,
      setName,
      setTemplate,
      start,
      cancel,
      backToDialog,
      leave,
      create,
    }),
    [
      dialogOpen,
      name,
      template,
      creating,
      openDialog,
      start,
      cancel,
      backToDialog,
      leave,
      create,
    ],
  );
}
