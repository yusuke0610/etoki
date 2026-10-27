import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../../api/boards";
import type {
  AnnotationImage,
  AnnotationStatus,
  InterpretationResult,
  InterpretationJob,
  SyncItem,
} from "../../api/types";
import { exportAnnotationImage } from "../../excalidraw/image";
import { JOB_POLL_MAX_FAILURES } from "../jobPolling";
import { useInterpretations } from "./useInterpretations";

// 画像の書き出しは canvas が要るので jsdom では動かない（`web/CLAUDE.md`）。
// ここで見るのは、書き出した画像がそのまま解釈に渡ることだけ。
vi.mock("../../excalidraw/image", () => ({ exportAnnotationImage: vi.fn() }));

/**
 * 注釈の解釈と、引いた解釈の履歴（#146 で `BoardPage` から切り出し）。
 *
 * **守りたいのは 4 つ。** 保存を挟んだら保存前のシーンに対する解釈を出さないこと
 * （`web/CLAUDE.md`「引いた解釈と実行の履歴」）、引き直しに失敗しても前の結果を
 * 消さないこと、解釈をサーバーのジョブとして走らせ、画面を離れても止めないこと、
 * 開き直したら走っているジョブを画面に戻すこと（ADR 0085）。
 */

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const annotation = (id: string, granularity: AnnotationStatus["granularity"]) =>
  ({ id, name: id, granularity, state: "uncreated" }) satisfies AnnotationStatus;

const result = (summary: string): InterpretationResult => ({
  summary,
  contentHash: "h",
  items: [],
  context: { premise: "", otherItems: [], omittedOtherItems: 0 },
});

const job = (
  id: string,
  status: InterpretationJob["status"],
  rest: Partial<InterpretationJob> = {},
): InterpretationJob => ({
  id,
  annotationId: "a1",
  status,
  startedAt: "2026-10-19T09:00:00Z",
  ...rest,
});

const done = (id: string, summary: string) =>
  job(id, "succeeded", { result: result(summary) });

const image: AnnotationImage = { mediaType: "image/png", data: "AAAA" };
const api = {} as ExcalidrawImperativeAPI;

/** ジョブを見に行く間隔を詰めて、待たずに済ませる。 */
const poll = 1;

/** 副作用のある API を、何もしない応答で埋める。個別のテストが上書きする。 */
function stubApi() {
  return {
    interpret: vi.spyOn(boardsApi, "interpret"),
    getJob: vi.spyOn(boardsApi, "interpretationJob"),
    discard: vi.spyOn(boardsApi, "discardInterpretationJob").mockResolvedValue(),
    list: vi.spyOn(boardsApi, "interpretationJobs").mockResolvedValue([]),
  };
}

function render(
  annotations: AnnotationStatus[] = [annotation("a1", "")],
  options: { api?: ExcalidrawImperativeAPI | null; enabled?: boolean } = {},
) {
  return renderHook(() =>
    useInterpretations({
      api: options.api ?? null,
      boardId: "b1",
      annotations,
      enabled: options.enabled ?? true,
      pollIntervalMs: poll,
    }),
  );
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.mocked(exportAnnotationImage).mockReset();
});

