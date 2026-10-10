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
  /** 未保存の変更があるあいだは解釈させない（ADR 0018）。 */
  stale: boolean;
  /** LLM が未設定なら理由。使えるなら null（ADR 0030）。 */
  interpretationUnavailable: string | null;
  /** ほかの囲みの解釈が走っているか。自分自身のぶんは含めない。 */
  interpretationBusyElsewhere: boolean;
  onInterpret: () => void;
  onSelectInterpretation: (runId: number) => void;
  onCreate: (interpretationId: number, interpretation: Interpretation) => void;
};

/**
 * 未保存のあいだ「解釈する」を押せない理由。テキストは保存済みシーンから、
 * 画像は画面から取るので、揃っていないと 1 回の解釈の入力が食い違う（ADR 0018）。
 */
const STALE_REASON =
  "保存してから解釈できます。テキストは保存済みのシーンから、画像は画面から取るためです。";

/**
 * ほかの囲みの解釈が走っているあいだ「解釈する」を押せない理由。解釈は 1 人に
 * つき同時に 1 件までで（ADR 0044）、押すと 429 で断られる。断られてから知らせる
 * より、押す前に言う。
 */
const BUSY_ELSEWHERE_REASON =
  "ほかの囲みを解釈している最中です。終わってから解釈できます。";

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
  interpretationBusyElsewhere,
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
    label:
      runs.length > 0 || state?.failure !== undefined ? "解釈をやり直す" : "解釈する",
    running,
    // **設定の不足が先。** 保存しても状況は変わらないので、「保存してから」を
    // 先に出すと、保存した人がもう一度同じところで止まる（ADR 0030）。
    // 走っている解釈も保存では変わらないので、未保存より先に出す。
    blocked:
      interpretationUnavailable ??
      (interpretationBusyElsewhere ? BUSY_ELSEWHERE_REASON : null) ??
      (stale ? STALE_REASON : null),
    onInterpret,
  };

  return (
    <div className="interpretation-result-area">
      {running && <p className="hint">解釈しています…</p>}

      {/*
        解釈の前でも詳細は開ける（粒度と種別を選ぶ場所がここなので）。何を押すと
        何が起きるかを出す。保存すると解釈は捨てる（前提のシーンが変わる、
        web/CLAUDE.md）ので、開いたまま保存したときも同じ文に戻る。
      */}
      {!running && runs.length === 0 && !state?.failure && (
        <p className="hint">
          まだ解釈していません。「解釈する」を押すと、この注釈を読んで、作るものの
          下書きを出します。保存すると、前に解釈した結果は捨てます。
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
