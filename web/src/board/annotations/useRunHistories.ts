import { useCallback, useMemo, useState } from "react";

import { boardsApi } from "../../api/boards";
import { describeFailure } from "../../api/errorMessage";
import { useReentryGuard } from "../exclusion";
import { createGenerations } from "../generation";
import type { RunHistoryState } from "./panelShared";

export type RunHistories = {
  /** 注釈 ID をキーにした実行履歴。開いていない注釈は入っていない。 */
  states: Record<string, RunHistoryState>;
  /** その注釈の実行履歴を引く。 */
  load: (annotationId: string) => Promise<void>;
  /**
   * 引いてある履歴を捨て、走っている読み込みも無効にする。
   *
   * 作成で履歴が 1 件増えたときに呼ぶ（`useCreation`）。
   */
  discard: (annotationId: string) => void;
};

/** 注釈ごとの実行の履歴（ADR 0007。#146 で `BoardPage` から切り出し）。 */
export function useRunHistories(boardId: string): RunHistories {
  const [states, setStates] = useState<Record<string, RunHistoryState>>({});
  // 履歴の読み込みは解釈・作成とは別の世代で持つ。作成すると履歴は 1 件増えるので、
  // 走っている読み込みは古くなる。
  const [generations] = useState(createGenerations);
  // 走っているあいだの二重押しは注釈ごとに弾く（`exclusion.ts`）。
  const loading = useReentryGuard();

  /**
   * その注釈の実行履歴を引く。
   *
   * **押されたときだけ引く。** 開いただけで全注釈ぶん引くと、注釈の数だけ
   * 問い合わせが増える（中核思想 3、作成先の名前の取り直しと同じ形）。
   */
  const load = useCallback(
    async (annotationId: string) => {
      if (!loading.enter(annotationId)) return;

      // 走っているあいだに作成が終わると、この応答は 1 件足りない履歴になる。
      // 世代で照合して捨てる（`.claude/rules/async-ui.md`）。
      const generation = generations.start(annotationId);

      setStates((prev) => ({ ...prev, [annotationId]: { status: "loading" } }));
      try {
        const runs = await boardsApi.runs(boardId, annotationId);
        if (!generations.isCurrent(annotationId, generation)) return;
        setStates((prev) => ({
          ...prev,
          [annotationId]: { status: "done", runs },
        }));
      } catch (e) {
        if (!generations.isCurrent(annotationId, generation)) return;
        // パネル内に残す。どの注釈の履歴で失敗したかが情報の一部なので、
        // 画面全体のエラー表示には流さない（解釈の失敗と同じ扱い）。
        setStates((prev) => ({
          ...prev,
          [annotationId]: {
            status: "error",
            failure: describeFailure("履歴を読み込めませんでした", e),
          },
        }));
      } finally {
        loading.leave(annotationId);
      }
    },
    [boardId, generations, loading],
  );

  // **黙って古いまま出さない。** 読み直すかどうかは、これまでどおり押した人が
  // 決める。走っている読み込みも無効にする。捨てた直後に古い応答が入ると、
  // 作ったばかりの run が抜けた履歴が残る。
  const discard = useCallback(
    (annotationId: string) => {
      generations.start(annotationId);
      setStates((prev) => {
        if (!(annotationId in prev)) return prev;
        const next = { ...prev };
        delete next[annotationId];
        return next;
      });
    },
    [generations],
  );

  return useMemo(() => ({ states, load, discard }), [states, load, discard]);
}
