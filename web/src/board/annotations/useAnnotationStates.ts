import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { boardsApi } from "../../api/boards";
import { describeFailure, type Failure } from "../../api/errorMessage";
import type { AnnotationStatus, DetachedAnnotation } from "../../api/types";
import type { NotifyOptions } from "../../notification/types";

/**
 * 注釈の状態が取れなかったときの通知の key。
 *
 * **effect から出すものには key を付ける。** 開発時の StrictMode では effect が
 * 2 回走るので、畳まないと同じ失敗が 2 件並ぶ。注釈の状態は保存や作成のたびにも
 * 取り直すので、読めたら下げる。ボードを離れるときに下げるのは `BoardPage`。
 */
export const ANNOTATIONS_FAILED = "annotations-failed";

export type AnnotationStates = {
  /** 保存済みシーンの注釈と、その 3 状態。状態は保存済みシーンが基準。 */
  annotations: AnnotationStatus[];
  /**
   * シーンから消えたのに GitHub 側にものが残っている注釈（#111）。
   *
   * **`annotations` と混ぜない。** 3 状態も名前も無いので、注釈のカードと
   * 同じ形では出せない。同じ問い合わせで返るので、引く回数は増えない。
   */
  detached: DetachedAnnotation[];
  /**
   * 注釈の状態を取り直し、反映した一覧を返す。取れなかったら null（通知は出す）。
   *
   * **成否を返す。** 取り直した一覧で開く先や読むかどうかを決める側
   * （`startEkidoki` / `interpretAfterSave`）が待って使う。state は描き直すまで
   * 古く、取れなかったときは前の一覧のまま残るので、どちらも判断に使えない。
   * 追い越されたら、追い越した取得の結果を返す（反映されるのはそちらなので）。
   */
  refreshAnnotations: () => Promise<AnnotationStatus[] | null>;
};

/**
 * 保存済みの注釈の状態（#352 で `BoardPage` から切り出し）。開いたら 1 度引き、
 * 保存・作成のあとは呼ぶ側が取り直す。
 *
 * **取り直しは etoki 自身の状態の読み直しであって、GitHub への同期ではない**
 * （`.claude/rules/async-ui.md`）。
 */
export function useAnnotationStates({
  boardId,
  onError,
  dismiss,
}: {
  boardId: string;
  onError: (failure: Failure, options?: Pick<NotifyOptions, "action" | "key">) => void;
  /** 通知を key で下げる。画面から外れたあとは下げない側を渡す（`BoardPage`）。 */
  dismiss: (key: string) => void;
}): AnnotationStates {
  const [annotations, setAnnotations] = useState<AnnotationStatus[]>([]);
  const [detached, setDetached] = useState<DetachedAnnotation[]>([]);

  // 状態の取得は初期表示・保存・作成から重なって走る。番号を振って最後に
  // 投げたものだけ反映する。古い応答で上書きすると、作成済みの注釈が未作成に
  // 巻き戻って見える。
  const request = useRef(0);
  // 最後に投げた取得。追い越された取得は、これの結果を返す。
  const latest = useRef<Promise<AnnotationStatus[] | null> | null>(null);

  const refreshAnnotations = useCallback((): Promise<AnnotationStatus[] | null> => {
    const mine = ++request.current;
    const pending = (async (): Promise<AnnotationStatus[] | null> => {
      try {
        const next = await boardsApi.annotations(boardId);
        if (mine !== request.current) return latest.current;
        setAnnotations(next.annotations);
        setDetached(next.detached);
        dismiss(ANNOTATIONS_FAILED);
        return next.annotations;
      } catch (e) {
        if (mine !== request.current) return latest.current;
        onError(describeFailure("絵の状態を取得できませんでした", e), {
          key: ANNOTATIONS_FAILED,
        });
        return null;
      }
    })();
    latest.current = pending;
    return pending;
  }, [boardId, dismiss, onError]);

  // 開いたら注釈の状態を引く。state を置くのは応答が届いてから（`await` の後）で、
  // effect の本体からは置かない。
  useEffect(() => {
    void refreshAnnotations();
  }, [refreshAnnotations]);

  return useMemo(
    () => ({ annotations, detached, refreshAnnotations }),
    [annotations, detached, refreshAnnotations],
  );
}
