import { ApiError, boardsApi } from "../../api/boards";
import type { Interpretation, InterpretationJob } from "../../api/types";

/**
 * 解釈のジョブを見に行く間隔（ミリ秒）。
 *
 * 解釈は 1〜数分かかる。短くしても結果が早く届くわけではなく、問い合わせが
 * 増えるだけ。長くすると、終わってから画面に出るまでが遅れる。
 */
export const JOB_POLL_INTERVAL_MS = 1500;

/**
 * 見に行く呼び出しが続けて失敗してよい回数。
 *
 * 解釈は数分かかり、そのあいだに何十回も見に行く。1 回の通信の失敗で待つのを
 * やめると、ジョブはサーバーで走り続けて結果ができるのに、画面は失敗を出す。
 * 押し直したときにジョブが終わっていれば、同じ入力でもう一度課金する。
 * 上限を置くのは、サーバーが落ちたままのときに待ち続けないため。
 */
export const JOB_POLL_MAX_FAILURES = 5;

/** ジョブが走り終わった結果。 */
export type JobOutcome =
  { kind: "done"; result: Interpretation } | { kind: "failed"; error: ApiError };

/**
 * ジョブが走り終わるまで見に行く（ADR 0085）。
 *
 * 見に行くのを**やめる**のはここ（`isCurrent` が false になったとき）で、
 * **ジョブを止めるのではない。** 画面を離れても、解釈はサーバーで走り続ける。
 * やめたときは `undefined` を返す。
 *
 * **一時的な失敗（届かない・5xx）は見に行き直す。** 続けて
 * `JOB_POLL_MAX_FAILURES` 回失敗したら、最後の例外を投げる。4xx（ジョブが消えた
 * 404、権限を失った 403 など）は見に行き直しても変わらないので、すぐ投げる。
 * 呼び出し側が失敗として見せる。
 */
export async function awaitJob(
  boardId: string,
  jobId: string,
  isCurrent: () => boolean,
  intervalMs: number = JOB_POLL_INTERVAL_MS,
): Promise<JobOutcome | undefined> {
  let failures = 0;
  for (;;) {
    if (!isCurrent()) return undefined;
    let job: InterpretationJob | undefined;
    try {
      job = await boardsApi.interpretationJob(boardId, jobId);
      failures = 0;
    } catch (e) {
      failures += 1;
      if (!isTransient(e) || failures >= JOB_POLL_MAX_FAILURES) throw e;
    }
    if (!isCurrent()) return undefined;

    const outcome = job && outcomeOf(job);
    if (outcome) return outcome;

    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }
}

/** 見に行き直せば通りうる失敗か。サーバーが答えた 4xx は、何度見ても同じ。 */
function isTransient(e: unknown): boolean {
  return !(e instanceof ApiError) || e.status >= 500;
}

/** 走り終わっていればその結果を、走っていれば undefined を返す。 */
function outcomeOf(job: InterpretationJob): JobOutcome | undefined {
  switch (job.status) {
    case "running":
      return undefined;
    case "succeeded":
      if (job.result) return { kind: "done", result: job.result };
      // 契約では成功なら必ず入る。無ければ、ジョブの失敗として見せる。
      return failure("internal", "the job succeeded without a result");
    case "failed":
      return failure(job.error?.code ?? "internal", job.error?.error ?? "");
    default:
      // サーバーのほうが新しく、知らない状態が来た。待ち続けず失敗として見せる。
      return failure("internal", `unknown job status: ${String(job.status)}`);
  }
}

/**
 * ジョブの失敗を、リクエストの失敗と同じ型にする。
 *
 * 文言の対応表（`errorMessage.ts`）は `code` で引くので、画面はジョブ経由か
 * どうかを区別しない。ステータスは応答のものではないので、上流の失敗を表す 502 を
 * 置く。読まれる場所は無い。
 */
function failure(code: string, message: string): JobOutcome {
  return { kind: "failed", error: new ApiError(502, code, message) };
}
