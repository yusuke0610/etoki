import { useCallback, useEffect, useMemo, useState } from "react";

import type { AnnotationStatus } from "../../api/types";
import { ANNOTATION_LIST_HEADING_ID } from "./AnnotationPanel";

/**
 * 保存済みの一覧にその注釈が在るか。開いていた詳細を閉じる判定（このフック）と、
 * 保存のあとに読むかどうかの判定（`useEkidoki` の `interpretAfterSave`）が使う。
 * **同じ条件で決める。** ずれると、詳細が閉じたのに読む（結果を出す先が無い）か、
 * 開いたままなのに読まない。
 *
 * **一覧が壊れていても投げない。** 詳細を閉じる判定はパネルの境界の外にあり、
 * 投げると画面ごと落ちる（ADR 0027、`railBadgesOf` と同じ）。
 */
export function listed(annotations: readonly AnnotationStatus[], id: string): boolean {
  return (annotations as readonly (AnnotationStatus | null)[]).some((a) => a?.id === id);
}

export type AnnotationDetailState = {
  /** キャンバスの上に開いている注釈の詳細（`AnnotationDetail`）。null なら閉じている。 */
  detailId: string | null;
  /**
   * 詳細を開く操作の回数。開いたままの注釈を開き直しても焦点を移すために、
   * 注釈 ID とは別に持つ（`AnnotationDetail`）。
   */
  detailRequest: number;
  openDetail: (id: string) => void;
  closeDetail: () => void;
};

/**
 * 注釈の詳細の開閉（#352 で `BoardPage` から切り出し）。annotations は保存済みの
 * 注釈の一覧で、詳細を閉じる判定に使う。
 *
 * **開いていた詳細の注釈が保存で消えたら、詳細を閉じる。** 描いたまま残すと、
 * 同じ注釈が戻ったとき（元に戻して保存したとき）に、閉じたはずの詳細が開き直る。
 */
export function useAnnotationDetail(
  annotations: readonly AnnotationStatus[],
): AnnotationDetailState {
  const [detailId, setDetailId] = useState<string | null>(null);
  const [detailRequest, setDetailRequest] = useState(0);
  const openDetail = useCallback((id: string) => {
    setDetailId(id);
    setDetailRequest((n) => n + 1);
  }, []);
  const closeDetail = useCallback(() => setDetailId(null), []);
  // 開いていた詳細を、注釈が消えたので閉じた回数（下の effect が焦点を移す）。
  // **ID ではなく回数で持つ。** 同じ注釈が戻ってまた消えたとき、ID だと値が
  // 変わらず effect が走らない。
  const [detailVanished, setDetailVanished] = useState(0);

  // 描いている最中に置く。前の値と違うときだけ置く形なので React が許している。
  // ここはパネルの境界の外なので、一覧が壊れていても投げない（`listed`）。
  if (detailId !== null && !listed(annotations, detailId)) {
    setDetailVanished((n) => n + 1);
    setDetailId(null);
  }
  // 閉じた詳細の中に焦点があったなら、一覧の見出しへ移す。**焦点が行き場を
  // 失ったときだけ。** キャンバスで描いている最中に保存して消えたなら、焦点は
  // キャンバスにあるので動かさない。
  useEffect(() => {
    if (detailVanished === 0) return;
    if (document.activeElement === null || document.activeElement === document.body) {
      document.getElementById(ANNOTATION_LIST_HEADING_ID)?.focus();
    }
  }, [detailVanished]);

  return useMemo(
    () => ({ detailId, detailRequest, openDetail, closeDetail }),
    [detailId, detailRequest, openDetail, closeDetail],
  );
}
