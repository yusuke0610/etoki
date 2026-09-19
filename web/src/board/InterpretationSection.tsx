import type { Granularity, Interpretation, ProjectAccess, SyncItem } from "../api/types";
import { ErrorNotice } from "../ErrorNotice";
import { GRANULARITY_LABEL } from "./annotationLabel";
import { InterpretationDraft } from "./InterpretationDraft";
import {
  interpretationOrderLabel,
  selectedInterpretation,
  type InterpretationRun,
  type InterpretationState,
} from "./interpretationHistory";
import { INTERPRETATION_UNAVAILABLE_ID, type CreationState } from "./panelShared";
import type { ProjectLink } from "./projectLink";

type InterpretationSectionProps = {
  /** 説明文の id を注釈ごとに分けるために持つ。一覧に複数並ぶため。 */
  annotationId: string;
  /** 注釈の粒度。作成前に手直しできる範囲がこれで変わる。 */
  granularity: Granularity;
  state?: InterpretationState;
  creation?: CreationState;
  /** 未保存の変更があるあいだは解釈させない（ADR 0018）。 */
  stale: boolean;
  /** 保存中は下書きの編集も止める。 */
  saving: boolean;
  /** いま作成を始められない理由。押せるなら null（表は `exclusion.ts`）。 */
  creationBlocked: string | null;
  projectAccess: ProjectAccess;
  /** LLM が未設定なら理由。使えるなら null（ADR 0030）。 */
  interpretationUnavailable: string | null;
  /** GitHub が未設定なら理由。使えるなら null（ADR 0030）。 */
  creationUnavailable: string | null;
  /** この注釈が GitHub に在らしめているもの（ADR 0026）。 */
  previous: SyncItem[];
  /** 作成したものを確かめにいく先。組めなければ null（ADR 0025）。 */
  projectLink: ProjectLink | null;
  onInterpret: () => void;
  onSelectInterpretation: (runId: number) => void;
  onCreate: (interpretationId: number, interpretation: Interpretation) => void;
};

/**
 * 解釈の実行と結果表示。
 *
 * 結果を見せるだけで、ここから GitHub には何も作らない。何を作るかは
 * 開発者が別途トリガーする。
 */
export function InterpretationSection({
  annotationId,
  granularity,
  state,
  creation,
  stale,
  saving,
  creationBlocked,
  projectAccess,
  interpretationUnavailable,
  creationUnavailable,
  previous,
  projectLink,
  onInterpret,
  onSelectInterpretation,
  onCreate,
}: InterpretationSectionProps) {
  const running = state?.running ?? false;
  const runs = state?.runs ?? [];
  // いま見ている解釈。1 件も返っていなければ undefined。
  const selected = selectedInterpretation(state);
  // 押せない理由は title に隠さず本文として出す。disabled なボタンはフォーカスも
  // 当たらないので、title ではキーボードと読み上げの利用者に理由が届かない。
  const blockedId = `interpret-blocked-${annotationId}`;
  // **設定の不足が先。** 保存しても状況は変わらないので、「保存してから」を
  // 先に出すと、保存した人がもう一度同じところで止まる（ADR 0030）。
  //
  // 未設定の理由はパネルの上に 1 つだけ出ているので、ここでは指すだけにする。
  // 注釈の数だけ同じ文を並べない。
  const unavailable = interpretationUnavailable !== null;
  const describedBy = unavailable
    ? INTERPRETATION_UNAVAILABLE_ID
    : stale
      ? blockedId
      : undefined;

  return (
    <div className="interpretation">
      {/*
        未保存のあいだは押させない。テキストは保存済みシーンから、画像は画面
        から取るので、揃っていないと 1 回の解釈の入力が食い違う（ADR 0018）。
      */}
      <button
        type="button"
        onClick={onInterpret}
        disabled={running || unavailable || stale}
        aria-describedby={describedBy}
      >
        {running ? "解釈中…" : "解釈する"}
      </button>

      {!unavailable && stale && (
        <p className="hint" id={blockedId}>
          保存してから解釈できます。テキストは保存済みのシーンから、
          画像は画面から取るためです。
        </p>
      )}

      {/*
        失敗しても過去の結果は消さない。引き直しに失敗しただけで前の結果まで
        消えると、やり直せば済むはずの失敗が取り返しのつかないものになる。
      */}
      {state?.failure && <ErrorNotice failure={state.failure} />}

      {/*
        2 件以上あるときだけ出す。1 件しか無いのに選択肢を並べると、選ぶ
        余地があるように見えて読むものが増える。
      */}
      {runs.length > 1 && (
        <InterpretationHistory
          annotationId={annotationId}
          runs={runs}
          selectedId={selected?.id ?? null}
          onSelect={onSelectInterpretation}
        />
      )}

      {selected && (
        <InterpretationDraft
          // 選び直したら手直しは引き継がない。別の解釈に対する編集が
          // 混ざると、何を作るのかが読めなくなる（解釈し直したときと同じ）。
          key={selected.id}
          annotationId={annotationId}
          granularity={granularity}
          result={selected.result}
          created={selected.created ?? []}
          creation={creation}
          saving={saving}
          creationBlocked={creationBlocked}
          projectAccess={projectAccess}
          creationUnavailable={creationUnavailable}
          previous={previous}
          projectLink={projectLink}
          onCreate={(interpretation) => onCreate(selected.id, interpretation)}
        />
      )}
    </div>
  );
}

/**
 * 引いた解釈を並べて、どれを見るか選ばせる。
 *
 * **サーバーには何も置かない。** 解釈は GitHub にも DB にも何も作らないので、
 * 残す意味があるのは画面を開いているあいだだけ。保存すると前提のシーンが
 * 変わるので、そこで丸ごと捨てる（`BoardPage` の `save`）。
 *
 * 実行時刻と粒度を添えるのは、見比べる材料がその 2 つだから。同じ粒度で
 * 引き直したのか、指定を変えて引いたのかが読めないと、選ぶ理由が無い。
 */
function InterpretationHistory({
  annotationId,
  runs,
  selectedId,
  onSelect,
}: {
  annotationId: string;
  /** 新しい順。 */
  runs: InterpretationRun[];
  selectedId: number | null;
  onSelect: (runId: number) => void;
}) {
  // 一覧に複数の注釈が並ぶので、id は注釈ごとに分ける。
  const selectId = `interpretation-history-${annotationId}`;

  return (
    <div className="interpretation-history">
      <label htmlFor={selectId}>解釈結果</label>
      <select
        id={selectId}
        value={selectedId ?? ""}
        onChange={(e) => onSelect(Number(e.target.value))}
      >
        {runs.map((run, i) => (
          <option key={run.id} value={run.id}>
            {`${interpretationOrderLabel(i)}・${formatRunTime(run.at)}・粒度 ${
              GRANULARITY_LABEL[run.granularity]
            }`}
          </option>
        ))}
      </select>
    </div>
  );
}

/**
 * 解釈を引いた時刻。
 *
 * 日付は出さない。解釈は保存で捨てるので、画面に並ぶのは同じセッションの
 * ものだけになる。
 */
function formatRunTime(at: string): string {
  return new Date(at).toLocaleTimeString("ja-JP");
}
