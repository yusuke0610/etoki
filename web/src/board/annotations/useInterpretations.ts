import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { boardsApi } from "../../api/boards";
import { describeFailure } from "../../api/errorMessage";
import type { AnnotationStatus, Granularity, SyncItem } from "../../api/types";
import { exportAnnotationImage } from "../../excalidraw/image";
import { createGenerations } from "../generation";
import {
  addInterpretation,
  cancelInterpretation,
  failInterpretation,
  recordCreated as recordCreatedIn,
  selectInterpretation,
  startInterpretation,
  trackJob,
  type InterpretationState,
} from "./interpretationHistory";
import { awaitJob, JOB_POLL_INTERVAL_MS } from "./interpretationJobs";

type Options = {
  api: ExcalidrawImperativeAPI | null;
  boardId: string;
  /** 保存済みシーンの注釈。解釈したときの粒度を控えるのに使う。 */
  annotations: AnnotationStatus[];
  /**
   * 解釈を使えるか。使えるときだけ、開いたときに走っているジョブを引く。
   *
   * 未設定のサーバーに引くと 503 が返るだけで、利用者に見せるものが無い。
   */
  enabled: boolean;
  /** ジョブを見に行く間隔。テストが待たずに済むよう差し替えられる。 */
  pollIntervalMs?: number;
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
  /**
   * 走っている解釈を取り消す（#330）。サーバーのジョブを止め、見に行くのも
   * やめる。失敗にはしない。
   *
   * **ジョブが分かる前は何もしない**（`InterpretationState.jobId`）。画面は
   * `jobId` があるときだけ取り消しの口を出す。
   */
  cancel: (annotationId: string) => void;
  /** 作ったものを、それを作った解釈に結びつける（ADR 0052）。 */
  recordCreated: (
    annotationId: string,
    interpretationId: number,
    items: SyncItem[],
  ) => void;
  /**
   * 引いた解釈と、走っている解釈をすべて捨てる。サーバーのジョブも止める。
   *
   * 呼ぶのは保存だけ。**何を捨てるかの一覧は `BoardPage` の 1 箇所にある**（#146）。
   */
  discardAll: () => void;
};

/**
 * 注釈を解釈させ、引いた解釈を注釈ごとに積む（#146 で `BoardPage` から切り出し）。
 *
 * **解釈はサーバーのジョブとして走る**（ADR 0085）。ここは始めて、終わるまで
 * 見に行き、結果を積む。画面を離れても解釈は止まらず、開き直したときに走って
 * いるぶんを引いて続きを見に行く。
 */
