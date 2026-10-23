import { boardsApi } from "../../api/boards";
import type { Interpretation } from "../../api/types";
import { JOB_POLL_INTERVAL_MS, pollJob, type JobOutcome } from "../jobPolling";

/**
 * 解釈のジョブが走り終わるまで見に行く（ADR 0085）。見に行き方は図のドラフト
 * 生成と共通で、`pollJob` にある。
 */
export function awaitJob(
  boardId: string,
  jobId: string,
  isCurrent: () => boolean,
  intervalMs: number = JOB_POLL_INTERVAL_MS,
): Promise<JobOutcome<Interpretation> | undefined> {
  return pollJob(
    () => boardsApi.interpretationJob(boardId, jobId),
    isCurrent,
    intervalMs,
  );
}
