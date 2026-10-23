import { ApiError } from "../api/boards";
import type { ErrorResponse } from "../api/types";

/**
 * ジョブを見に行く間隔（ミリ秒）。
 *
 * 解釈は 1〜数分、図のドラフト生成は数十秒かかる。短くしても結果が早く届くわけ
 * ではなく、問い合わせが増えるだけ。長くすると、終わってから画面に出るまでが遅れる。
 */
export const JOB_POLL_INTERVAL_MS = 1500;

/**
 * 見に行く呼び出しが続けて失敗してよい回数。
 *
 * 待つあいだに何十回も見に行く。1 回の通信の失敗で待つのをやめると、ジョブは
 * サーバーで走り続けて結果ができるのに、画面は失敗を出す。押し直したときに
 * ジョブが終わっていれば、同じ入力でもう一度課金する。上限を置くのは、サーバーが
 * 落ちたままのときに待ち続けないため。
 */
export const JOB_POLL_MAX_FAILURES = 5;

/**
 * 見に行くジョブの形。解釈（ADR 0085）と図のドラフト生成（ADR 0091）で共通の部分。
 */
export type PolledJob<R> = {
  status: string;
  result?: R;
  error?: ErrorResponse;
};

/** ジョブが走り終わった結果。 */
export type JobOutcome<R> =
  { kind: "done"; result: R } | { kind: "failed"; error: ApiError };

/**
 * ジョブが走り終わるまで見に行く。
 *
 * 見に行くのを**やめる**のはここ（`isCurrent` が false になったとき）で、
 * **ジョブを止めるのではない。** 画面を離れても、ジョブはサーバーで走り続ける。
 * やめたときは `undefined` を返す。
 *
 * **一時的な失敗（届かない・5xx）は見に行き直す。** 続けて
 * `JOB_POLL_MAX_FAILURES` 回失敗したら、最後の例外を投げる。4xx（ジョブが消えた
 * 404、権限を失った 403 など）は見に行き直しても変わらないので、すぐ投げる。
 * 呼び出し側が失敗として見せる。
 */
export async function pollJob<R>(
  fetchJob: () => Promise<PolledJob<R>>,
  isCurrent: () => boolean,
  intervalMs: number = JOB_POLL_INTERVAL_MS,
): Promise<JobOutcome<R> | undefined> {
  let failures = 0;
  for (;;) {
    if (!isCurrent()) return undefined;
    let job: PolledJob<R> | undefined;
    try {
      job = await fetchJob();
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
function outcomeOf<R>(job: PolledJob<R>): JobOutcome<R> | undefined {
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
function failure<R>(code: string, message: string): JobOutcome<R> {
  return { kind: "failed", error: new ApiError(502, code, message) };
}
