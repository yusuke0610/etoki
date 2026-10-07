import { useCallback, useMemo, useState } from "react";

import { boardsApi } from "../../api/boards";
import { describeFailure } from "../../api/errorMessage";
import type { Interpretation, SyncItem } from "../../api/types";
import type { Exclusion } from "../exclusion";
import { createGenerations } from "../generation";
import type { CreationState } from "./panelShared";

type Options = {
  boardId: string;
  /** 保存・取り込みとの排他（`exclusion.ts`）。**BoardPage に 1 つ。** */
  exclusive: Exclusion;
  /** 作ったものを解釈に結びつける（`useInterpretations`、ADR 0052）。 */
  recordCreated: (
    annotationId: string,
    interpretationId: number,
    items: SyncItem[],
  ) => void;
  /** 引いてある実行の履歴を捨てる（`useRunHistories`）。 */
  discardRuns: (annotationId: string) => void;
  /** 注釈の状態を取り直す。失敗は自分で出す。 */
  refreshAnnotations: () => Promise<void>;
};

export type Creation = {
  /** 注釈 ID をキーにした、作成の結果。 */
  states: Record<string, CreationState>;
  create: (
    annotationId: string,
    interpretationId: number,
    interpretation: Interpretation,
  ) => Promise<void>;
  /**
   * 作成の結果と、走っている作成の応答をすべて捨てる。
   *
   * 呼ぶのは保存だけ。**何を捨てるかの一覧は `BoardPage` の 1 箇所にある**（#146）。
   * 捨てるのは画面の結果だけで、GitHub への作成は止まらない（止められない）。
   */
  discardAll: () => void;
};

/** 解釈結果から draft issue を作る（#146 で `BoardPage` から切り出し）。 */
export function useCreation({
  boardId,
  exclusive,
  recordCreated,
  discardRuns,
  refreshAnnotations,
}: Options): Creation {
  const [states, setStates] = useState<Record<string, CreationState>>({});
  // 作成は解釈とは別の世代で管理する。共有すると、片方の実行がもう片方の
  // 応答まで無効にしてしまう。
  const [generations] = useState(createGenerations);

  /**
   * 解釈結果から draft issue を作る。
   *
   * 作成後は状態が created に変わるので、注釈の状態を取り直す。
   */
  const create = useCallback(
    async (
      annotationId: string,
      interpretationId: number,
      interpretation: Interpretation,
    ) => {
      // disabled は表示の約束。保存や取り込みの最中、または別の注釈を作成中に
      // 直接呼ばれても、取り消せない GitHub への作成を並走させない
      // （表は `exclusion.ts`）。
      await exclusive.run("creating", async () => {
        const generation = generations.start(annotationId);
        setStates((prev) => ({ ...prev, [annotationId]: { status: "running" } }));

        try {
          const run = await boardsApi.createItems(boardId, annotationId, interpretation);
          // 保存が挟まっていたら、この結果は保存前の解釈に対するもの。表示すると
          // いまの内容に対して作られたと誤読される。
          if (!generations.isCurrent(annotationId, generation)) return;
          // 作ったものを解釈に結びつける。下書きはこれを見て、作れた項目を
          // 同じ下書きから新規に作らせない（ADR 0052）。応答を受けた時点で
          // 入れる。実行中は下書きが止まっているので、ここで外れても押せない。
          recordCreated(annotationId, interpretationId, run.items);
          // 履歴は 1 件増えたので、引いてあるものは捨てる（`useRunHistories`）。
          discardRuns(annotationId);
          await refreshAnnotations();
          // **状態を取り直してから done にする。** 先に done にすると、作ったばかりの
          // item が畳み込みに入る前の隙間で、保存や押し直しと競合する
          // （.claude/rules/async-ui.md）。取り直しの失敗は refreshAnnotations が
          // 自分で出すので、ここでは待つだけ。
          if (!generations.isCurrent(annotationId, generation)) return;
          setStates((prev) => ({
            ...prev,
            [annotationId]: { status: "done", run },
          }));
        } catch (e) {
          if (!generations.isCurrent(annotationId, generation)) return;
          setStates((prev) => ({
            ...prev,
            [annotationId]: {
              status: "error",
              failure: describeFailure("作成できませんでした", e),
            },
          }));
        }
      });
    },
    [boardId, discardRuns, exclusive, generations, recordCreated, refreshAnnotations],
  );

  const discardAll = useCallback(() => {
    generations.invalidateAll();
    setStates({});
  }, [generations]);

  return useMemo(() => ({ states, create, discardAll }), [states, create, discardAll]);
}
