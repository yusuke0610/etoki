import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../../api/boards";
import {
  describeFailure,
  diagramNotPlaceableFailure,
  mermaidPasteFailure,
} from "../../api/errorMessage";
import type {
  DiagramDraft,
  DiagramJob,
  ErrorResponse,
  GenerateDiagramRequest,
} from "../../api/types";
import { log } from "../../app/logger";
import type { SceneElement } from "../../excalidraw/annotation";
import { draftOrigin, mermaidToElements, moveDraft } from "../../excalidraw/mermaid";
import { pasteToElements } from "../../excalidraw/mermaidPaste";
import { conversionRetryPrompt } from "./diagramChat";
import { useDiagramDraft } from "./useDiagramDraft";

// mermaid の変換は jsdom では本物を通せない（ADR 0040）。ここで見るのは、変換の
// 結果に応じて置く・頼み直す・理由を返すの分かれ方と、置き方だけ。
vi.mock("../../excalidraw/mermaid", () => ({
  mermaidToElements: vi.fn(),
  draftOrigin: vi.fn(),
  moveDraft: vi.fn(),
}));
vi.mock("../../excalidraw/mermaidPaste", () => ({ pasteToElements: vi.fn() }));

/**
 * 図のドラフト（LLM に作らせる・mermaid を貼る・置く）。#146 で `BoardPage` から
 * 切り出し。
 *
 * **守りたいのは、既存の絵に触らないこと**（ADR 0041）と、種類を変えたあとに
 * 前の記法の図を会話に積まないこと。置く操作は図のドラフトと貼り付けで 1 つの
 * 守りを共有する（同じ `draftOrigin` を読むので、分けると同じ場所に重なる）。
 */

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const el = (id: string) => ({ id }) as unknown as SceneElement;
const existing = [el("hand-drawn")];
const converted = [el("c1"), el("c2")];
const placed = [el("p1"), el("p2")];

const draft = (mermaid: string): DiagramDraft => ({
  kind: "todo",
  mermaid,
  turnsRemaining: 9,
});

/** ジョブの終わり方。無ければ走ったまま。 */
type Outcome = { mermaid: string } | { error: ErrorResponse };

/**
 * 生成のジョブを真似る（ADR 0091）。始めたジョブには、`outcomes` を始めた順に
 * 割り当てる。見に行くと、割り当てた終わり方で終わっている。
 *
 * **捨てたジョブは消す。** 実物と同じく、捨てたあとに見に行くと 404。
 */
function fakeServer(outcomes: (Outcome | undefined)[] = []) {
  const queue = [...outcomes];
  const jobs = new Map<string, { job: DiagramJob; outcome: Outcome | undefined }>();
  let seq = 0;

  const toJob = (job: DiagramJob, outcome: Outcome | undefined): DiagramJob => {
    if (outcome === undefined) return job;
    if ("error" in outcome) return { ...job, status: "failed", error: outcome.error };
    return { ...job, status: "succeeded", result: draft(outcome.mermaid) };
  };

  const start = vi
    .spyOn(boardsApi, "generateDiagram")
    .mockImplementation(async (_boardId: string, request: GenerateDiagramRequest) => {
      seq += 1;
      const job: DiagramJob = {
        id: `job-${seq}`,
        status: "running",
        startedAt: "2026-10-22T09:00:00Z",
        request,
      };
      jobs.set(job.id, { job, outcome: queue.shift() });
      return job;
    });
  const get = vi
    .spyOn(boardsApi, "diagramJob")
    .mockImplementation(async (_boardId: string, id: string) => {
      const entry = jobs.get(id);
      if (!entry) throw new ApiError(404, "not_found", "not found");
      return toJob(entry.job, entry.outcome);
    });
  const discard = vi
    .spyOn(boardsApi, "discardDiagramJob")
    .mockImplementation(async (_boardId: string, id: string) => {
      jobs.delete(id);
    });
  const list = vi.spyOn(boardsApi, "diagramJobs").mockResolvedValue([]);

  return {
    start,
    get,
    discard,
    list,
    /** 走っているジョブを終わらせる。 */
    finish(id: string, outcome: Outcome) {
      const entry = jobs.get(id);
      if (entry) jobs.set(id, { ...entry, outcome });
    },
    /** 開いたときに引く一覧に、ジョブを置いておく。 */
    seed(job: DiagramJob, outcome?: Outcome) {
      jobs.set(job.id, { job, outcome });
      list.mockResolvedValue([...jobs.values()].map((e) => ({ ...e.job })));
    },
  };
}