export function useInterpretations({
  api,
  boardId,
  annotations,
  enabled,
  pollIntervalMs = JOB_POLL_INTERVAL_MS,
}: Options): Interpretations {
  const [states, setStates] = useState<Record<string, InterpretationState>>({});
  // 実行中の解釈を無効にするための世代。useState の初期化関数で 1 度だけ作る。
  const [generations] = useState(createGenerations);

  // 見に行くのをやめる印。**ジョブは止めない。** 立てるのは effect で、StrictMode が
  // effect を 2 回走らせても、2 回目で立て直す。
  const alive = useRef(false);
  // 保存で捨てた回数。始めたあと、応答が返る前に保存が挟まったかを見る。世代は
  // 「同じ注釈の新しい要求」でも進むので、そちらでは区別できない。
  const epoch = useRef(0);
  // いま手元で追っているジョブ。保存で捨てるときに、サーバーのジョブも止める。
  const jobIds = useRef(new Set<string>());
  // 注釈ごとに、いま見に行っているジョブ。取り消すときに引く。state の `jobId` と
  // 同じものを持つが、押した時点の値を読むためにこちらも持つ（state は描画の
  // あとでしか新しくならない）。
  const tracked = useRef(new Map<string, string>());
  // 復元のとき、実行した粒度を控えるのに最新の一覧を読む。依存に入れると、一覧が
  // 変わるたびにジョブを引き直す。
  const latestAnnotations = useRef(annotations);
  useEffect(() => {
    latestAnnotations.current = annotations;
  }, [annotations]);

  /** ジョブを要らないものとして捨てる。失敗しても何もしない。 */
  const discardJob = useCallback(
    (jobId: string) => {
      jobIds.current.delete(jobId);
      // 捨て損ねても、保持の期限（1 時間）で消える。画面に出すものが無い。
      boardsApi.discardInterpretationJob(boardId, jobId).catch(() => undefined);
    },
    [boardId],
  );

  /**
   * ジョブが走り終わるまで見に行き、結果か失敗を積む。
   *
   * **捨てるべき応答を捨てる責任は世代側にあり、履歴は返ってきたものを積むだけ。**
   * 世代が古くなったときはジョブを捨てない。同じ注釈を押し直すと、サーバーは
   * 走っているジョブを返すので、新しい要求も同じジョブを見ている。
   */
  const settle = useCallback(
    async (
      annotationId: string,
      generation: number,
      jobId: string,
      granularity: Granularity,
    ) => {
      const isCurrent = () =>
        alive.current && generations.isCurrent(annotationId, generation);

      // 受け取り終えたら手放す。**古い世代は手放さない。** 押し直した新しい要求が
      // 同じジョブを見ていることがあるので、それの取り消しの口まで消える。
      const untrack = () => {
        if (tracked.current.get(annotationId) === jobId)
          tracked.current.delete(annotationId);
      };

      let outcome;
      try {
        outcome = await awaitJob(boardId, jobId, isCurrent, pollIntervalMs);
      } catch (e) {
        if (!isCurrent()) return;
        untrack();
        setStates((prev) => ({
          ...prev,
          [annotationId]: failInterpretation(
            prev[annotationId],
            describeFailure("絵解きできませんでした", e),
          ),
        }));
        return;
      }
      if (!outcome || !isCurrent()) return;
      untrack();

      if (outcome.kind === "done") {
        const { result } = outcome;
        setStates((prev) => ({
          ...prev,
          [annotationId]: addInterpretation(prev[annotationId], {
            id: generation,
            at: new Date().toISOString(),
            granularity,
            result,
          }),
        }));
      } else {
        setStates((prev) => ({
          ...prev,
          [annotationId]: failInterpretation(
            prev[annotationId],
            describeFailure("絵解きできませんでした", outcome.error),
          ),
        }));
      }
      // 受け取り終えた。残すとサーバーのメモリに溜まり、開き直したときに同じ結果が
      // もう一度出る。
      discardJob(jobId);
    },
    [boardId, discardJob, generations, pollIntervalMs],
  );

  /** 見に行くジョブを控える。保存と取り消しで、サーバーのジョブを止めるのに使う。 */
  const track = useCallback((annotationId: string, jobId: string) => {
    jobIds.current.add(jobId);
    tracked.current.set(annotationId, jobId);
    setStates((prev) => {
      const state = prev[annotationId];
      return state ? { ...prev, [annotationId]: trackJob(state, jobId) } : prev;
    });
  }, []);

  /**
   * 注釈を解釈させる。
   *
   * エラーはパネル内に残す。どの注釈で何が起きたか分からなくなるので、
   * 画面全体のエラー表示には流さない。
   */
  const interpret = useCallback(
    async (annotationId: string, options?: InterpretOptions) => {
      // 応答を受け取ったとき、これがまだ最新の要求かを判断できるようにする。
      // 履歴に積むかどうかもこれで決める。
      const generation = generations.start(annotationId);
      const startedEpoch = epoch.current;
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

      let jobId: string;
      try {
        // 画像は画面から書き出す。テキストは保存済みシーンから取るので、
        // 未保存なら保存してから呼ぶ（ADR 0018 / 0077、`BoardPage` の
        // `interpretAfterSave`）。ここに来た時点で両者は揃っている。
        const image = api ? await exportAnnotationImage(api, annotationId) : undefined;
        if (!generations.isCurrent(annotationId, generation)) return;

        jobId = (await boardsApi.interpret(boardId, annotationId, image)).id;
      } catch (e) {
        if (!generations.isCurrent(annotationId, generation)) return;
        setStates((prev) => ({
          ...prev,
          [annotationId]: failInterpretation(
            prev[annotationId],
            describeFailure("絵解きできませんでした", e),
          ),
        }));
        return;
      }

      // 始めている間に保存が挟まった。保存前のシーンの解釈なので、受け取らずに
      // サーバーでも止める。
      if (epoch.current !== startedEpoch) {
        discardJob(jobId);
        return;
      }
      if (!generations.isCurrent(annotationId, generation)) return;

      track(annotationId, jobId);
      await settle(annotationId, generation, jobId, granularity);
    },
    [annotations, api, boardId, discardJob, generations, settle, track],
  );

  // 開いたとき、走っている解釈と、終わったが受け取っていない解釈を画面に戻す。
  // **ボードごとに 1 回だけ引く。** 注釈ごとには引かない（`web/CLAUDE.md`）。
  useEffect(() => {
    alive.current = true;
    if (!enabled) {
      return () => {
        alive.current = false;
      };
    }

    let cancelled = false;
    const startedEpoch = epoch.current;
    void (async () => {
      let jobs;
      try {
        jobs = await boardsApi.interpretationJobs(boardId);
      } catch {
        // 復元は付け足し。引けなくても、押せば始められる。
        return;
      }
      if (cancelled) return;

      for (const job of jobs) {
        // 引いているあいだに保存が挟まった。保存前のシーンの解釈は受け取らない。
        if (epoch.current !== startedEpoch) {
          discardJob(job.id);
          continue;
        }
        const annotationId = job.annotationId;
        const generation = generations.start(annotationId);
        const granularity =
          latestAnnotations.current.find((a) => a.id === annotationId)?.granularity ?? "";
        setStates((prev) => ({
          ...prev,
          [annotationId]: startInterpretation(prev[annotationId]),
        }));
        track(annotationId, job.id);
        void settle(annotationId, generation, job.id, granularity);
      }
    })();

    return () => {
      cancelled = true;
      alive.current = false;
    };
  }, [boardId, discardJob, enabled, generations, settle, track]);

  const cancel = useCallback(
    (annotationId: string) => {
      const jobId = tracked.current.get(annotationId);
      if (jobId === undefined) return;
      tracked.current.delete(annotationId);
      // 世代を進めて、見に行っているぶんを止める。止めたジョブを見に行き続けると、
      // 消えたジョブの 404 を失敗として出す。
      generations.start(annotationId);
      discardJob(jobId);
      setStates((prev) => {
        const state = prev[annotationId];
        return state ? { ...prev, [annotationId]: cancelInterpretation(state) } : prev;
      });
    },
    [discardJob, generations],
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
    epoch.current += 1;
    generations.invalidateAll();
    setStates({});
    tracked.current.clear();
    // サーバーで走っているものも止める。残すと、保存前のシーンを解釈した結果が
    // 開き直したときに戻り、走っているあいだ枠を握り続ける。
    for (const jobId of [...jobIds.current]) discardJob(jobId);
  }, [discardJob, generations]);

  return useMemo(
    () => ({ states, interpret, select, cancel, recordCreated, discardAll }),
    [states, interpret, select, cancel, recordCreated, discardAll],
  );
}
