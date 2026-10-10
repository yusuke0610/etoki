import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useMemo, useState } from "react";

import { boardsApi } from "../../api/boards";
import { describeFailure } from "../../api/errorMessage";
import type { AnnotationStatus, Granularity, SyncItem } from "../../api/types";
import { exportAnnotationImage } from "../../excalidraw/image";
import { createGenerations } from "../generation";
import {
  addInterpretation,
  failInterpretation,
  recordCreated as recordCreatedIn,
  selectInterpretation,
  startInterpretation,
  type InterpretationState,
} from "./interpretationHistory";

type Options = {
  api: ExcalidrawImperativeAPI | null;
  boardId: string;
  /** 保存済みシーンの注釈。解釈したときの粒度を控えるのに使う。 */
  annotations: AnnotationStatus[];
};

export type InterpretOptions = {
  /**
   * 控える粒度。**保存の直後に呼ぶ側だけが渡す。** `annotations`（保存済みシーンの
   * 注釈）は保存で取り直されるが、呼び出しの時点の関数はまだ古い一覧を握っている。
   * 省くと保存前の粒度が履歴に残る（「絵解き」、#247）。
   */
  granularity?: Granularity;
};

export type Interpretations = {
  /** 注釈 ID をキーにした、引いた解釈。フロントのメモリだけに持つ。 */
  states: Record<string, InterpretationState>;
  interpret: (annotationId: string, options?: InterpretOptions) => Promise<void>;
  /** 見る解釈を選び直す。 */
  select: (annotationId: string, runId: number) => void;
  /** 作ったものを、それを作った解釈に結びつける（ADR 0052）。 */
  recordCreated: (
    annotationId: string,
    interpretationId: number,
    items: SyncItem[],
  ) => void;
  /**
   * 引いた解釈と、走っている解釈の応答をすべて捨てる。
   *
   * 呼ぶのは保存だけ。**何を捨てるかの一覧は `BoardPage` の 1 箇所にある**（#146）。
   */
  discardAll: () => void;
};

/** 注釈を解釈させ、引いた解釈を注釈ごとに積む（#146 で `BoardPage` から切り出し）。 */
export function useInterpretations({
  api,
  boardId,
  annotations,
}: Options): Interpretations {
  const [states, setStates] = useState<Record<string, InterpretationState>>({});
  // 実行中の解釈を無効にするための世代。useState の初期化関数で 1 度だけ作る。
  const [generations] = useState(createGenerations);

  /**
   * 注釈を解釈させる。
   *
   * エラーはパネル内に残す。どの注釈で何が起きたか分からなくなるので、
   * 画面全体のエラー表示には流さない。
   */
  const interpret = useCallback(
    async (annotationId: string, options?: InterpretOptions) => {
      // 応答を受け取ったとき、これがまだ最新の要求かを判断できるようにする。
      // 履歴に積むかどうかもこれで決める。**捨てるべき応答を捨てる責任は
      // 世代側にあり、履歴は返ってきたものを積むだけ。**
      const generation = generations.start(annotationId);
      // 実行したときの粒度を控える。並べて見比べるとき、同じ指定で引き直した
      // のか指定を変えたのかが読めないと選ぶ理由が無い。判定に使う粒度は
      // これまでどおり保存済みシーン側（AnnotationStatus）のもの。
      const granularity =
        options?.granularity ??
        annotations.find((a) => a.id === annotationId)?.granularity ??
        "";

      setStates((prev) => ({
        ...prev,
        [annotationId]: startInterpretation(prev[annotationId]),
      }));

      try {
        // 画像は画面から書き出す。テキストは保存済みシーンから取るので、
        // 未保存のあいだは押させない（ADR 0018）。ここに来た時点で両者は
        // 揃っている。
        const image = api ? await exportAnnotationImage(api, annotationId) : undefined;
        if (!generations.isCurrent(annotationId, generation)) return;

        const result = await boardsApi.interpret(boardId, annotationId, image);
        if (!generations.isCurrent(annotationId, generation)) return;
        setStates((prev) => ({
          ...prev,
          [annotationId]: addInterpretation(prev[annotationId], {
            id: generation,
            at: new Date().toISOString(),
            granularity,
            result,
          }),
        }));
      } catch (e) {
        if (!generations.isCurrent(annotationId, generation)) return;
        setStates((prev) => ({
          ...prev,
          [annotationId]: failInterpretation(
            prev[annotationId],
            describeFailure("解釈できませんでした", e),
          ),
        }));
      }
    },
    [annotations, api, boardId, generations],
  );

  /**
   * 見る解釈を選び直す。
   *
   * 画面の中だけの操作なので、サーバーにも世代にも触らない。実行中の解釈が
   * 返ってきたら、そちらが選ばれ直す（`addInterpretation`）。引き直した直後に
   * 前の結果が出ていると、押した操作と画面が食い違うため。
   */
  const select = useCallback((annotationId: string, runId: number) => {
    setStates((prev) => {
      const state = prev[annotationId];
      if (!state) return prev;
      return { ...prev, [annotationId]: selectInterpretation(state, runId) };
    });
  }, []);

  // 下書きはこれを見て、作れた項目を同じ下書きから新規に作らせない（ADR 0052）。
  const recordCreated = useCallback(
    (annotationId: string, interpretationId: number, items: SyncItem[]) => {
      setStates((prev) => {
        const state = prev[annotationId];
        if (!state) return prev;
        return {
          ...prev,
          [annotationId]: recordCreatedIn(state, interpretationId, items),
        };
      });
    },
    [],
  );

  const discardAll = useCallback(() => {
    generations.invalidateAll();
    setStates({});
  }, [generations]);

  return useMemo(
    () => ({ states, interpret, select, recordCreated, discardAll }),
    [states, interpret, select, recordCreated, discardAll],
  );
}