function setup({ enabled = true }: { enabled?: boolean } = {}) {
  const scrollToContent = vi.fn();
  const api = { scrollToContent } as unknown as ExcalidrawImperativeAPI;
  const updateElements = vi.fn();
  const currentElements = vi.fn(() => existing);
  const onPlaced = vi.fn();
  const hook = renderHook(() =>
    useDiagramDraft({
      api,
      boardId: "b1",
      currentElements,
      updateElements,
      onPlaced,
      enabled,
      pollIntervalMs: 0,
    }),
  );
  return { hook, scrollToContent, updateElements, onPlaced };
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.mocked(mermaidToElements).mockReset();
  vi.mocked(pasteToElements).mockReset();
  vi.mocked(draftOrigin).mockReset();
  vi.mocked(moveDraft).mockReset();
});

describe("useDiagramDraft の生成", () => {
  it("成功したら往復が積まれ、失敗は会話の中に出る", async () => {
    const server = fakeServer([
      { mermaid: "flowchart TD\n  A" },
      { error: { code: "llm_unavailable", error: "boom" } },
    ]);
    const { hook } = setup();

    await act(() => hook.result.current.generate("TODO を"));
    expect(server.start).toHaveBeenLastCalledWith("b1", {
      kind: "todo",
      prompt: "TODO を",
      history: [],
      internal: false,
    });
    expect(hook.result.current.chat.turns).toEqual([
      { prompt: "TODO を", mermaid: "flowchart TD\n  A", internal: false },
    ]);

    await act(() => hook.result.current.generate("もう 1 つ"));
    // 2 回目は成立した往復を添えて送る。
    expect(server.start).toHaveBeenLastCalledWith("b1", {
      kind: "todo",
      prompt: "もう 1 つ",
      history: [{ prompt: "TODO を", mermaid: "flowchart TD\n  A", internal: false }],
      internal: false,
    });
    // ジョブの失敗も、応答の失敗と同じ表で文言を引く。
    expect(hook.result.current.chat.failure).toEqual(
      describeFailure(
        "生成できませんでした",
        new ApiError(502, "llm_unavailable", "boom"),
      ),
    );
    expect(hook.result.current.chat.turns).toHaveLength(1);
  });

  it("始める時点で断られたら、ジョブを見に行かずに会話の中に出す", async () => {
    const server = fakeServer();
    server.start.mockRejectedValueOnce(new ApiError(429, "test_unknown", "busy"));
    const { hook } = setup();

    await act(() => hook.result.current.generate("TODO を"));

    expect(server.get).not.toHaveBeenCalled();
    expect(hook.result.current.chat.pending).toBeNull();
    expect(hook.result.current.chat.failure).toEqual({
      message: "生成できませんでした: busy",
      detail: "",
    });
  });

  // 受け取り終えたジョブを残すと、サーバーのメモリに溜まり、開き直したときに
  // 同じ結果がもう一度出る。
  it("受け取ったら、成功でも失敗でもジョブを捨てる", async () => {
    const server = fakeServer([
      { mermaid: "flowchart TD\n  A" },
      { error: { code: "diagram_failed", error: "no diagram" } },
    ]);
    const { hook } = setup();

    await act(() => hook.result.current.generate("TODO を"));
    await act(() => hook.result.current.generate("もう 1 つ"));

    expect(server.discard.mock.calls).toEqual([
      ["b1", "job-1"],
      ["b1", "job-2"],
    ]);
  });

  it("生成できたら入力欄を消し、できなかったら残す", async () => {
    fakeServer([
      { mermaid: "flowchart TD\n  A" },
      { error: { code: "diagram_failed", error: "no diagram" } },
    ]);
    const { hook } = setup();

    act(() => hook.result.current.setPrompt("TODO を"));
    await act(() => hook.result.current.generate("TODO を"));
    expect(hook.result.current.prompt).toBe("");

    act(() => hook.result.current.setPrompt("もう 1 つ"));
    await act(() => hook.result.current.generate("もう 1 つ"));
    // 失敗したぶんは残して、直して送り直せるようにする。
    expect(hook.result.current.prompt).toBe("もう 1 つ");
  });

  it("始めているあいだに種類を変えたら、遅れて届いたジョブを受け取らずに捨てる", async () => {
    const server = fakeServer([{ mermaid: "flowchart TD\n  A" }]);
    const starting = deferred<DiagramJob>();
    server.start.mockReturnValueOnce(starting.promise);
    const { hook } = setup();

    let generating!: Promise<boolean>;
    act(() => {
      generating = hook.result.current.generate("TODO を");
    });
    act(() => hook.result.current.changeKind("sequence"));

    await act(async () => {
      starting.resolve({
        id: "job-late",
        status: "running",
        startedAt: "2026-10-22T09:00:00Z",
        request: { kind: "todo", prompt: "TODO を" },
      });
      await generating;
    });
    expect(hook.result.current.chat).toMatchObject({
      kind: "sequence",
      turns: [],
      pending: null,
      draft: null,
    });
    // 捨てた会話の生成が枠を握り続けないよう、サーバーでも止める。
    expect(server.discard).toHaveBeenCalledWith("b1", "job-late");
    expect(server.get).not.toHaveBeenCalled();
  });

  it("走っているあいだに種類を変えたら、結果を積まずにサーバーのジョブを止める", async () => {
    const server = fakeServer([undefined]);
    const { hook } = setup();

    let generating!: Promise<boolean>;
    act(() => {
      generating = hook.result.current.generate("TODO を");
    });
    await vi.waitFor(() => expect(server.get).toHaveBeenCalled());
    act(() => hook.result.current.changeKind("sequence"));
    server.finish("job-1", { mermaid: "flowchart TD\n  A" });

    await act(async () => {
      await generating;
    });
    expect(server.discard).toHaveBeenCalledWith("b1", "job-1");
    expect(hook.result.current.chat).toMatchObject({ kind: "sequence", turns: [] });
  });

  it("同じ種類を選び直しても、生成中の応答はそのまま積まれる", async () => {
    const server = fakeServer([undefined]);
    const { hook } = setup();

    let generating!: Promise<boolean>;
    act(() => {
      generating = hook.result.current.generate("TODO を");
    });
    await vi.waitFor(() => expect(server.get).toHaveBeenCalled());
    act(() => hook.result.current.changeKind("todo"));
    server.finish("job-1", { mermaid: "flowchart TD\n  A" });

    await act(async () => {
      await generating;
    });
    expect(hook.result.current.chat.pending).toBeNull();
    expect(hook.result.current.chat.turns).toHaveLength(1);
    expect(server.discard).toHaveBeenCalledTimes(1);
  });

  // 画面を離れても生成は止めない（#329）。止めるのは、種類を変えて会話ごと
  // 捨てたときだけ。
  it("画面を離れたら見に行くのをやめるが、ジョブは捨てない", async () => {
    const server = fakeServer([undefined]);
    const { hook } = setup();

    let generating!: Promise<boolean>;
    act(() => {
      generating = hook.result.current.generate("TODO を");
    });
    await vi.waitFor(() => expect(server.get).toHaveBeenCalled());
    hook.unmount();

    await expect(generating).resolves.toBe(false);
    expect(server.discard).not.toHaveBeenCalled();
  });
});

