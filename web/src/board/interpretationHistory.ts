import type { Failure } from "../api/errorMessage";
import type { Granularity, Interpretation } from "../api/types";

/**
 * 1 つの注釈に残す解釈の件数。
 *
 * **無制限にはしない。** 解釈結果は epic / issue の一覧を丸ごと抱えるので、
 * 引き直すほどメモリに積み上がる。一方で、見比べるのは直近の数回なので、
 * 古いものを持ち続ける理由も無い。
 */
export const MAX_INTERPRETATIONS = 5;

/** 1 回ぶんの解釈。 */
export type InterpretationRun = {
  /**
   * この注釈の中での通し番号。世代（`Generations`）と同じ値を使う。
   *
   * **時刻をキーにしない。** 時刻は同じ値が並びうるうえ、選択の同一性に
   * 使うと、表示のために持っている値が識別子を兼ねることになる。
   */
  id: number;
  /** 実行時刻（ISO 8601）。どれがいつのものかを並べて見せるために持つ。 */
  at: string;
  /**
   * 実行したときの粒度。
   *
   * **いまの粒度ではなく、その解釈を出したときの粒度。** 粒度を変えて引き
   * 直すのは想定した使い方なので、後から見て「どの指定で出たものか」が
   * 読めないと見比べる意味が薄れる。
   */
  granularity: Granularity;
  result: Interpretation;
};

/** 1 つの注釈ぶんの解釈の状態。 */
export type InterpretationState = {
  /** 解釈の往復が走っているか。 */
  running: boolean;
  /**
   * 直近の失敗。成功したら消える。
   *
   * **失敗しても過去の結果は捨てない。** 引き直しに失敗しただけで前の結果まで
   * 消えると、失敗の代償が「やり直せばよい」で済まなくなる。
   */
  failure?: Failure;
  /** 新しい順。最大 MAX_INTERPRETATIONS 件。 */
  runs: InterpretationRun[];
  /** いま見ている解釈の id。1 件も無ければ null。 */
  selectedId: number | null;
};

/** まだ一度も解釈していない状態。 */
export const emptyInterpretation: InterpretationState = {
  running: false,
  runs: [],
  selectedId: null,
};

/**
 * 解釈を始めた。
 *
 * **過去の結果と選択はそのまま残す。** 走っているあいだも前の結果を読める
 * ようにしておかないと、引き直しは「消えるかもしれない」操作になる。
 */
export function startInterpretation(
  prev: InterpretationState = emptyInterpretation,
): InterpretationState {
  return { ...prev, running: true, failure: undefined };
}

/**
 * 解釈が返ってきた。
 *
 * 新しいものを先頭に積み、上限を超えたぶんは古いほうから落とす。積んだものを
 * そのまま選択する。引き直した直後に出ているのが前の結果では、押した操作と
 * 画面が食い違う。
 */
export function addInterpretation(
  prev: InterpretationState = emptyInterpretation,
  run: InterpretationRun,
): InterpretationState {
  return {
    running: false,
    failure: undefined,
    runs: [run, ...prev.runs].slice(0, MAX_INTERPRETATIONS),
    selectedId: run.id,
  };
}

/** 解釈に失敗した。過去の結果と選択は残す。 */
export function failInterpretation(
  prev: InterpretationState = emptyInterpretation,
  failure: Failure,
): InterpretationState {
  return { ...prev, running: false, failure };
}

/**
 * 見る解釈を選び直す。
 *
 * 上限を超えて落ちた id を選ぼうとしたら何もしない。無いものを選んだことに
 * すると、画面には何も出ていないのに「選ばれている」状態になる。
 */
export function selectInterpretation(
  prev: InterpretationState,
  id: number,
): InterpretationState {
  if (!prev.runs.some((r) => r.id === id)) return prev;
  return { ...prev, selectedId: id };
}

/** いま見ている解釈。1 件も無ければ undefined。 */
export function selectedInterpretation(
  state: InterpretationState | undefined,
): InterpretationRun | undefined {
  if (!state) return undefined;
  return state.runs.find((r) => r.id === state.selectedId);
}

/**
 * 一覧に出す並び順のラベル。
 *
 * 時刻だけだと、どれが最後に引いたものかを読み取るのに時刻を比べることに
 * なる。並びは新しい順なので、位置がそのまま新しさになる。
 */
export function interpretationOrderLabel(index: number): string {
  return index === 0 ? "最新" : `${index} つ前`;
}