describe("useInterpretations", () => {
  it("解釈すると結果が積まれて選ばれ、実行したときの粒度を控える", async () => {
    vi.mocked(exportAnnotationImage).mockResolvedValue(image);
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(done("j1", "一つ目"));
    const { result: hook } = render([annotation("a1", "epic")], { api });

    await act(() => hook.current.interpret("a1"));

    expect(s.interpret).toHaveBeenCalledWith("b1", "a1", image);
    const state = hook.current.states.a1;
    expect(state?.running).toBe(false);
    expect(state?.runs).toHaveLength(1);
    expect(state?.runs[0]?.granularity).toBe("epic");
    expect(state?.runs[0]?.result).toEqual(result("一つ目"));
    expect(state?.selectedId).toBe(state?.runs[0]?.id);
  });

  it("走っているあいだは見に行き続け、終わったら止める", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob
      .mockResolvedValueOnce(job("j1", "running"))
      .mockResolvedValueOnce(job("j1", "running"))
      .mockResolvedValue(done("j1", "終わった"));
    const { result: hook } = render();

    await act(() => hook.current.interpret("a1"));

    expect(s.getJob).toHaveBeenCalledTimes(3);
    expect(hook.current.states.a1?.runs[0]?.result.summary).toBe("終わった");
  });

  it("見に行く呼び出しが一時的に失敗しても、待つのをやめずに結果を積む", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockRejectedValueOnce(new ApiError(502, "internal", "bad gateway"))
      .mockResolvedValue(done("j1", "取り直せた"));
    const { result: hook } = render();

    await act(() => hook.current.interpret("a1"));

    expect(hook.current.states.a1?.failure).toBeUndefined();
    expect(hook.current.states.a1?.runs[0]?.result.summary).toBe("取り直せた");
  });

  it("一時的な失敗が上限まで続いたら、失敗として出す", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockRejectedValue(new TypeError("Failed to fetch"));
    const { result: hook } = render();

    await act(() => hook.current.interpret("a1"));

    expect(s.getJob).toHaveBeenCalledTimes(JOB_POLL_MAX_FAILURES);
    expect(hook.current.states.a1?.failure?.message).toMatch(/^絵解きできませんでした/);
    expect(hook.current.states.a1?.running).toBe(false);
  });

  it("ジョブが消えていたら（404）、見に行き直さずに失敗として出す", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockRejectedValue(new ApiError(404, "not_found", "not found"));
    const { result: hook } = render();

    await act(() => hook.current.interpret("a1"));

    expect(s.getJob).toHaveBeenCalledTimes(1);
    expect(hook.current.states.a1?.failure?.message).toMatch(/^絵解きできませんでした/);
  });

  it("受け取り終えたジョブは、サーバーに捨てさせる", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(done("j1", "一つ目"));
    const { result: hook } = render();

    await act(() => hook.current.interpret("a1"));

    expect(s.discard).toHaveBeenCalledWith("b1", "j1");
  });

  it("ジョブが失敗したら、リクエストの失敗と同じ文言で出す。受け取り済みとして捨てる", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(
      job("j1", "failed", { error: { code: "llm_unavailable", error: "401" } }),
    );
    const { result: hook } = render();

    await act(() => hook.current.interpret("a1"));

    const failure = hook.current.states.a1?.failure;
    expect(failure?.message).toMatch(/^絵解きできませんでした: /);
    expect(failure?.detail).toBe("401");
    expect(hook.current.states.a1?.running).toBe(false);
    expect(s.discard).toHaveBeenCalledWith("b1", "j1");
  });

  it("始めるリクエストが失敗しても、前の結果は消さない", async () => {
    const s = stubApi();
    s.interpret
      .mockResolvedValueOnce(job("j1", "running"))
      .mockRejectedValueOnce(new ApiError(500, "test_unknown", "boom"));
    s.getJob.mockResolvedValue(done("j1", "一つ目"));
    const { result: hook } = render();

    await act(() => hook.current.interpret("a1"));
    await act(() => hook.current.interpret("a1"));

    expect(s.interpret).toHaveBeenCalledTimes(2);
    const state = hook.current.states.a1;
    expect(state?.failure).toEqual({
      message: "絵解きできませんでした: boom",
      detail: "",
    });
    expect(state?.runs.map((r) => r.result.summary)).toEqual(["一つ目"]);
  });

  it("同じ注釈を引き直したら、先に投げた応答は捨てる", async () => {
    const first = deferred<InterpretationJob>();
    const second = deferred<InterpretationJob>();
    const s = stubApi();
    s.interpret.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    s.getJob.mockImplementation((_b, id) =>
      Promise.resolve(done(id, id === "j-first" ? "先" : "後")),
    );
    const { result: hook } = render();

    let a!: Promise<void>;
    let b!: Promise<void>;
    act(() => {
      a = hook.current.interpret("a1");
      b = hook.current.interpret("a1");
    });
    await act(async () => {
      second.resolve(job("j-second", "running"));
      first.resolve(job("j-first", "running"));
      await Promise.all([a, b]);
    });

    expect(hook.current.states.a1?.runs.map((r) => r.result.summary)).toEqual(["後"]);
    // 古い要求のジョブは捨てない。押し直しでサーバーが同じジョブを返すことがあり、
    // 捨てると新しい要求が見ているジョブまで止まる。
    expect(s.discard).not.toHaveBeenCalledWith("b1", "j-first");
  });

  it("保存を挟んだら、遅れて届いた応答を入れず、全部消え、サーバーのジョブも止める", async () => {
    const pending = deferred<InterpretationJob>();
    const s = stubApi();
    s.interpret
      .mockResolvedValueOnce(job("j1", "running"))
      .mockReturnValueOnce(pending.promise);
    s.getJob.mockResolvedValue(done("j1", "保存前に引いたもの"));
    const { result: hook } = render();

    await act(() => hook.current.interpret("a1"));
    let running!: Promise<void>;
    act(() => {
      running = hook.current.interpret("a1");
    });
    act(() => hook.current.discardAll());
    expect(hook.current.states).toEqual({});

    await act(async () => {
      pending.resolve(job("j2", "running"));
      await running;
    });
    expect(hook.current.states).toEqual({});
    // 始めている最中に保存が挟まったジョブは、返ってきた時点で止める。
    expect(s.discard).toHaveBeenCalledWith("b1", "j2");
  });

  it("保存で捨てるとき、走っているジョブをサーバーでも止める", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(job("j1", "running"));
    const { result: hook } = render();

    act(() => {
      void hook.current.interpret("a1");
    });
    await waitFor(() => expect(s.getJob).toHaveBeenCalled());
    act(() => hook.current.discardAll());

    expect(s.discard).toHaveBeenCalledWith("b1", "j1");
    expect(hook.current.states).toEqual({});
  });

  // 押した人が止められる（#330）。失敗の帯は出さず、前の結果も残す。
  it("取り消すと、サーバーのジョブを止め、見に行くのをやめ、失敗にはしない", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(job("j1", "running"));
    const { result: hook } = render();

    act(() => {
      void hook.current.interpret("a1");
    });
    await waitFor(() => expect(hook.current.states.a1?.jobId).toBe("j1"));
    await act(() => hook.current.cancel("a1"));

    expect(s.discard).toHaveBeenCalledWith("b1", "j1");
    const state = hook.current.states.a1;
    expect(state?.running).toBe(false);
    expect(state?.jobId).toBeUndefined();
    expect(state?.failure).toBeUndefined();

    // 見に行くのもやめる。止めたジョブを見に行き続けると、404 を失敗として出す。
    const calls = s.getJob.mock.calls.length;
    await new Promise((r) => setTimeout(r, 20));
    expect(s.getJob.mock.calls.length).toBeLessThanOrEqual(calls + 1);
    expect(hook.current.states.a1?.failure).toBeUndefined();
  });

  // ジョブが分かる前に取り消せると、押し直したときに同じジョブが返る仕組みと
  // 競合する。始める途中は何もしない。
  it("ジョブが分かる前は、取り消しは何もしない", async () => {
    const s = stubApi();
    const started = deferred<InterpretationJob>();
    s.interpret.mockReturnValue(started.promise);
    s.getJob.mockResolvedValue(done("j1", "一つ目"));
    const { result: hook } = render();

    let pending!: Promise<void>;
    act(() => {
      pending = hook.current.interpret("a1");
    });
    await act(() => hook.current.cancel("a1"));
    expect(s.discard).not.toHaveBeenCalled();
    expect(hook.current.states.a1?.running).toBe(true);

    started.resolve(job("j1", "running"));
    await act(() => pending);
    expect(hook.current.states.a1?.runs[0]?.result.summary).toBe("一つ目");
  });

  it("取り消したあとに押し直せば、新しく絵解く", async () => {
    const s = stubApi();
    s.interpret
      .mockResolvedValueOnce(job("j1", "running"))
      .mockResolvedValueOnce(job("j2", "running"));
    s.getJob.mockImplementation((_b, id) =>
      Promise.resolve(id === "j1" ? job("j1", "running") : done("j2", "やり直した")),
    );
    const { result: hook } = render();

    act(() => {
      void hook.current.interpret("a1");
    });
    await waitFor(() => expect(hook.current.states.a1?.jobId).toBe("j1"));
    await act(() => hook.current.cancel("a1"));

    await act(() => hook.current.interpret("a1"));
    expect(hook.current.states.a1?.runs[0]?.result.summary).toBe("やり直した");
    expect(hook.current.states.a1?.failure).toBeUndefined();
  });

  it("開き直して戻した、走っているジョブも取り消せる", async () => {
    const s = stubApi();
    s.list.mockResolvedValue([job("j1", "running")]);
    s.getJob.mockResolvedValue(job("j1", "running"));
    const { result: hook } = render();

    await waitFor(() => expect(hook.current.states.a1?.jobId).toBe("j1"));
    await act(() => hook.current.cancel("a1"));

    expect(s.discard).toHaveBeenCalledWith("b1", "j1");
    expect(hook.current.states.a1?.running).toBe(false);
  });

  // 取り消しを送っても、サーバーが受けたとは限らない（#330 のレビュー）。受けて
  // いないのに画面だけ止めると、ジョブは走り続けて枠を握り、開き直すと戻ってくる。
  it("取り消しが失敗したら、絵解き中のまま見に行き続け、取り消せなかったと出す", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(job("j1", "running"));
    s.discard.mockRejectedValueOnce(new ApiError(500, "internal", "boom"));
    const { result: hook } = render();

    act(() => {
      void hook.current.interpret("a1");
    });
    await waitFor(() => expect(hook.current.states.a1?.jobId).toBe("j1"));
    await act(() => hook.current.cancel("a1"));

    const state = hook.current.states.a1;
    expect(state?.running).toBe(true);
    expect(state?.jobId).toBe("j1");
    expect(state?.cancelling).toBe(false);
    expect(state?.failure?.message).toMatch(/^絵解きを取り消せませんでした/);

    // 見に行き続ける。押し直せば、もう一度取り消せる。
    const calls = s.getJob.mock.calls.length;
    await waitFor(() => expect(s.getJob.mock.calls.length).toBeGreaterThan(calls));
    await act(() => hook.current.cancel("a1"));
    expect(hook.current.states.a1?.running).toBe(false);
  });

  // 消えていた（受け取り済みや期限切れ）なら、サーバーにはもう無い。取り消せている。
  it("取り消しが 404 なら、取り消せたものとして扱う", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(job("j1", "running"));
    s.discard.mockRejectedValueOnce(new ApiError(404, "not_found", "gone"));
    const { result: hook } = render();

    act(() => {
      void hook.current.interpret("a1");
    });
    await waitFor(() => expect(hook.current.states.a1?.jobId).toBe("j1"));
    await act(() => hook.current.cancel("a1"));

    expect(hook.current.states.a1?.running).toBe(false);
    expect(hook.current.states.a1?.failure).toBeUndefined();
  });

  it("取り消しの返事を待つあいだは、取り消し中にする", async () => {
    const s = stubApi();
    const discarded = deferred<void>();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(job("j1", "running"));
    s.discard.mockReturnValueOnce(discarded.promise);
    const { result: hook } = render();

    act(() => {
      void hook.current.interpret("a1");
    });
    await waitFor(() => expect(hook.current.states.a1?.jobId).toBe("j1"));
    let pending!: Promise<void>;
    act(() => {
      pending = hook.current.cancel("a1");
    });
    expect(hook.current.states.a1?.cancelling).toBe(true);
    expect(hook.current.states.a1?.running).toBe(true);

    discarded.resolve();
    await act(() => pending);
    expect(hook.current.states.a1?.running).toBe(false);
    expect(hook.current.states.a1?.cancelling).toBe(false);
  });

  // サーバーは取り消しを受けた時点でジョブを消すので、返事より先に、見に行った
  // ほうが 404 を受けうる（#333 のレビュー）。それを失敗として出すと、取り消した
  // ものが「絵解きできませんでした」になる。
  it("取り消しの返事を待つあいだに見に行って 404 が返っても、失敗にしない", async () => {
    const s = stubApi();
    const discarded = deferred<void>();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(job("j1", "running"));
    s.discard.mockReturnValueOnce(discarded.promise);
    const { result: hook } = render();

    act(() => {
      void hook.current.interpret("a1");
    });
    await waitFor(() => expect(hook.current.states.a1?.jobId).toBe("j1"));
    let pending!: Promise<void>;
    act(() => {
      pending = hook.current.cancel("a1");
    });
    s.getJob.mockRejectedValue(new ApiError(404, "not_found", "gone"));
    const calls = s.getJob.mock.calls.length;
    await waitFor(() => expect(s.getJob.mock.calls.length).toBeGreaterThan(calls));
    // 返事が来るまでは、取り消し中のまま待つ。
    expect(hook.current.states.a1?.failure).toBeUndefined();
    expect(hook.current.states.a1?.cancelling).toBe(true);

    discarded.resolve();
    await act(() => pending);
    const state = hook.current.states.a1;
    expect(state?.running).toBe(false);
    expect(state?.failure).toBeUndefined();
  });

  // 取り消すは「要らない」の意味なので、返事を待つあいだに終わった結果も積まない。
  it("取り消しの返事を待つあいだに終わっても、取り消せたら結果を積まない", async () => {
    const s = stubApi();
    const discarded = deferred<void>();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(job("j1", "running"));
    s.discard.mockReturnValueOnce(discarded.promise);
    const { result: hook } = render();

    act(() => {
      void hook.current.interpret("a1");
    });
    await waitFor(() => expect(hook.current.states.a1?.jobId).toBe("j1"));
    let pending!: Promise<void>;
    act(() => {
      pending = hook.current.cancel("a1");
    });
    s.getJob.mockResolvedValue(done("j1", "間に合った"));
    const calls = s.getJob.mock.calls.length;
    await waitFor(() => expect(s.getJob.mock.calls.length).toBeGreaterThan(calls));
    expect(hook.current.states.a1?.runs).toEqual([]);

    discarded.resolve();
    await act(() => pending);
    expect(hook.current.states.a1?.running).toBe(false);
    expect(hook.current.states.a1?.runs).toEqual([]);
  });

  // 取り消せなかったなら、ジョブは止まっていない。届いた結果はそのまま受け取る。
  it("取り消しが失敗したら、返事を待つあいだに終わった結果を積む", async () => {
    const s = stubApi();
    let failDiscard!: (e: unknown) => void;
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(job("j1", "running"));
    s.discard.mockReturnValueOnce(
      new Promise<void>((_, reject) => {
        failDiscard = reject;
      }),
    );
    const { result: hook } = render();

    act(() => {
      void hook.current.interpret("a1");
    });
    await waitFor(() => expect(hook.current.states.a1?.jobId).toBe("j1"));
    let pending!: Promise<void>;
    act(() => {
      pending = hook.current.cancel("a1");
    });
    s.getJob.mockResolvedValue(done("j1", "止まらなかった"));
    const calls = s.getJob.mock.calls.length;
    await waitFor(() => expect(s.getJob.mock.calls.length).toBeGreaterThan(calls));

    failDiscard(new ApiError(500, "internal", "boom"));
    await act(() => pending);
    await waitFor(() =>
      expect(hook.current.states.a1?.runs[0]?.result.summary).toBe("止まらなかった"),
    );
    expect(hook.current.states.a1?.running).toBe(false);
  });

  it("画面を離れても、ジョブは止めない。見に行くのをやめるだけ", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(job("j1", "running"));
    const { result: hook, unmount } = render();

    act(() => {
      void hook.current.interpret("a1");
    });
    await waitFor(() => expect(s.getJob).toHaveBeenCalled());
    unmount();
    const calls = s.getJob.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 20));

    expect(s.getJob.mock.calls.length).toBeLessThanOrEqual(calls + 1);
    expect(s.discard).not.toHaveBeenCalled();
  });

  it("開き直したら、走っているジョブを画面に戻し、終わったら結果を積む", async () => {
    const s = stubApi();
    const finish = deferred<InterpretationJob>();
    s.list.mockResolvedValue([job("j1", "running")]);
    s.getJob.mockReturnValue(finish.promise);
    const { result: hook } = render([annotation("a1", "issue")]);

    await waitFor(() => expect(hook.current.states.a1?.running).toBe(true));
    finish.resolve(done("j1", "戻ってきた"));
    await waitFor(() => expect(hook.current.states.a1?.runs).toHaveLength(1));

    expect(hook.current.states.a1?.running).toBe(false);
    expect(hook.current.states.a1?.runs[0]?.result.summary).toBe("戻ってきた");
    expect(hook.current.states.a1?.runs[0]?.granularity).toBe("issue");
    expect(s.interpret).not.toHaveBeenCalled();
  });

  it("開き直したとき、終わっていて受け取っていない結果も積む", async () => {
    const s = stubApi();
    s.list.mockResolvedValue([job("j1", "succeeded")]);
    s.getJob.mockResolvedValue(done("j1", "離れているあいだに終わった"));
    const { result: hook } = render();

    await waitFor(() => expect(hook.current.states.a1?.runs).toHaveLength(1));
    expect(s.discard).toHaveBeenCalledWith("b1", "j1");
  });

  it("使えない構成では、開いてもジョブを引かない", async () => {
    const s = stubApi();
    render([annotation("a1", "")], { enabled: false });
    await new Promise((resolve) => setTimeout(resolve, 10));

    expect(s.list).not.toHaveBeenCalled();
  });

  it("ジョブの一覧が引けなくても、解釈は始められる", async () => {
    const s = stubApi();
    s.list.mockRejectedValue(new ApiError(503, "llm_not_configured", "x"));
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(done("j1", "一つ目"));
    const { result: hook } = render();

    await act(() => hook.current.interpret("a1"));

    expect(hook.current.states.a1?.runs).toHaveLength(1);
  });

  it("作ったものを、その解釈に結びつける。解釈の無い注釈には何もしない", async () => {
    const s = stubApi();
    s.interpret.mockResolvedValue(job("j1", "running"));
    s.getJob.mockResolvedValue(done("j1", "一つ目"));
    const { result: hook } = render();
    await act(() => hook.current.interpret("a1"));
    const runId = hook.current.states.a1?.runs[0]?.id ?? -1;
    const items: SyncItem[] = [
      {
        itemId: "I_1",
        kind: "issue",
        title: "t",
        body: "",
        localId: "l1",
        action: "created",
        confirmed: true,
      },
    ];

    act(() => hook.current.recordCreated("a1", runId, items));
    act(() => hook.current.recordCreated("a2", 1, items));

    expect(hook.current.states.a1?.runs[0]?.created).toEqual([items]);
    expect(hook.current.states).not.toHaveProperty("a2");
  });

  it("見る解釈の選び直しは画面の中だけで、API を呼ばない", async () => {
    const s = stubApi();
    s.interpret
      .mockResolvedValueOnce(job("j1", "running"))
      .mockResolvedValueOnce(job("j2", "running"));
    s.getJob.mockImplementation((_b, id) =>
      Promise.resolve(done(id, id === "j1" ? "一つ目" : "二つ目")),
    );
    const { result: hook } = render();
    await act(() => hook.current.interpret("a1"));
    await act(() => hook.current.interpret("a1"));
    const older = hook.current.states.a1?.runs.find((r) => r.result.summary === "一つ目");
    const calls = s.getJob.mock.calls.length;

    act(() => hook.current.select("a1", older?.id ?? -1));

    expect(hook.current.states.a1?.selectedId).toBe(older?.id);
    expect(s.interpret).toHaveBeenCalledTimes(2);
    expect(s.getJob.mock.calls.length).toBe(calls);
  });
});