// 開き直した画面は、受け取っていない生成を会話ごと戻す（ADR 0091）。
describe("useDiagramDraft の開き直し", () => {
  const running = (request: GenerateDiagramRequest, id = "job-9"): DiagramJob => ({
    id,
    status: "running",
    startedAt: "2026-10-22T09:00:00Z",
    request,
  });
  const request: GenerateDiagramRequest = {
    kind: "sequence",
    prompt: "返金も足して",
    history: [
      { prompt: "注文の流れ", mermaid: "sequenceDiagram\n  A->>B: 注文" },
      { prompt: "投げ直し", mermaid: "sequenceDiagram\n  A->>B: 注文", internal: true },
    ],
  };

  it("走っている生成を、往復と送った指示ごと戻し、終わったら積む", async () => {
    const server = fakeServer();
    server.seed(running(request));
    const { hook } = setup();

    await vi.waitFor(() => expect(hook.result.current.chat.pending).not.toBeNull());
    expect(hook.result.current.chat.kind).toBe("sequence");
    expect(hook.result.current.chat.turns).toEqual([
      {
        prompt: "注文の流れ",
        mermaid: "sequenceDiagram\n  A->>B: 注文",
        internal: false,
      },
      { prompt: "投げ直し", mermaid: "sequenceDiagram\n  A->>B: 注文", internal: true },
    ]);
    // 送っている最中の画面と同じく、送った指示が入力欄に残っている。
    expect(hook.result.current.prompt).toBe("返金も足して");

    server.finish("job-9", { mermaid: "sequenceDiagram\n  A->>B: 返金" });
    await vi.waitFor(() => expect(hook.result.current.chat.pending).toBeNull());
    expect(hook.result.current.chat.turns).toHaveLength(3);
    expect(hook.result.current.chat.turns[2]).toEqual({
      prompt: "返金も足して",
      mermaid: "sequenceDiagram\n  A->>B: 返金",
      internal: false,
    });
    expect(hook.result.current.chat.draft?.mermaid).toBe(
      "sequenceDiagram\n  A->>B: 返金",
    );
    expect(hook.result.current.prompt).toBe("");
    expect(server.discard).toHaveBeenCalledWith("b1", "job-9");
  });

  it("終わっていた失敗も、どの指示への失敗かと一緒に戻す", async () => {
    const server = fakeServer();
    server.seed(running(request), { error: { code: "llm_unavailable", error: "boom" } });
    const { hook } = setup();

    await vi.waitFor(() => expect(hook.result.current.chat.failure).not.toBeNull());
    expect(hook.result.current.chat.failure).toEqual(
      describeFailure(
        "生成できませんでした",
        new ApiError(502, "llm_unavailable", "boom"),
      ),
    );
    expect(hook.result.current.chat.turns).toHaveLength(2);
    // 失敗したぶんは入力欄に残して、送り直せるようにする。
    expect(hook.result.current.prompt).toBe("返金も足して");
  });

  it("投げ直しの生成なら、入力欄には戻さない", async () => {
    const server = fakeServer();
    server.seed(running({ ...request, prompt: "直して", internal: true }));
    const { hook } = setup();

    await vi.waitFor(() => expect(hook.result.current.chat.pending).not.toBeNull());
    expect(hook.result.current.chat.pending).toEqual({
      prompt: "直して",
      internal: true,
    });
    expect(hook.result.current.prompt).toBe("");
  });

  // 会話は 1 本。捨て損ねた古いジョブまで戻すと、どれが今の会話か決まらない。
  it("いちばん新しいものだけを戻し、ほかは捨てる", async () => {
    const server = fakeServer();
    server.seed(running({ kind: "todo", prompt: "古い指示" }, "job-old"));
    server.seed(running(request, "job-new"));
    const { hook } = setup();

    await vi.waitFor(() => expect(hook.result.current.chat.pending).not.toBeNull());
    expect(hook.result.current.chat.pending?.prompt).toBe("返金も足して");
    expect(server.discard).toHaveBeenCalledWith("b1", "job-old");
    expect(server.discard).not.toHaveBeenCalledWith("b1", "job-new");
  });

  it("引いているあいだに送ったら、引いたジョブで会話を上書きしない", async () => {
    const server = fakeServer([undefined]);
    const listing = deferred<DiagramJob[]>();
    server.list.mockReturnValueOnce(listing.promise);
    const { hook } = setup();

    act(() => {
      void hook.result.current.generate("TODO を");
    });
    await act(async () => {
      listing.resolve([running(request)]);
      await listing.promise;
    });

    expect(hook.result.current.chat.kind).toBe("todo");
    expect(hook.result.current.chat.pending?.prompt).toBe("TODO を");
  });

  it("使えないうちは引かない", () => {
    const server = fakeServer();
    setup({ enabled: false });

    expect(server.list).not.toHaveBeenCalled();
  });
});

