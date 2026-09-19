import { type Failure } from "../api/errorMessage";
import type { CreatedRun, SyncRun } from "../api/types";

/**
 * 注釈パネルの複数のファイルが同じ値を指す必要があるもの（#146）。
 *
 * **ここに置くのは、分けたファイルどうしで揃っていないと壊れるものだけ。**
 * `CreationState` / `RunHistoryState` は `BoardPage` が作って渡す状態、
 * `INTERPRETATION_UNAVAILABLE_ID` は説明文を出す側（`AnnotationPanel`）と
 * 指す側（`InterpretationSection`）が同じ値を使う必要がある。
 */

/** 注釈 1 つぶんの作成の進み具合。 */
export type CreationState =
  | { status: "running" }
  | { status: "done"; run: CreatedRun }
  | { status: "error"; failure: Failure };

/**
 * 注釈 1 つぶんの実行履歴の読み込み具合。
 *
 * 未実行（キーが無い）と「引いたが 0 件」は別物。前者はまだ押していない、
 * 後者は「一度も作っていない」ことが分かっている状態。
 */
export type RunHistoryState =
  | { status: "loading" }
  | { status: "done"; runs: SyncRun[] }
  | { status: "error"; failure: Failure };

/**
 * 「LLM が未設定」の説明文の id。
 *
 * パネルに 1 つしか出さないので固定でよい。各注釈の「解釈する」がここを指す。
 */
export const INTERPRETATION_UNAVAILABLE_ID = "interpretation-unavailable";
