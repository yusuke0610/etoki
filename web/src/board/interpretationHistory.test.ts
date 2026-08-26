import { describe, expect, it } from "vitest";

import type { Failure } from "../api/errorMessage";
import type { Granularity, Interpretation } from "../api/types";
import { createGenerations } from "./generation";
import {
  addInterpretation,
  emptyInterpretation,
  failInterpretation,
  interpretationOrderLabel,
  MAX_INTERPRETATIONS,
  selectedInterpretation,
  selectInterpretation,
  startInterpretation,
  type InterpretationRun,
} from "./interpretationHistory";

function run(id: number, granularity: Granularity = ""): InterpretationRun {
  return {
    id,
    at: `2026-08-26T0${id}:00:00.000Z`,
    granularity,
    result: interpretation(`要約 ${id}`),
  };
}

function interpretation(summary: string): Interpretation {
  return { summary, contentHash: "h1", items: [] };
}

const failure: Failure = { message: "解釈できませんでした", detail: "" };

describe("解釈の履歴", () => {
  it("解釈が返ると先頭に積まれ、それが選ばれる", () => {
    const state = addInterpretation(emptyInterpretation, run(1));

    expect(state.runs).toHaveLength(1);
    expect(state.selectedId).toBe(1);
    expect(state.running).toBe(false);
    expect(selectedInterpretation(state)?.result.summary).toBe("要約 1");
  });

  // 引き直した直後に前の結果が出ていると、押した操作と画面が食い違う。
  it("引き直すと新しいほうが選ばれ、前のものも残る", () => {
    const first = addInterpretation(emptyInterpretation, run(1));
    const second = addInterpretation(first, run(2));

    expect(second.runs.map((r) => r.id)).toEqual([2, 1]);
    expect(second.selectedId).toBe(2);
  });

  it("選び直すと前の結果を見られる", () => {
    const state = selectInterpretation(
      addInterpretation(addInterpretation(emptyInterpretation, run(1)), run(2)),
      1,
    );

    expect(selectedInterpretation(state)?.result.summary).toBe("要約 1");
  });

  // 落ちた解釈を選べると、何も表示できないのに「選ばれている」状態になる。
  it("残っていない id は選べない", () => {
    const state = addInterpretation(emptyInterpretation, run(1));

    expect(selectInterpretation(state, 999)).toBe(state);
  });

  it("上限を超えたら古いほうから落ちる", () => {
    let state = emptyInterpretation;
    for (let i = 1; i <= MAX_INTERPRETATIONS + 2; i++) {
      state = addInterpretation(state, run(i));
    }

    expect(state.runs).toHaveLength(MAX_INTERPRETATIONS);
    expect(state.runs.at(0)?.id).toBe(MAX_INTERPRETATIONS + 2);
    expect(state.runs.at(-1)?.id).toBe(3);
  });

  // 走っているあいだも前の結果を読めるようにする。消えるなら、引き直しは
  // 「失うかもしれない」操作になる。
  it("解釈中も前の結果は残る", () => {
    const state = startInterpretation(addInterpretation(emptyInterpretation, run(1)));

    expect(state.running).toBe(true);
    expect(selectedInterpretation(state)?.result.summary).toBe("要約 1");
  });

  it("失敗しても前の結果は残る", () => {
    const state = failInterpretation(
      startInterpretation(addInterpretation(emptyInterpretation, run(1))),
      failure,
    );

    expect(state.running).toBe(false);
    expect(state.failure).toBe(failure);
    expect(selectedInterpretation(state)?.result.summary).toBe("要約 1");
  });

  it("成功すると前の失敗は消える", () => {
    const state = addInterpretation(
      failInterpretation(emptyInterpretation, failure),
      run(1),
    );

    expect(state.failure).toBeUndefined();
  });

  it("実行時の粒度を持ち回る", () => {
    const state = addInterpretation(emptyInterpretation, run(1, "epic"));

    expect(selectedInterpretation(state)?.granularity).toBe("epic");
  });

  it("並び順のラベルは新しい順で付く", () => {
    expect(interpretationOrderLabel(0)).toBe("最新");
    expect(interpretationOrderLabel(2)).toBe("2 つ前");
  });
});

/**
 * 世代の無効化と履歴の追加が噛み合うことを固定する。
 *
 * 履歴は「返ってきたものを積む」だけなので、**捨てるべき応答を捨てる責任は
 * 世代側にある。** 保存が `invalidateAll` を呼んだ後に古い応答が届いても、
 * 積まないことをここで見る。切れると、保存で捨てたはずの解釈が履歴として
 * 復活する（いまの内容を解釈したものだと誤読される）。
 */
describe("世代との噛み合わせ", () => {
  it("保存で無効にした解釈は積まれない", () => {
    const generations = createGenerations();
    const gen = generations.start("a1");

    generations.invalidateAll();

    let state = emptyInterpretation;
    if (generations.isCurrent("a1", gen)) {
      state = addInterpretation(state, run(gen));
    }

    expect(state.runs).toEqual([]);
  });

  it("無効になっていない解釈は積まれる", () => {
    const generations = createGenerations();
    const gen = generations.start("a1");

    let state = emptyInterpretation;
    if (generations.isCurrent("a1", gen)) {
      state = addInterpretation(state, run(gen));
    }

    expect(state.runs.map((r) => r.id)).toEqual([gen]);
  });
});
