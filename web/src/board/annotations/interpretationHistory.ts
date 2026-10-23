import type { Failure } from "../../api/errorMessage";
import type { Granularity, Interpretation, SyncItem } from "../../api/types";

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
  /**
   * この解釈から作ったもの。作成の 1 回ごとに 1 要素で、古い順。
   *
   * **下書きではなく解釈の側に持つ**（ADR 0052）。下書きは解釈を選び直すと
   * 作り直されるので、そちらだけに持つと、戻ってきたときに作成済みの項目が
   * 全部選ばれた状態に戻る。解釈と同じく保存で捨てる（前提のシーンが変わる）。
   *
   * **回ごとに分けて持つ。** 下書きが外すのは 1 回の作成で増えたぶんだけ
   * （`markCreated`）なので、平らにすると「どこまで反映したか」を数えられない。
   */
  created?: SyncItem[][];
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
  /**
   * 走っているサーバーのジョブ（ADR 0085）。始めて、ジョブが分かってから入る。
   *
   * **取り消しはこれがあるときだけ出す**（#330）。始める途中（画像の書き出しと
   * 始める要求の往復）で取り消せると、同じ絵を押し直したときに同じジョブが返る
   * 仕組みと競合し、押し直した側のジョブまで捨てうる。
   */
  jobId?: string;
  /**
   * 取り消しを送って、返事を待っているあいだ。**取り消せたと分かるまでは走って
   * いる扱いのまま**（`running` は true）。サーバーが受けていないのに画面だけ
   * 止めると、ジョブは走り続けて枠を握り、開き直すと戻ってくる。
   */
  cancelling?: boolean;
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
  return {
    ...prev,
    running: true,
    failure: undefined,
    jobId: undefined,
    cancelling: undefined,
  };
}

/** 走っているジョブが分かった。 */
export function trackJob(prev: InterpretationState, jobId: string): InterpretationState {
  return { ...prev, jobId };
}

/**
 * 解釈を取り消した（#330）。
 *
 * **失敗にはしない。** 押した人が止めたので、失敗の帯で知らせることが無い。
 * 過去の結果と選択は残す（引き直しを始める前と同じ）。
 */
export function cancelInterpretation(prev: InterpretationState): InterpretationState {
  return { ...prev, running: false, jobId: undefined, cancelling: false };
}

/** 取り消しを送った。返事が来るまでは走っている扱いのまま。 */
export function startCancel(prev: InterpretationState): InterpretationState {
  return { ...prev, cancelling: true, failure: undefined };
}

/**
 * 取り消せなかった。**走っている扱いのまま、理由を出す。** 見に行くのも続ける
 * ので、終われば結果が積まれる。押し直せば、もう一度取り消せる。
 */
export function cancelFailed(
  prev: InterpretationState,
  failure: Failure,
): InterpretationState {
  return { ...prev, cancelling: false, failure };
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
  return { ...prev, running: false, failure, jobId: undefined, cancelling: false };
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

/**
 * 解釈から作った 1 回ぶんを積む。
 *
 * 上限を超えて落ちた id を指したら何もしない。
 */
export function recordCreated(
  prev: InterpretationState,
  id: number,
  items: SyncItem[],
): InterpretationState {
  if (!prev.runs.some((r) => r.id === id)) return prev;
  return {
    ...prev,
    runs: prev.runs.map((r) =>
      r.id === id ? { ...r, created: [...(r.created ?? []), items] } : r,
    ),
  };
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
