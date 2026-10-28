import { useCallback, useMemo, useRef, useState } from "react";

import { ApiError, boardsApi } from "../../api/boards";
import { describeFailure, type Failure } from "../../api/errorMessage";
import type { BoardDetail } from "../../api/types";

/**
 * 背景の書きかけ。
 *
 * `base` は編集を始めた時点の背景で、保存の照合に送る（ADR 0093）。いまの背景と
 * 食い違えばサーバーは書かずに 409 を返す。
 */
type PremiseDraft = { text: string; base: string };

export type PremiseEditor = {
  /** ダイアログが開いているか。開くのはキャンバスのメニュー。 */
  open: boolean;
  openEditor: () => void;
  closeEditor: () => void;
  /** 欄に出す文。書きかけがあればそれ、無ければいまの背景。 */
  text: string;
  /** 編集を始めた時点の背景から書き換えてあるか。 */
  edited: boolean;
  change: (text: string) => void;
  /** 書きかけを捨てて、いまの背景に戻す。 */
  discard: () => void;
  saving: boolean;
  save: () => Promise<void>;
  /** 保存できなかった理由。フォームの隣に出す（ADR 0058）。 */
  failure: Failure | null;
  /**
   * 先に書き換えられていたときに取り直した、いまの背景。null なら出さない。
   *
   * **書きかけと並べて見せる。** 409 の文言は「見比べてから保存し直す」と言う
   * ので、比べる相手が画面に無いと次の手が打てない。
   */
  latest: string | null;
};

const SAVE_FAILED_ACTION = "ボードの背景を保存できませんでした";

/**
 * ボードの背景（ADR 0093）の編集。ダイアログ（`PremiseDialog`）の中身を持つ。
 *
 * **書きかけはダイアログではなくここで持つ。** ダイアログは閉じると中身を外す
 * ので、そちらに置くと閉じただけで書きかけが消える（`.claude/rules/async-ui.md`
 * の「外れない親で持つ」）。背景は長い文になりうる（上限はサーバーだけが持つ、
 * ADR 0093）ので、共有の招待のように打ち直せば済む量ではない。**ボードを離れたら捨てる。** 名前の書きかけと同じで、
 * 「未保存」（`useDirtyScene`）には数えない。理由は `web/CLAUDE.md` の
 * 「未保存のまま離れさせない」。
 *
 * **ほかの操作と排他にしない**（`exclusion.ts` に行を足さない）。背景はシーンの
 * 外の列で、版も進めない（ADR 0092）ので、保存・取り込み・作成のどれとも同じ
 * ものを書かない。走っている解釈は始めたときの背景で読み続け、結果に渡した
 * 背景が載る。
 */
export function usePremise({
  board,
  onChanged,
}: {
  board: BoardDetail;
  /**
   * サーバーから受け取ったボードで、手元を差し替えてもらう。保存した直後と、
   * 先に書き換えられていたのでいまの背景を取り直したときに呼ぶ。版は動かない
   * ので、保存の基準は据え置く（App の `replaceBoard`、ADR 0092）。
   */
  onChanged: (board: BoardDetail) => void;
}): PremiseEditor {
  const [open, setOpen] = useState(false);
  const [draft, setDraftState] = useState<PremiseDraft | null>(null);
  // 応答を待ったあとで「送った値のままか」を見るために、いまの書きかけを ref にも
  // 持つ。state は押した時点の関数が握った古い値しか読めない。
  const draftRef = useRef<PremiseDraft | null>(null);
  const setDraft = useCallback((next: PremiseDraft | null) => {
    draftRef.current = next;
    setDraftState(next);
  }, []);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [latest, setLatest] = useState<string | null>(null);
  // 二度押しは押した時点の値で弾く。state だと同じ tick の 2 回目がまだ false を
  // 読む（`.claude/rules/async-ui.md`）。
  const inFlight = useRef(false);

  const edited = draft !== null && draft.text !== draft.base;

  const clear = useCallback(() => {
    setDraft(null);
    setFailure(null);
    setLatest(null);
  }, [setDraft]);

  const openEditor = useCallback(() => setOpen(true), []);

  // **書き換えていなければ書きかけを持ち越さない。** 持ち越すと、閉じている
  // あいだにほかの人が保存した背景ではなく、開いた時点の背景を次に開いたときにも
  // 出し続ける。
  const closeEditor = useCallback(() => {
    setOpen(false);
    const current = draftRef.current;
    if (current === null || current.text === current.base) clear();
  }, [clear]);

  const change = useCallback(
    (text: string) => {
      setDraft({ text, base: draftRef.current?.base ?? board.premise });
    },
    [board.premise, setDraft],
  );

  const save = useCallback(async () => {
    if (inFlight.current) return;
    const sending = draftRef.current;
    // 書き換えていなければ送らずに閉じる（改名と同じ）。
    if (sending === null || sending.text === sending.base) {
      setOpen(false);
      clear();
      return;
    }

    inFlight.current = true;
    setSaving(true);
    setFailure(null);
    try {
      const saved = await boardsApi.setPremise(board.id, sending.text, sending.base);
      onChanged(saved);
      setLatest(null);
      // **送った値のままのときだけ書きかけを消して閉じる。** 待つあいだに書き
      // 足したぶんは残し、基準を保存できた背景へ進める。消すと、書き足したものが
      // 黙って消える（`.claude/rules/async-ui.md`）。
      const current = draftRef.current;
      if (current === null || current.text === sending.text) {
        setDraft(null);
        setOpen(false);
      } else {
        setDraft({ text: current.text, base: saved.premise });
      }
    } catch (e) {
      setFailure(describeFailure(SAVE_FAILED_ACTION, e));
      if (e instanceof ApiError && e.code === "premise_conflict") {
        await takeLatest();
      }
    } finally {
      inFlight.current = false;
      setSaving(false);
    }

    /**
     * 先に書き換えられていたので、いまの背景を取り直して並べる。
     *
     * **基準をいまの背景へ進める。** 見比べたうえで押し直せば、その背景を上書き
     * する。押し直すかどうかを決めるのは人（中核思想 3）。書きかけはそのまま残す。
     *
     * 取り直せなければ基準は据え置く。押し直すとまた 409 になり、取り直しから
     * やり直す。理由は 409 の文言が言っている。
     *
     * 背景を返す口は開く口（`boardsApi.get`）だけで、貼った画像も一緒に届く。
     * 409 は 2 人が同時に背景を書き換えたときにしか起きないので、その重さは
     * 受け入れる。背景のためだけの読み取りの口は足さない。
     */
    async function takeLatest() {
      let fresh: BoardDetail;
      try {
        fresh = await boardsApi.get(board.id);
      } catch {
        return;
      }
      onChanged(fresh);
      setLatest(fresh.premise);
      const current = draftRef.current;
      if (current !== null) setDraft({ text: current.text, base: fresh.premise });
    }
  }, [board.id, clear, onChanged, setDraft]);

  return useMemo(
    () => ({
      open,
      openEditor,
      closeEditor,
      text: draft?.text ?? board.premise,
      edited,
      change,
      discard: clear,
      saving,
      save,
      failure,
      latest,
    }),
    [
      open,
      openEditor,
      closeEditor,
      draft,
      board.premise,
      edited,
      change,
      clear,
      saving,
      save,
      failure,
      latest,
    ],
  );
}
