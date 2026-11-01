import {
  type KeyboardEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

export type NameDraft = {
  /** 書いている途中の名前。null なら書いていない（ラベルを出している）。 */
  draft: string | null;
  /** 欄を開く（ラベルを押した）。いまの名前から書き始める。 */
  start: () => void;
  change: (value: string) => void;
  /**
   * 書きかけを確定する。書いていなければ何もしない。**変えていなければ書かない。**
   *
   * 欄を離れたときのほかに、詳細の「絵解く」からも呼ぶ。帯のボタンは押しても
   * 焦点を奪わない（`DetailBand`）ので、欄を離れたときの確定が走らない。
   */
  commit: () => void;
  /** 欄の Enter（確定）と Escape（書きかけを捨てる）。 */
  onInputKeyDown: (e: KeyboardEvent<HTMLInputElement>) => void;
  /**
   * 欄の `ref` に渡す。出た一度だけ焦点を入れる。
   *
   * **ref オブジェクトにせず関数で渡す。** 束を受けた側は、分けてから `ref` に
   * 渡す（`AnnotationNameLabel`）。束のまま `ref` に渡すと、`react-hooks/refs` が
   * 束ごと ref とみなし、`draft` まで描画中に読めなくなる。
   */
  attachInput: (el: HTMLInputElement | null) => void;
  /** ラベル（名前を押して変えるボタン）の `ref` に渡す。閉じたあとの焦点の戻り先。 */
  attachLabel: (el: HTMLButtonElement | null) => void;
};

/**
 * 注釈の名前欄の書きかけ（#384 で `AnnotationFace` から切り出し、#249 / #305）。
 *
 * 名前は**キャンバスの値**を出す。ただし入力の途中は書きかけを持ち、**確定
 * （Enter か欄を離れる）したときだけキャンバスに書く。** 1 文字ごとに書くと、
 * 打つたびに未保存になり、「元に戻す」に 1 文字ずつ積まれる。
 *
 * 名前欄の約束は `.claude/rules/async-ui.md` に積まれている（#261 / #308）。
 * 開く条件は開いたあとも見続ける・変えたかどうかは開いた時点の値と比べる・
 * 変換中の Enter では確定しない。`useNameDraft.test.ts` が押さえる。
 */
export function useNameDraft({
  id,
  name,
  canRename,
  onChangeName,
}: {
  /** 注釈（frame）の ID。 */
  id: string;
  /** いまの名前（キャンバスの値。まだ分からなければ保存済みの値）。 */
  name: string;
  /** 名前を変えられるか（編集できる・キャンバスに在る・注釈から外していない）。 */
  canRename: boolean;
  /** frame の名前を書き換える。確定したときだけ呼ぶ。 */
  onChangeName: (frameId: string, name: string) => void;
}): NameDraft {
  const [draft, setDraft] = useState<string | null>(null);
  // 書いている途中で押せる条件を失ったら（読むだけに変わった・注釈から外した・
  // キャンバスから消えた）、書きかけを捨てて欄を閉じる。書く口が書けない対象に
  // 確定させると入力が黙って消え、条件が戻ったとき古い書きかけが欄ごと復活する。
  // effect ではなく描画中に捨てるのは、1 回ぶんの古い欄を描かないため。
  if (!canRename && draft !== null) setDraft(null);
  // 欄を開いた時点の名前。「変えたか」はこれと比べる。確定時のキャンバスの名前と
  // 比べると、キャンバスの名前が分かる前（保存済みの値から始めた）に開いた下書きを、
  // 触っていないのに古い名前としてキャンバスへ書き戻す。
  const draftStart = useRef("");
  const label = useRef<HTMLButtonElement | null>(null);
  const attachLabel = useCallback((el: HTMLButtonElement | null) => {
    label.current = el;
  }, []);
  // 欄を閉じたあとに焦点をラベルへ戻すか。Enter と Escape だけ戻す。欄を離れて
  // 確定したときは、離れた先の焦点を奪わない。
  const refocusLabel = useRef(false);
  useEffect(() => {
    if (draft === null && refocusLabel.current) {
      refocusLabel.current = false;
      label.current?.focus();
    }
  }, [draft]);
  // 欄が出た一度だけ焦点を入れる。ref を毎回作り直すと、打つたびに奪い直す。
  const attachInput = useCallback((el: HTMLInputElement | null) => {
    el?.focus();
    el?.select();
  }, []);

  const start = useCallback(() => {
    draftStart.current = name;
    setDraft(name);
  }, [name]);

  const commit = useCallback(() => {
    if (draft === null) return;
    setDraft(null);
    // 開いた時点と同じ条件を確定でも見る。
    if (!canRename) return;
    // 変えていないのに書くと、開いて閉じただけで未保存になる。前後の空白は
    // 書く側（`setAnnotationName`）が落とすので、比べるのも落としてから。
    if (draft.trim() !== draftStart.current.trim()) {
      onChangeName(id, draft);
    }
  }, [canRename, draft, id, onChangeName]);

  const onInputKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      // 日本語の変換を確定する Enter では書かない。書くと、変換の途中の
      // 名前がキャンバスに載る。
      if (e.key === "Enter" && !e.nativeEvent.isComposing) {
        e.preventDefault();
        refocusLabel.current = true;
        commit();
      } else if (e.key === "Escape") {
        // 書きかけを捨てるだけにする。面まで閉じると、書きかけを捨てた
        // つもりで面ごと閉じる。
        e.stopPropagation();
        refocusLabel.current = true;
        setDraft(null);
      }
    },
    [commit],
  );

  return useMemo(
    () => ({
      draft,
      start,
      change: setDraft,
      commit,
      onInputKeyDown,
      attachInput,
      attachLabel,
    }),
    [draft, start, commit, onInputKeyDown, attachInput, attachLabel],
  );
}
