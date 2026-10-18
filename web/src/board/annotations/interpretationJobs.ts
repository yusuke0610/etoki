import { ApiError, boardsApi } from "../../api/boards";
import type { Interpretation, InterpretationJob } from "../../api/types";

/**
 * 解釈のジョブを見に行く間隔（ミリ秒）。
 *
 * 解釈は 1〜数分かかる。短くしても結果が早く届くわけではなく、問い合わせが
 * 増えるだけ。長くすると、終わってから画面に出るまでが遅れる。
 */
export const JOB_POLL_INTERVAL_MS = 1500;

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
 * 見に行く呼び出しが失敗したら、その例外をそのまま投げる。ジョブが消えていれば
 * 404、サーバーに届かなければネットワークの失敗で、呼び出し側が失敗として見せる。
 */
export async function awaitJob(
  boardId: string,
  jobId: string,
  isCurrent: () => boolean,
  intervalMs: number = JOB_POLL_INTERVAL_MS,
): Promise<JobOutcome | undefined> {
  for (;;) {
    if (!isCurrent()) return undefined;
    const job = await boardsApi.interpretationJob(boardId, jobId);
    if (!isCurrent()) return undefined;

    const outcome = outcomeOf(job);
    if (outcome) return outcome;

    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }
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
