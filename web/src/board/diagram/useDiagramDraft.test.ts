import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../../api/boards";
import { diagramNotPlaceableFailure, mermaidPasteFailure } from "../../api/errorMessage";
import type { DiagramDraft } from "../../api/types";
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

function setup() {
  const scrollToContent = vi.fn();
  const api = { scrollToContent } as unknown as ExcalidrawImperativeAPI;
  const updateElements = vi.fn();
  const currentElements = vi.fn(() => existing);
  const hook = renderHook(() =>
    useDiagramDraft({ api, boardId: "b1", currentElements, updateElements }),
  );
  return { hook, scrollToContent, updateElements };
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
    const generate = vi
      .spyOn(boardsApi, "generateDiagram")
      .mockResolvedValueOnce(draft("flowchart TD\n  A"))
      .mockRejectedValueOnce(new ApiError(502, "test_unknown", "boom"));
    const { hook } = setup();

    await act(() => hook.result.current.generate("TODO を"));
    expect(generate).toHaveBeenLastCalledWith("b1", "todo", "TODO を", []);
    expect(hook.result.current.chat.turns).toEqual([
      { prompt: "TODO を", mermaid: "flowchart TD\n  A", internal: false },
    ]);

    await act(() => hook.result.current.generate("もう 1 つ"));
    // 2 回目は成立した往復を添えて送る。
    expect(generate).toHaveBeenLastCalledWith("b1", "todo", "もう 1 つ", [
      { prompt: "TODO を", mermaid: "flowchart TD\n  A" },
    ]);
    expect(hook.result.current.chat.failure).toEqual({
      message: "生成できませんでした: boom",
      detail: "",
    });
    expect(hook.result.current.chat.turns).toHaveLength(1);
  });

  it("生成中に種類を変えたら、遅れて届いた図を積まない", async () => {
    const pending = deferred<DiagramDraft>();
    vi.spyOn(boardsApi, "generateDiagram").mockReturnValue(pending.promise);
    const { hook } = setup();

    let generating!: Promise<boolean>;
    act(() => {
      generating = hook.result.current.generate("TODO を");
    });
    act(() => hook.result.current.changeKind("sequence"));

    await act(async () => {
      pending.resolve(draft("flowchart TD\n  A"));
      await generating;
    });
    expect(hook.result.current.chat).toMatchObject({
      kind: "sequence",
      turns: [],
      pending: null,
      draft: null,
    });
  });

  it("同じ種類を選び直しても、生成中の応答はそのまま積まれる", async () => {
    const pending = deferred<DiagramDraft>();
    vi.spyOn(boardsApi, "generateDiagram").mockReturnValue(pending.promise);
    const { hook } = setup();

    let generating!: Promise<boolean>;
    act(() => {
      generating = hook.result.current.generate("TODO を");
    });
    act(() => hook.result.current.changeKind("todo"));

    await act(async () => {
      pending.resolve(draft("flowchart TD\n  A"));
      await generating;
    });
    expect(hook.result.current.chat.pending).toBeNull();
    expect(hook.result.current.chat.turns).toHaveLength(1);
  });
});

describe("useDiagramDraft の置く", () => {
  it("変換できたら、既存の要素の後ろに足して置き、置いた先へ寄せる", async () => {
    vi.spyOn(boardsApi, "generateDiagram").mockResolvedValue(draft("flowchart TD\n  A"));
    vi.mocked(mermaidToElements).mockResolvedValue({ ok: true, elements: converted });
    vi.mocked(draftOrigin).mockReturnValue({ x: 500, y: 0 });
    vi.mocked(moveDraft).mockReturnValue(placed);
    const { hook, updateElements, scrollToContent } = setup();

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
  });

  it("構文エラーなら、会話の次の 1 往復として内部の印つきで頼み直す", async () => {
    const generate = vi
      .spyOn(boardsApi, "generateDiagram")
      .mockResolvedValueOnce(draft("flowchart TD\n  A -->"))
      .mockResolvedValueOnce(draft("flowchart TD\n  A --> B"));
    vi.mocked(mermaidToElements).mockResolvedValue({
      ok: false,
      reason: "syntax",
      detail: "Parse error on line 2",
    });
    const { hook, updateElements } = setup();

    await act(() => hook.result.current.generate("TODO を"));
    await act(() => hook.result.current.placeDraft());

    expect(generate).toHaveBeenLastCalledWith(
      "b1",
      "todo",
      conversionRetryPrompt("Parse error on line 2"),
      [{ prompt: "TODO を", mermaid: "flowchart TD\n  A -->" }],
    );
    await vi.waitFor(() => expect(hook.result.current.chat.turns).toHaveLength(2));
    expect(hook.result.current.chat.turns[1]?.internal).toBe(true);
    expect(updateElements).not.toHaveBeenCalled();
  });

  it("置けない種類なら、頼み直さずに理由を出す", async () => {
    const generate = vi
      .spyOn(boardsApi, "generateDiagram")
      .mockResolvedValue(draft("mindmap\n  root"));
    vi.mocked(mermaidToElements).mockResolvedValue({
      ok: false,
      reason: "unsupported",
      detail: "image",
    });
    const { hook, updateElements } = setup();

    await act(() => hook.result.current.generate("マインドマップを"));
    await act(() => hook.result.current.placeDraft());

    expect(generate).toHaveBeenCalledTimes(1);
    expect(hook.result.current.chat.failure).toEqual(diagramNotPlaceableFailure());
    expect(updateElements).not.toHaveBeenCalled();
  });

  it("置く・貼るの二重押しは、どちらからでも 1 回しか置かない", async () => {
    vi.spyOn(boardsApi, "generateDiagram").mockResolvedValue(draft("flowchart TD\n  A"));
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
    const { hook, updateElements } = setup();

    act(() => hook.result.current.setPasteText("flowchart TD\n  X"));
    let outcome: unknown;
    await act(async () => {
      outcome = await hook.result.current.pasteMermaid("flowchart TD\n  X");
    });

    expect(outcome).toEqual({ placed: true });
    expect(updateElements).toHaveBeenCalledWith([...existing, ...placed]);
    expect(hook.result.current.pasteText).toBe("");
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
    const { hook, updateElements } = setup();

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
  });
});
