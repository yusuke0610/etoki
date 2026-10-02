import type { Granularity, Interpretation, ProjectAccess, SyncItem } from "../api/types";
import { ErrorNotice } from "../ErrorNotice";
import { GRANULARITY_LABEL } from "./annotationLabel";
import { DraftEditor } from "./DraftEditor";
import {
  interpretationOrderLabel,
  selectedInterpretation,
  type InterpretationRun,
  type InterpretationState,
} from "./interpretationHistory";
import { INTERPRETATION_UNAVAILABLE_ID, type CreationState } from "./panelShared";
import type { ProjectLink } from "./projectLink";

/**
 * カードの「解釈結果を開く」の id。詳細を閉じたとき、焦点をここへ戻す
 * （`AnnotationDetail`）。
 */
export function openDetailButtonId(annotationId: string): string {
  return `interpret-open-${annotationId}`;
}

/**
 * カードの「解釈する」の id。「解釈結果を開く」が無いとき（保存で解釈が捨てられた
 * あとなど）、詳細を閉じた焦点の戻り先にする（`AnnotationDetail`）。
 */
export function interpretButtonId(annotationId: string): string {
  return `interpret-run-${annotationId}`;
}

type InterpretControlProps = {
  /** 説明文の id を注釈ごとに分けるために持つ。一覧に複数並ぶため。 */
  annotationId: string;
  /**
   * 注釈の見出し。ボタンの名前に見えない形で添える。**添えないと、注釈の数だけ
   * 「解釈結果を開く」が並び、読み上げではどれの結果か区別できない。**
   */
  label: string;
  state?: InterpretationState;
  /** 未保存の変更があるあいだは解釈させない（ADR 0018）。 */
  stale: boolean;
  /** LLM が未設定なら理由。使えるなら null（ADR 0030）。 */
  interpretationUnavailable: string | null;
  onInterpret: () => void;
  /** 解釈の結果（中央の面）を開く。 */
  onOpen: () => void;
};

/**
 * 注釈のカードに残す、解釈の口（ADR 0065 の右のパネル）。
 *
 * **結果はここに出さない。** 下書きの手直しまで含めると 1 件で画面数枚分に
 * 伸び、ほかの注釈の状態（このパネルの主題）が読めなくなる。結果は中央の面
 * （`AnnotationDetail`）に広く出し、ここには押す口と開く口だけを置く。
 */
export function InterpretControl({
  annotationId,
  label,
  state,
  stale,
  interpretationUnavailable,
  onInterpret,
  onOpen,
}: InterpretControlProps) {
  const running = state?.running ?? false;
  // 開けるものがあるか。**解釈中も開ける。** 閉じたあとに結果を待つ場所が
  // 無いと、返ってきたことに気づけない。
  const openable =
    running || (state?.runs.length ?? 0) > 0 || state?.failure !== undefined;
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
      <div className="interpretation-actions">
        {/*
          未保存のあいだは押させない。テキストは保存済みシーンから、画像は画面
          から取るので、揃っていないと 1 回の解釈の入力が食い違う（ADR 0018）。
        */}
        <button
          type="button"
          id={interpretButtonId(annotationId)}
          onClick={onInterpret}
          disabled={running || unavailable || stale}
          aria-describedby={describedBy}
        >
          {running ? "解釈中…" : "解釈する"}
          <span className="visually-hidden">（{label}）</span>
        </button>
        {openable && (
          <button type="button" id={openDetailButtonId(annotationId)} onClick={onOpen}>
            解釈結果を開く
            <span className="visually-hidden">（{label}）</span>
          </button>
        )}
      </div>

      {!unavailable && stale && (
        <p className="hint" id={blockedId}>
          保存してから解釈できます。テキストは保存済みのシーンから、
          画像は画面から取るためです。
        </p>
      )}
    </div>
  );
}

type InterpretationSectionProps = {
  /** 説明文の id を注釈ごとに分けるために持つ。一覧に複数並ぶため。 */
  annotationId: string;
  /** 注釈の粒度。作成前に手直しできる範囲がこれで変わる。 */
  granularity: Granularity;
  state?: InterpretationState;
  creation?: CreationState;
  /** 保存中は下書きの編集も止める。 */
  saving: boolean;
  /** いま作成を始められない理由。押せるなら null（表は `exclusion.ts`）。 */
  creationBlocked: string | null;
  projectAccess: ProjectAccess;
  /** GitHub が未設定なら理由。使えるなら null（ADR 0030）。 */
  creationUnavailable: string | null;
  /** この注釈が GitHub に在らしめているもの（ADR 0026）。 */
  previous: SyncItem[];
  /** 作成したものを確かめにいく先。組めなければ null（ADR 0025）。 */
  projectLink: ProjectLink | null;
  onSelectInterpretation: (runId: number) => void;
  onCreate: (interpretationId: number, interpretation: Interpretation) => void;
};

/**
 * 解釈の結果。中央の面（`AnnotationDetail`）に出す。
 *
 * 結果を見せるだけで、ここから GitHub には何も作らない。何を作るかは
 * 開発者が別途トリガーする。解釈する口は注釈のカード（`InterpretControl`）に
 * ある。
 */
export function InterpretationSection({
  annotationId,
  granularity,
  state,
  creation,
  saving,
  creationBlocked,
  projectAccess,
  creationUnavailable,
  previous,
  projectLink,
  onSelectInterpretation,
  onCreate,
}: InterpretationSectionProps) {
  const running = state?.running ?? false;
  const runs = state?.runs ?? [];
  // いま見ている解釈。1 件も返っていなければ undefined。
  const selected = selectedInterpretation(state);

  return (
    <div className="interpretation-result-area">
      {running && <p className="hint">解釈しています…</p>}

      {/*
        保存すると解釈は捨てる（前提のシーンが変わる、web/CLAUDE.md）。開いた
        ままだと中が空になるので、何が起きたのかと、次に何をすればよいかを出す。
      */}
      {!running && runs.length === 0 && !state?.failure && (
        <p className="hint">
          解釈の結果はありません。保存すると前の結果は捨てます。注釈の「解釈する」から
          解釈し直せます。
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
        <DraftEditor
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
