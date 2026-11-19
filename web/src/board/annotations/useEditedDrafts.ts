import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export type EditedDrafts = {
  /** 手直しした下書きがあるか。**離れる前の確認の登録に使う。** */
  any: boolean;
  /**
   * 手直しした下書きがあるかを ref から読む。
   *
   * **押した時点の判定（保存の前の確認）はこちらを使う**（`useDirtyScene` の
   * `isDirty` と同じ理由）。state は描き直すまで古い。
   */
  hasEdited: () => boolean;
  /** 注釈ごとに、手直ししたかを受け取る。`DraftEditor` から呼ばれる。 */
  report: (annotationId: string, edited: boolean) => void;
};

/**
 * 手直しした下書きがある注釈を集める（#432、ADR 0021 の追記）。
 *
 * **下書きそのものは持たない。** 下書きは `DraftEditor` が持つ（そこに置く理由は
 * `DraftEditor` の doc）。ここに上がるのは手直ししたかだけで、離れる前と保存の
 * 前の確認がそれを見る。下書きは保存しない（ADR 0021 のまま）。
 *
 * `onChange` は、ボードの外（`App` の導線）に手直しした下書きの有無を知らせる。
 * 未保存のシーン（`useDirtyScene` の `onDirtyChange`）とは分けて渡す。失うものが
 * 違うので、確認の文言を分ける。
 */
export function useEditedDrafts(onChange: (edited: boolean) => void): EditedDrafts {
  const [edited, setEdited] = useState<ReadonlySet<string>>(() => new Set());
  const editedRef = useRef<ReadonlySet<string>>(edited);

  const report = useCallback((annotationId: string, isEdited: boolean) => {
    const current = editedRef.current;
    if (current.has(annotationId) === isEdited) return;
    const next = new Set(current);
    if (isEdited) next.add(annotationId);
    else next.delete(annotationId);
    // **ref と state を一緒に書く。** 片方だけだと「手直しがあるか」の答えが 2 つになる。
    editedRef.current = next;
    setEdited(next);
  }, []);

  const any = edited.size > 0;

  useEffect(() => {
    onChange(any);
  }, [any, onChange]);

  // 外れるときは下ろす（`useDirtyScene` と同じ）。残すと、ボードがもう無いのに
  // 親が止め続ける。
  useEffect(() => () => onChange(false), [onChange]);

  const hasEdited = useCallback(() => editedRef.current.size > 0, []);

  return useMemo(() => ({ any, hasEdited, report }), [any, hasEdited, report]);
}
