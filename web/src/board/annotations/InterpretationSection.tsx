import type {
  Granularity,
  Interpretation,
  ProjectAccess,
  SyncItem,
} from "../../api/types";
import { ErrorNotice } from "../../app/ErrorNotice";
import { GRANULARITY_LABEL } from "./annotationLabel";
import { DraftEditor } from "./DraftEditor";
import {
  interpretationOrderLabel,
  selectedInterpretation,
  type InterpretationRun,
  type InterpretationState,
} from "./interpretationHistory";
import { DetailBand, type InterpretControl } from "./DetailBand";
import type { CreationState } from "./panelShared";
import type { ProjectLink } from "../target/projectLink";

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
  /** 作る先の見出し（`acme/web › #1 ロードマップ`）。帯の文に出す。 */
  targetLabel: string | null;
  /**
   * 未保存の変更があるか。**押せない理由には使わない。** 「絵解く」は未保存なら
   * 保存してから読む（`BoardPage`、#247）。ここでは、そうなることを案内するために使う。
   */
  stale: boolean;
  /** LLM が未設定なら理由。使えるなら null（ADR 0030）。 */
  interpretationUnavailable: string | null;
  onInterpret: () => void;
  onSelectInterpretation: (runId: number) => void;
  onCreate: (interpretationId: number, interpretation: Interpretation) => void;
};

/**
 * 解釈の結果と、下端の帯（`DetailBand`）。注釈の詳細（`AnnotationDetail`）の
 * 本文に出す。
 *
 * 解釈を 1 件選んでいれば、帯は下書きの手直し（`DraftEditor`）が描く。作成の
 * 口は下書きを持つ側にしか組めないため。まだ選べるものが無いうちは、ここで
 * 「解釈する」だけの帯を描く（**解釈するまで作成のボタンは出さない**）。
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
  targetLabel,
  stale,
  interpretationUnavailable,
  onInterpret,
  onSelectInterpretation,
  onCreate,
}: InterpretationSectionProps) {
  const running = state?.running ?? false;
  const runs = state?.runs ?? [];
  // いま見ている解釈。1 件も返っていなければ undefined。
  const selected = selectedInterpretation(state);

  const interpret: InterpretControl = {
    annotationId,
    label: runs.length > 0 || state?.failure !== undefined ? "絵解き直す" : "絵解く",
    // 保存しているあいだも「絵解き中」に数える。押した操作の中で保存するので
    // （`BoardPage`）、保存が終わるまでの待ちも絵解きの一部。
    running: running || saving,
    // 押せない理由は設定の不足だけ。**未保存では止めない。** 保存してから読むので、
    // テキスト（保存済みシーン）と画像（画面）は揃う（ADR 0018）。
    blocked: interpretationUnavailable,
    onInterpret,
  };

  return (
    <div className="interpretation-result-area">
      {running && <p className="hint">絵解きしています…</p>}

      {/*
        解釈の前でも詳細は開ける（粒度と種別を選ぶ場所がここなので）。何を押すと
        何が起きるかを出す。保存すると解釈は捨てる（前提のシーンが変わる、
        web/CLAUDE.md）ので、開いたまま保存したときも同じ文に戻る。
      */}
      {!running && runs.length === 0 && !state?.failure && (
        <p className="hint">
          まだ絵解きしていません。「絵解く」を押すと、この注釈を読んで、作るものの
          下書きを出します。
        </p>
      )}
      {/*
        未保存のあいだだけ、保存が入ることを言う。**保存は全部の注釈の引いた解釈と
        作成の状態を捨てる**ので、この注釈の前の結果も、ほかの注釈の結果も消える。
        黙って保存すると、結果が消えた理由が読めない。
      */}
      {stale && !running && (
        <p className="hint">
          未保存の変更があります。「{interpret.label}」を押すと保存してから読みます。
          保存すると、ほかの注釈のぶんも含めて、これまでの絵解きの結果を捨てます。
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

      {selected ? (
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
          targetLabel={targetLabel}
          interpret={interpret}
          onCreate={(interpretation) => onCreate(selected.id, interpretation)}
        />
      ) : (
        <DetailBand interpret={interpret} />
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