describe("useDiagramDraft の置く", () => {
  it("変換できたら、既存の要素の後ろに足して置き、置いた先へ寄せる", async () => {
    fakeServer([{ mermaid: "flowchart TD\n  A" }]);
    vi.mocked(mermaidToElements).mockResolvedValue({ ok: true, elements: converted });
    vi.mocked(draftOrigin).mockReturnValue({ x: 500, y: 0 });
    vi.mocked(moveDraft).mockReturnValue(placed);
    const { hook, updateElements, scrollToContent, onPlaced } = setup();

    await act(() => hook.result.current.generate("TODO を"));
    await act(() => hook.result.current.placeDraft());

    expect(mermaidToElements).toHaveBeenCalledWith("flowchart TD\n  A");
    expect(draftOrigin).toHaveBeenCalledWith(existing);
    expect(moveDraft).toHaveBeenCalledWith(converted, { x: 500, y: 0 });
    // 既存の要素はそのまま先頭に残す。
    expect(updateElements).toHaveBeenCalledWith([...existing, ...placed]);
    expect(scrollToContent).toHaveBeenCalledWith(placed, {
      fitToContent: true,
      animate: true,
    });
    // 置けたことを知らせる（スマホではパネルを閉じて置いた図を見せる、#199）。
    expect(onPlaced).toHaveBeenCalledTimes(1);
  });

  it("構文エラーなら、会話の次の 1 往復として内部の印つきで頼み直す", async () => {
    const server = fakeServer([
      { mermaid: "flowchart TD\n  A -->" },
      { mermaid: "flowchart TD\n  A --> B" },
    ]);
    vi.mocked(mermaidToElements).mockResolvedValue({
      ok: false,
      reason: "syntax",
      detail: "Parse error on line 2",
    });
    const { hook, updateElements, onPlaced } = setup();

    act(() => hook.result.current.setPrompt("TODO を"));
    await act(() => hook.result.current.generate("TODO を"));
    // 置く前に、次の指示を書きかけている。
    act(() => hook.result.current.setPrompt("次は返金も"));
    await act(() => hook.result.current.placeDraft());

    expect(server.start).toHaveBeenLastCalledWith("b1", {
      kind: "todo",
      prompt: conversionRetryPrompt("Parse error on line 2"),
      history: [{ prompt: "TODO を", mermaid: "flowchart TD\n  A -->", internal: false }],
      internal: true,
    });
    await vi.waitFor(() => expect(hook.result.current.chat.turns).toHaveLength(2));
    expect(hook.result.current.chat.turns[1]?.internal).toBe(true);
    // 投げ直しは利用者の入力ではない。書きかけの指示は消さない。
    expect(hook.result.current.prompt).toBe("次は返金も");
    expect(updateElements).not.toHaveBeenCalled();
    expect(onPlaced).not.toHaveBeenCalled();
  });

  it("置けない種類なら、頼み直さずに理由を出す", async () => {
    const server = fakeServer([{ mermaid: "mindmap\n  root" }]);
    vi.mocked(mermaidToElements).mockResolvedValue({
      ok: false,
      reason: "unsupported",
      detail: "image",
    });
    const { hook, updateElements, onPlaced } = setup();

    await act(() => hook.result.current.generate("マインドマップを"));
    await act(() => hook.result.current.placeDraft());

    expect(server.start).toHaveBeenCalledTimes(1);
    expect(hook.result.current.chat.failure).toEqual(diagramNotPlaceableFailure());
    expect(updateElements).not.toHaveBeenCalled();
    // 置けなかったらパネルを閉じさせない。出した理由を読ませる。
    expect(onPlaced).not.toHaveBeenCalled();
  });

  it("置く・貼るの二重押しは、どちらからでも 1 回しか置かない", async () => {
    fakeServer([{ mermaid: "flowchart TD\n  A" }]);
    const converting = deferred<{ ok: true; elements: SceneElement[] }>();
    vi.mocked(mermaidToElements).mockReturnValue(converting.promise);
    vi.mocked(pasteToElements).mockResolvedValue({ ok: true, elements: converted });
    vi.mocked(moveDraft).mockReturnValue(placed);
    const { hook, updateElements } = setup();
    await act(() => hook.result.current.generate("TODO を"));

    let placing!: Promise<void>;
    let pasted!: Promise<unknown>;
    act(() => {
      placing = hook.result.current.placeDraft();
      void hook.result.current.placeDraft();
      pasted = hook.result.current.pasteMermaid("flowchart TD\n  X");
    });
    await expect(pasted).resolves.toEqual({ placed: false, failure: null });
    expect(pasteToElements).not.toHaveBeenCalled();

    await act(async () => {
      converting.resolve({ ok: true, elements: converted });
      await placing;
    });
    expect(mermaidToElements).toHaveBeenCalledTimes(1);
    expect(updateElements).toHaveBeenCalledTimes(1);
  });
});

describe("useDiagramDraft の貼り付け", () => {
  it("置けたら入力を消す", async () => {
    vi.mocked(pasteToElements).mockResolvedValue({ ok: true, elements: converted });
    vi.mocked(moveDraft).mockReturnValue(placed);
    const { hook, updateElements, onPlaced } = setup();

    act(() => hook.result.current.setPasteText("flowchart TD\n  X"));
    let outcome: unknown;
    await act(async () => {
      outcome = await hook.result.current.pasteMermaid("flowchart TD\n  X");
    });

    expect(outcome).toEqual({ placed: true });
    expect(updateElements).toHaveBeenCalledWith([...existing, ...placed]);
    expect(hook.result.current.pasteText).toBe("");
    expect(onPlaced).toHaveBeenCalledTimes(1);
  });

  it("変換を待つあいだに書き換えられた入力は消さない", async () => {
    const converting = deferred<{ ok: true; elements: SceneElement[] }>();
    vi.mocked(pasteToElements).mockReturnValue(converting.promise);
    vi.mocked(moveDraft).mockReturnValue(placed);
    const { hook } = setup();

    act(() => hook.result.current.setPasteText("flowchart TD\n  X"));
    let pasting!: Promise<unknown>;
    act(() => {
      pasting = hook.result.current.pasteMermaid("flowchart TD\n  X");
    });
    act(() => hook.result.current.setPasteText("flowchart TD\n  Y"));
    await act(async () => {
      converting.resolve({ ok: true, elements: converted });
      await pasting;
    });

    expect(hook.result.current.pasteText).toBe("flowchart TD\n  Y");
  });

  it("置けなかったら入力を残し、理由を返す", async () => {
    vi.spyOn(log, "warn").mockImplementation(() => {});
    vi.mocked(pasteToElements).mockResolvedValue({
      ok: false,
      reason: "syntax",
      detail: "Parse error on line 1",
    });
    const { hook, updateElements, onPlaced } = setup();

    act(() => hook.result.current.setPasteText("flowchart TD\n  X -->"));
    let outcome: unknown;
    await act(async () => {
      outcome = await hook.result.current.pasteMermaid("flowchart TD\n  X -->");
    });

    expect(outcome).toEqual({
      placed: false,
      failure: mermaidPasteFailure("syntax", "Parse error on line 1"),
    });
    expect(hook.result.current.pasteText).toBe("flowchart TD\n  X -->");
    expect(updateElements).not.toHaveBeenCalled();
    expect(onPlaced).not.toHaveBeenCalled();
  });
});
